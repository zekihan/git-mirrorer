// Package cachefs provides bounded, no-follow traversal of private managed caches.
package cachefs

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"syscall"
)

// Walk streams batches, with a fixed depth limit and a caller-supplied entry
// limit/deadline. visit runs after directory children, allowing bounded cleanup.
// The caller must exclude concurrent hostile local writers from the managed tree.
func Walk(ctx context.Context, path string, maxEntries int64, visit func(string, os.FileInfo) error) (int64, error) {
	var entriesSeen int64
	var walk func(string, int) error
	walk = func(path string, depth int) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if depth > 128 {
			return errors.New("cache directory depth limit exceeded")
		}
		fd, err := syscall.Open(path, syscall.O_RDONLY|syscall.O_DIRECTORY|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0)
		if err != nil {
			return err
		}
		dir := os.NewFile(uintptr(fd), path)
		defer dir.Close()
		for {
			if err := ctx.Err(); err != nil {
				return err
			}
			entries, readErr := dir.ReadDir(128)
			for _, entry := range entries {
				if err := ctx.Err(); err != nil {
					return err
				}
				entriesSeen++
				if entriesSeen > maxEntries {
					return errors.New("cache entry budget exceeded")
				}
				child := filepath.Join(path, entry.Name())
				info, err := entry.Info()
				if errors.Is(err, os.ErrNotExist) {
					continue
				}
				if err != nil {
					return err
				}
				if info.Mode()&os.ModeSymlink != 0 {
					return errors.New("symlinked cache entry")
				}
				if info.IsDir() {
					if err := walk(child, depth+1); err != nil {
						if errors.Is(err, os.ErrNotExist) {
							continue
						}
						return err
					}
				} else if !info.Mode().IsRegular() {
					return errors.New("cache contains a special file")
				}
				if visit != nil {
					if err := visit(child, info); err != nil {
						return err
					}
				}
			}
			if errors.Is(readErr, io.EOF) {
				return nil
			}
			if readErr != nil {
				return readErr
			}
		}
	}
	err := walk(path, 0)
	return entriesSeen, err
}

// RemoveTree stops on the deadline or entry budget. A partially removed tree is
// left for explicit operator recovery instead of unbounded background cleanup.
func RemoveTree(ctx context.Context, path string, maxEntries int64) error {
	_, err := Walk(ctx, path, maxEntries, func(path string, _ os.FileInfo) error { return os.Remove(path) })
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	return os.Remove(path)
}
