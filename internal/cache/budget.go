package cache

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/zekihan/git-mirrorer/internal/cachefs"
)

// Limits bound retained storage and the work needed to inspect it. Bytes are
// logical file sizes; Entries include directories, files, and temporary files.
// Objects counts loose Git objects and objects in pack headers (including copies).
// Subprocess writes can overshoot between samples: use filesystem quotas for a
// hard allocation limit. All writers sharing stateDir must use the same policy.
type Limits struct {
	SourceBytes       int64         `yaml:"sourceBytes"`
	TotalBytes        int64         `yaml:"totalBytes"`
	SourceEntries     int64         `yaml:"sourceEntries"`
	TotalEntries      int64         `yaml:"totalEntries"`
	SourceObjects     int64         `yaml:"sourceObjects"`
	TotalObjects      int64         `yaml:"totalObjects"`
	ReserveBytes      int64         `yaml:"reserveBytes"`
	ValidationTimeout time.Duration `yaml:"validationTimeout"`
}

func DefaultLimits() Limits {
	return Limits{SourceBytes: 5 << 30, TotalBytes: 20 << 30, SourceEntries: 250_000,
		TotalEntries: 1_000_000, SourceObjects: 2_000_000, TotalObjects: 8_000_000,
		ReserveBytes: 1 << 30, ValidationTimeout: 10 * time.Second}
}

func (l Limits) Validate() error {
	if l.SourceBytes <= 0 || l.TotalBytes < l.SourceBytes || l.SourceEntries <= 0 || l.TotalEntries < l.SourceEntries || l.SourceObjects <= 0 || l.TotalObjects < l.SourceObjects || l.ReserveBytes <= 0 || l.ValidationTimeout <= 0 {
		return errors.New("cache limits must be positive and total limits must be at least source limits")
	}
	return nil
}

type usage struct{ bytes, entries, objects int64 }

// scan streams directory batches instead of WalkDir's unbounded sorted listing.
// The cache is private to the service account; local concurrent hostile writers
// are outside its trust model. Links and special files are always rejected.
func scan(ctx context.Context, path string, limit usage) (usage, error) {
	var used usage
	entries, err := cachefs.Walk(ctx, path, limit.entries, func(path string, info os.FileInfo) error {
		if info.Mode().IsRegular() {
			if info.Size() > limit.bytes-used.bytes {
				return errors.New("cache byte budget exceeded")
			}
			used.bytes += info.Size()
			objects, err := objectCount(path)
			if err != nil && !errors.Is(err, os.ErrNotExist) {
				return err
			}
			if objects > limit.objects-used.objects {
				return errors.New("cache object budget exceeded")
			}
			used.objects += objects
		}
		return nil
	})
	used.entries = entries
	return used, err
}

func objectCount(path string) (int64, error) {
	parent := filepath.Dir(path)
	if filepath.Base(parent) == "pack" && filepath.Base(filepath.Dir(parent)) == "objects" && strings.HasSuffix(path, ".pack") {
		fd, err := syscall.Open(path, syscall.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0)
		if err != nil {
			return 0, err
		}
		file := os.NewFile(uintptr(fd), path)
		defer file.Close()
		var header [12]byte
		_, err = io.ReadFull(file, header[:])
		// An in-progress pack can have an incomplete header. Git validates it before
		// publication; the post-operation scan checks every completed pack again.
		if err != nil {
			return 0, nil
		}
		if string(header[:4]) != "PACK" {
			return 0, errors.New("invalid Git pack header")
		}
		return int64(binary.BigEndian.Uint32(header[8:])), nil
	}
	if filepath.Base(filepath.Dir(parent)) == "objects" && len(filepath.Base(parent)) == 2 && (len(filepath.Base(path)) == 38 || len(filepath.Base(path)) == 62) {
		return 1, nil
	}
	return 0, nil
}

func (l *Lease) checkBudget(ctx context.Context, limits Limits) error {
	ctx, cancel := context.WithTimeout(ctx, limits.ValidationTimeout)
	defer cancel()
	root := filepath.Dir(l.entry)
	if _, err := scan(ctx, root, usage{limits.TotalBytes, limits.TotalEntries, limits.TotalObjects}); err != nil {
		return fmt.Errorf("total cache: %w", err)
	}
	paths := []string{l.entry}
	if temp := l.temporary.Load(); temp != nil {
		paths = append(paths, *temp)
	}
	var source usage
	for _, path := range paths {
		if _, err := os.Lstat(path); errors.Is(err, os.ErrNotExist) {
			continue
		} else if err != nil {
			return err
		}
		used, err := scan(ctx, path, usage{limits.SourceBytes - source.bytes, limits.SourceEntries - source.entries, limits.SourceObjects - source.objects})
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("source cache: %w", err)
		}
		source.bytes += used.bytes
		source.entries += used.entries
		source.objects += used.objects
	}
	var stat syscall.Statfs_t
	if err := syscall.Statfs(root, &stat); err != nil {
		return err
	}
	if uint64(stat.Bavail) < uint64(limits.ReserveBytes)/uint64(stat.Bsize)+1 {
		return errors.New("cache free-space reserve exhausted")
	}
	return ctx.Err()
}

// Mutate serializes storage writers across processes, allowing total accounting
// to include idle caches and abandoned clones without racing another writer.
// Source leases remain held through fan-out; only the storage phase is serialized.
func (l *Lease) Mutate(ctx context.Context, limits Limits, timeout time.Duration, operation func(context.Context) error) error {
	if err := limits.Validate(); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	path := filepath.Join(filepath.Dir(filepath.Dir(l.entry)), "locks", "budget.lock")
	fd, err := syscall.Open(path, syscall.O_CREAT|syscall.O_RDWR|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0o600)
	if err != nil {
		return err
	}
	file := os.NewFile(uintptr(fd), path)
	defer file.Close()
	if info, err := file.Stat(); err != nil || !info.Mode().IsRegular() {
		return errors.New("budget lock is not a regular file")
	}
	for {
		err := syscall.Flock(fd, syscall.LOCK_EX|syscall.LOCK_NB)
		if err == nil {
			break
		}
		if !errors.Is(err, syscall.EWOULDBLOCK) && !errors.Is(err, syscall.EAGAIN) {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(50 * time.Millisecond):
		}
	}
	if err := l.checkBudget(ctx, limits); err != nil {
		return err
	}
	workCtx, stop := context.WithCancelCause(ctx)
	inspectCtx, stopInspection := context.WithCancel(workCtx)
	done := make(chan error, 1)
	go func() {
		ticker := time.NewTicker(200 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-inspectCtx.Done():
				done <- nil
				return
			case <-ticker.C:
				if err := l.checkBudget(inspectCtx, limits); err != nil {
					if inspectCtx.Err() != nil {
						done <- nil
						return
					}
					stop(err)
					done <- err
					return
				}
			}
		}
	}()
	err = operation(workCtx)
	// Join inspection and reap subprocesses before releasing the accounting lock.
	stopInspection()
	inspectionErr := <-done
	cause := context.Cause(workCtx)
	stop(nil)
	if inspectionErr != nil {
		return inspectionErr
	}
	if cause != nil {
		return cause
	}
	if err != nil {
		return err
	}
	return l.checkBudget(ctx, limits)
}

// ClearLFS removes only this leased source's old payloads, before any new payload
// fetch or destination upload. Fetch --all rebuilds the complete current snapshot.
func (l *Lease) ClearLFS(ctx context.Context, limits Limits) error {
	if err := l.requireLock(); err != nil {
		return err
	}
	path := filepath.Join(l.Path, "lfs")
	if _, err := os.Lstat(path); errors.Is(err, os.ErrNotExist) {
		return nil
	} else if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, limits.ValidationTimeout)
	defer cancel()
	return cachefs.RemoveTree(ctx, path, limits.SourceEntries)
}
