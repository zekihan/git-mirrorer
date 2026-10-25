// Package cachefs provides bounded, no-follow traversal of private managed caches.
package cachefs

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"syscall"

	"github.com/zekihan/git-mirrorer/internal/safepath"
)

// Walk streams batches, with a fixed depth limit and a caller-supplied entry
// limit/deadline. visit runs after directory children, allowing bounded cleanup.
// The caller must exclude concurrent hostile local writers from the managed tree.
func Walk(ctx context.Context, path string, maxEntries int64, visit func(string, os.FileInfo) error) (int64, error) {
	var entriesSeen int64
	root, err := safepath.Open(path, false)
	if err != nil {
		return 0, err
	}
	defer func() { _ = root.Close() }()
	var walk func(*os.Root, string, int) error
	walk = func(root *os.Root, path string, depth int) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if depth > 128 {
			return errors.New("cache directory depth limit exceeded")
		}
		dir, err := root.Open(".")
		if err != nil {
			return err
		}
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
				info, err := root.Lstat(entry.Name())
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
					childRoot, err := root.OpenRoot(entry.Name())
					if errors.Is(err, os.ErrNotExist) {
						continue
					}
					if err != nil {
						return err
					}
					childDir, err := childRoot.Open(".")
					if err != nil {
						_ = childRoot.Close()
						return err
					}
					opened, err := childDir.Stat()
					childDir.Close()
					if err != nil || !os.SameFile(info, opened) {
						_ = childRoot.Close()
						return errors.New("cache directory identity changed")
					}
					err = safepath.Trusted(opened, false)
					if err == nil {
						err = walk(childRoot, child, depth+1)
					}
					_ = childRoot.Close()
					if err != nil {
						if errors.Is(err, os.ErrNotExist) {
							continue
						}
						return err
					}
				} else {
					if !info.Mode().IsRegular() {
						return errors.New("cache contains a special file")
					}
					file, err := root.OpenFile(entry.Name(), os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
					if errors.Is(err, os.ErrNotExist) {
						continue
					}
					if err != nil {
						return err
					}
					opened, err := file.Stat()
					file.Close()
					if err != nil {
						return err
					}
					if !os.SameFile(info, opened) {
						return errors.New("cache file identity changed")
					}
					if err := safepath.Trusted(opened, false); err != nil {
						return err
					}
					info = opened
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
	err = walk(root, path, 0)
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
