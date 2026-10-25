package cache

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync/atomic"
	"syscall"

	"github.com/zekihan/git-mirrorer/internal/cachefs"
	"github.com/zekihan/git-mirrorer/internal/git"
)

type Lease struct {
	lock      *os.File
	Path      string
	entry     string
	source    string
	limits    Limits
	temporary atomic.Pointer[string]
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
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm()&0o022 != 0 {
		return fmt.Errorf("managed path must be a real directory without group/other write access: %s", path)
	}
	return nil
}

func (l *Lease) Close() error {
	// Closing the descriptor releases the advisory lock. Never unlink a lockfile:
	// another process may already have opened that inode.
	return l.lock.Close()
}

func (l *Lease) SetLimits(limits Limits) { l.limits = limits }

// Prepare returns true when a clone already supplied this run's source snapshot.
func (l *Lease) Prepare(ctx context.Context, client *git.Client) (cloned bool, prepareErr error) {
	if err := l.requireLock(); err != nil {
		return false, err
	}
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
	l.temporary.Store(&temp)
	defer l.temporary.Store(nil)
	defer func() {
		limits := l.limits
		if limits == (Limits{}) {
			limits = DefaultLimits()
		}
		cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), limits.ValidationTimeout)
		defer cancel()
		if err := cachefs.RemoveTree(cleanupCtx, temp, limits.SourceEntries); err != nil {
			prepareErr = errors.Join(prepareErr, fmt.Errorf("partial clone cleanup stopped; remove the abandoned temporary entry while idle: %w", err))
		}
	}()
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
	limits := l.limits
	if limits == (Limits{}) {
		limits = DefaultLimits()
	}
	inspectCtx, cancel := context.WithTimeout(ctx, limits.ValidationTimeout)
	defer cancel()
	if _, err := scan(inspectCtx, temp, usage{limits.SourceBytes, limits.SourceEntries, limits.SourceObjects}); err != nil {
		return false, err
	}
	if err := os.Rename(temp, l.entry); err != nil {
		return false, err
	}
	return true, nil
}

func (l *Lease) validateMetadata(ctx context.Context) error {
	// Validation always has a deadline and a finite entry budget, even when
	// runTimeout is disabled. Read ownership metadata with a fixed size bound.
	limits := l.limits
	if limits == (Limits{}) {
		limits = DefaultLimits()
	}
	ctx, cancel := context.WithTimeout(ctx, limits.ValidationTimeout)
	defer cancel()
	if _, err := scan(ctx, l.entry, usage{limits.SourceBytes, limits.SourceEntries, limits.SourceObjects}); err != nil {
		return err
	}
	fd, err := syscall.Open(filepath.Join(l.entry, "metadata.json"), syscall.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0)
	if err != nil {
		return fmt.Errorf("invalid cache ownership metadata: %w", err)
	}
	file := os.NewFile(uintptr(fd), "metadata.json")
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, 64*1024+1))
	if err != nil || len(data) > 64*1024 {
		return errors.New("invalid cache ownership metadata size")
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

// ForPath supplies accounting for a path whose source lease is already held.
func ForPath(path string) *Lease { return &Lease{Path: path, entry: filepath.Dir(path)} }

func (l *Lease) requireLock() error {
	if l.lock == nil {
		return errors.New("source lease is required")
	}
	if _, err := l.lock.Stat(); err != nil {
		return fmt.Errorf("source lease is closed: %w", err)
	}
	return nil
}

func (l *Lease) Reclaim(ctx context.Context, client *git.Client) error {
	if err := l.requireLock(); err != nil {
		return err
	}
	return client.Reclaim(ctx, l.Path)
}
