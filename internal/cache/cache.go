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
	"github.com/zekihan/git-mirrorer/internal/safepath"
)

type Lease struct {
	lock      *os.File
	Path      string
	entry     string
	source    string
	namespace *safepath.Snapshot
	identity  *safepath.Snapshot
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
	locks, err := safepath.Open(filepath.Join(stateDir, "locks"), false)
	if err != nil {
		return nil, err
	}
	defer func() { _ = locks.Close() }()
	file, err := locks.OpenFile(key+".lock", os.O_CREATE|os.O_RDWR|syscall.O_NOFOLLOW, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open cache lock: %w", err)
	}
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || safepath.Trusted(info, false) != nil || info.Sys().(*syscall.Stat_t).Nlink != 1 {
		file.Close()
		return nil, errors.New("cache lock is not a regular file")
	}
	if err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		file.Close()
		return nil, fmt.Errorf("source cache is locked or locking is unavailable: %w", err)
	}
	entry := filepath.Join(stateDir, "repositories", key)
	namespace, err := safepath.Capture(filepath.Join(stateDir, "repositories"))
	if err != nil {
		file.Close()
		return nil, err
	}
	return &Lease{namespace: namespace, lock: file, Path: filepath.Join(entry, "repo.git"), entry: entry, source: source}, nil
}

func ensureDirectory(path string) error {
	root, err := safepath.Open(path, true)
	if err != nil {
		return err
	}
	return root.Close()
}

func (l *Lease) Close() error {
	// Closing the descriptor releases the advisory lock. Never unlink a lockfile:
	// another process may already have opened that inode.
	return l.lock.Close()
}

// Prepare returns true when a clone already supplied this run's source snapshot.
func (l *Lease) Prepare(ctx context.Context, client *git.Client) (bool, error) {
	if err := l.namespace.Check(); err != nil {
		return false, err
	}
	if l.identity != nil {
		if err := l.identity.Check(); err != nil {
			return false, err
		}
	}
	_, err := os.Lstat(l.entry)
	if err == nil {
		if err := l.validateMetadata(ctx); err != nil {
			return false, err
		}
		if err := client.Validate(ctx, l.Path, l.source); err != nil {
			return false, fmt.Errorf("invalid cache %s; remove this managed entry while idle to rebuild: %w", l.entry, err)
		}
		l.identity, err = safepath.Capture(l.entry)
		return false, err
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
	l.identity, err = safepath.Capture(l.entry)
	return true, err
}

func (l *Lease) validateMetadata(ctx context.Context) error {
	// Validate every descendant through stable descriptors before invoking Git.
	root, err := safepath.Open(l.entry, false)
	if err != nil {
		return err
	}
	defer func() { _ = root.Close() }()
	if err := validateTree(ctx, root); err != nil {
		return err
	}
	data, err := root.ReadFile("metadata.json")
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

func validateTree(ctx context.Context, root *os.Root) error {
	dir, err := root.Open(".")
	if err != nil {
		return err
	}
	defer dir.Close()
	entries, err := dir.ReadDir(-1)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return err
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return errors.New("symlinked cache entry")
		}
		file, err := root.OpenFile(entry.Name(), os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
		if err != nil {
			return err
		}
		info, err := file.Stat()
		file.Close()
		if err != nil {
			return err
		}
		if err := safepath.Trusted(info, false); err != nil {
			return err
		}
		if info.IsDir() {
			child, err := root.OpenRoot(entry.Name())
			if err != nil {
				return err
			}
			err = validateTree(ctx, child)
			_ = child.Close()
			if err != nil {
				return err
			}
		} else if !info.Mode().IsRegular() || info.Sys().(*syscall.Stat_t).Nlink != 1 {
			return errors.New("cache entries must be private regular files or directories")
		}
	}
	return nil
}
