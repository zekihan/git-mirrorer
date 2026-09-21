package cache

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"

	"github.com/zekihan/git-mirrorer/internal/git"
)

type Lease struct {
	lock   *os.File
	Path   string
	entry  string
	source string
}

type metadata struct {
	Version int    `json:"version"`
	Source  string `json:"source"`
}

type RefreshError struct{ Err error }

func (e *RefreshError) Error() string { return e.Err.Error() }
func (e *RefreshError) Unwrap() error { return e.Err }

func Key(source string) string {
	sum := sha256.Sum256([]byte(source))
	return hex.EncodeToString(sum[:])
}

func Acquire(stateDir, source string) (*Lease, error) {
	for _, dir := range []string{stateDir, filepath.Join(stateDir, "locks"), filepath.Join(stateDir, "repositories")} {
		if err := ensureDirectory(dir); err != nil {
			return nil, err
		}
	}
	key := Key(source)
	lockPath := filepath.Join(stateDir, "locks", key+".lock")
	fd, err := syscall.Open(lockPath, syscall.O_CREAT|syscall.O_RDWR|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open cache lock: %w", err)
	}
	file := os.NewFile(uintptr(fd), lockPath)
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		file.Close()
		return nil, errors.New("cache lock is not a regular file")
	}
	if err := syscall.Flock(fd, syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		file.Close()
		return nil, fmt.Errorf("source cache is locked or locking is unavailable: %w", err)
	}
	entry := filepath.Join(stateDir, "repositories", key)
	return &Lease{lock: file, Path: filepath.Join(entry, "repo.git"), entry: entry, source: source}, nil
}

func ensureDirectory(path string) error {
	if err := os.MkdirAll(path, 0o700); err != nil {
		return err
	}
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("managed path must be a real directory: %s", path)
	}
	return nil
}

func (l *Lease) Close() error {
	// Closing the descriptor releases the advisory lock. Never unlink a lockfile:
	// another process may already have opened that inode.
	return l.lock.Close()
}

// Prepare returns true when a clone already supplied this run's source snapshot.
func (l *Lease) Prepare(ctx context.Context, client *git.Client) (bool, error) {
	_, err := os.Lstat(l.entry)
	if err == nil {
		if err := l.validateMetadata(ctx); err != nil {
			return false, err
		}
		if err := client.Validate(ctx, l.Path, l.source); err != nil {
			return false, fmt.Errorf("invalid cache %s; remove this managed entry while idle to rebuild: %w", l.entry, err)
		}
		return false, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return false, err
	}
	temp, err := os.MkdirTemp(filepath.Dir(l.entry), ".clone-*")
	if err != nil {
		return false, err
	}
	defer os.RemoveAll(temp)
	repoPath := filepath.Join(temp, "repo.git")
	if err := client.Clone(ctx, l.source, repoPath); err != nil {
		return false, &RefreshError{Err: err}
	}
	if err := client.Validate(ctx, repoPath, l.source); err != nil {
		return false, err
	}
	data, err := json.Marshal(metadata{Version: 1, Source: l.source})
	if err != nil {
		return false, err
	}
	if err := os.WriteFile(filepath.Join(temp, "metadata.json"), data, 0o600); err != nil {
		return false, err
	}
	if err := os.Rename(temp, l.entry); err != nil {
		return false, err
	}
	return true, nil
}

func (l *Lease) validateMetadata(ctx context.Context) error {
	// Reject links anywhere inside a completed cache, including config/object paths.
	err := filepath.WalkDir(l.entry, func(path string, entry os.DirEntry, err error) error {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err != nil {
			return err
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("symlinked cache entry: %s", path)
		}
		return nil
	})
	if err != nil {
		return err
	}
	data, err := os.ReadFile(filepath.Join(l.entry, "metadata.json"))
	if err != nil {
		return fmt.Errorf("invalid cache ownership metadata: %w", err)
	}
	var meta metadata
	if err := json.Unmarshal(data, &meta); err != nil || meta.Version != 1 || meta.Source != l.source {
		return fmt.Errorf("cache ownership mismatch at %s; remove this managed entry while idle to rebuild", l.entry)
	}
	info, err := os.Stat(l.Path)
	if err != nil || !info.IsDir() {
		return errors.New("cache repository directory is missing")
	}
	return nil
}
