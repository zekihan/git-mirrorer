package cache

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/zekihan/git-mirrorer/internal/git"
	"github.com/zekihan/git-mirrorer/internal/testutil"
)

func acquireTestLease(t *testing.T, state, source string) *Lease {
	t.Helper()
	lease, err := Acquire(state, source)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = lease.Close() })
	return lease
}

func writeFixture(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestBudgetPreflightIncludesIdleSourcesAndAbandonedClones(t *testing.T) {
	for _, name := range []string{"idle", ".clone-abandoned"} {
		t.Run(name, func(t *testing.T) {
			lease := acquireTestLease(t, t.TempDir(), "active")
			limits := DefaultLimits()
			limits.SourceBytes = 16
			limits.TotalBytes = 32
			writeFixture(t, filepath.Join(filepath.Dir(lease.entry), name, "data"), make([]byte, 33))
			called := false
			err := lease.Mutate(t.Context(), limits, time.Second, func(context.Context) error { called = true; return nil })
			if err == nil || !strings.Contains(err.Error(), "total cache") || called {
				t.Fatalf("preflight: called=%v err=%v", called, err)
			}
		})
	}
}

func TestBudgetStopsGrowingOperationAndReleasesLock(t *testing.T) {
	lease := acquireTestLease(t, t.TempDir(), "active")
	limits := DefaultLimits()
	limits.SourceBytes = 16
	limits.TotalBytes = 128
	start := time.Now()
	err := lease.Mutate(t.Context(), limits, 2*time.Second, func(ctx context.Context) error {
		writeFixture(t, filepath.Join(lease.entry, "oversized"), make([]byte, 17))
		<-ctx.Done()
		return ctx.Err()
	})
	if err == nil || !strings.Contains(err.Error(), "source cache") || time.Since(start) > time.Second {
		t.Fatalf("growth monitor: %v (%v)", err, time.Since(start))
	}
	if err := os.Remove(filepath.Join(lease.entry, "oversized")); err != nil {
		t.Fatal(err)
	}
	if err := lease.Mutate(t.Context(), limits, time.Second, func(context.Context) error { return nil }); err != nil {
		t.Fatalf("accounting lock leaked: %v", err)
	}
}

func TestBudgetChecksCompletedFastWritesAndReserve(t *testing.T) {
	lease := acquireTestLease(t, t.TempDir(), "active")
	limits := DefaultLimits()
	limits.SourceBytes = 16
	err := lease.Mutate(t.Context(), limits, time.Second, func(context.Context) error {
		writeFixture(t, filepath.Join(lease.entry, "fast"), make([]byte, 17))
		return nil
	})
	if err == nil || !strings.Contains(err.Error(), "byte budget") {
		t.Fatalf("postflight: %v", err)
	}
	limits = DefaultLimits()
	limits.ReserveBytes = 1 << 62
	called := false
	err = lease.Mutate(t.Context(), limits, time.Second, func(context.Context) error { called = true; return nil })
	if err == nil || !strings.Contains(err.Error(), "reserve") || called {
		t.Fatalf("reserve: %v called=%v", err, called)
	}
}

func TestBudgetLockWaitHasDeadline(t *testing.T) {
	lease := acquireTestLease(t, t.TempDir(), "active")
	path := filepath.Join(filepath.Dir(filepath.Dir(lease.entry)), "locks", "budget.lock")
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	if err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		t.Fatal(err)
	}
	called := false
	err = lease.Mutate(t.Context(), DefaultLimits(), 20*time.Millisecond, func(context.Context) error { called = true; return nil })
	if !errors.Is(err, context.DeadlineExceeded) || called {
		t.Fatalf("lock wait: %v called=%v", err, called)
	}
}

func TestStreamingScanEntryDeadlineObjectAndSymlinkLimits(t *testing.T) {
	root := t.TempDir()
	for i := range 256 {
		writeFixture(t, filepath.Join(root, fmt.Sprintf("%04d", i)), nil)
	}
	if used, err := scan(t.Context(), root, usage{1 << 20, 10, 100}); err == nil || used.entries != 11 {
		t.Fatalf("entry bound: %+v %v", used, err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), time.Nanosecond)
	defer cancel()
	<-ctx.Done()
	if _, err := scan(ctx, root, usage{1 << 20, 1000, 100}); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("deadline: %v", err)
	}
	root = t.TempDir()
	header := make([]byte, 12)
	copy(header, "PACK")
	binary.BigEndian.PutUint32(header[4:], 2)
	binary.BigEndian.PutUint32(header[8:], 101)
	writeFixture(t, filepath.Join(root, "objects", "pack", "pack-test.pack"), header)
	if _, err := scan(t.Context(), root, usage{1 << 20, 1000, 100}); err == nil || !strings.Contains(err.Error(), "object budget") {
		t.Fatalf("pack objects: %v", err)
	}
	root = t.TempDir()
	writeFixture(t, filepath.Join(root, "objects", "ab", strings.Repeat("c", 38)), nil)
	if used, err := scan(t.Context(), root, usage{1 << 20, 1000, 100}); err != nil || used.objects != 1 {
		t.Fatalf("loose objects: %+v %v", used, err)
	}
	if err := os.Symlink(t.TempDir(), filepath.Join(root, "link")); err != nil {
		t.Fatal(err)
	}
	if _, err := scan(t.Context(), root, usage{1 << 20, 1000, 100}); err == nil || !strings.Contains(err.Error(), "symlink") {
		t.Fatalf("symlink: %v", err)
	}
}

func TestCloneBudgetFailureDoesNotPublish(t *testing.T) {
	testutil.Isolate(t)
	source := testutil.Repository(t, false)
	testutil.Commit(t, source, "data", strings.Repeat("a", 100))
	client, err := git.New(t.Context(), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	lease := acquireTestLease(t, t.TempDir(), source)
	limits := DefaultLimits()
	limits.SourceBytes = 1
	lease.SetLimits(limits)
	err = lease.Mutate(t.Context(), limits, 5*time.Second, func(ctx context.Context) error { _, err := lease.Prepare(ctx, client); return err })
	if err == nil || !strings.Contains(err.Error(), "budget") {
		t.Fatalf("clone limit: %v", err)
	}
	if _, err := os.Lstat(lease.entry); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("oversize clone published: %v", err)
	}
	entries, err := os.ReadDir(filepath.Dir(lease.entry))
	if err != nil || len(entries) != 0 {
		t.Fatalf("clone leaked: %v %v", entries, err)
	}
}

func TestOwnershipMetadataReadIsBounded(t *testing.T) {
	lease := acquireTestLease(t, t.TempDir(), "source")
	writeFixture(t, filepath.Join(lease.entry, "metadata.json"), make([]byte, 64*1024+1))
	if err := lease.validateMetadata(t.Context()); err == nil || !strings.Contains(err.Error(), "metadata size") {
		t.Fatalf("metadata bound: %v", err)
	}
}

func TestBudgetRejectsUnsafeStateDirectoryAndLock(t *testing.T) {
	state := t.TempDir()
	if err := os.Chmod(state, 0o777); err != nil {
		t.Fatal(err)
	}
	if lease, err := Acquire(state, "source"); err == nil {
		lease.Close()
		t.Fatal("accepted writable shared state")
	}
	lease := acquireTestLease(t, t.TempDir(), "source")
	lock := filepath.Join(filepath.Dir(filepath.Dir(lease.entry)), "locks", "budget.lock")
	if err := os.Symlink(filepath.Join(t.TempDir(), "target"), lock); err != nil {
		t.Fatal(err)
	}
	called := false
	err := lease.Mutate(t.Context(), DefaultLimits(), time.Second, func(context.Context) error { called = true; return nil })
	if err == nil || called {
		t.Fatalf("symlinked budget lock: %v called=%v", err, called)
	}
}

func TestCleanupRequiresLiveSourceLease(t *testing.T) {
	lease := acquireTestLease(t, t.TempDir(), "source")
	payload := filepath.Join(lease.Path, "lfs", "objects", "payload")
	writeFixture(t, payload, []byte("required"))
	if err := ForPath(lease.Path).ClearLFS(t.Context(), DefaultLimits()); err == nil {
		t.Fatal("cleanup without source lease accepted")
	}
	if err := lease.Close(); err != nil {
		t.Fatal(err)
	}
	if err := lease.ClearLFS(t.Context(), DefaultLimits()); err == nil {
		t.Fatal("cleanup with closed source lease accepted")
	}
	if err := lease.Reclaim(t.Context(), nil); err == nil {
		t.Fatal("Git reclamation with closed source lease accepted")
	}
	if got, err := os.ReadFile(payload); err != nil || string(got) != "required" {
		t.Fatalf("unleased cleanup touched payload: %q %v", got, err)
	}
}
