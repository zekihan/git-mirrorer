package cachefs

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/zekihan/git-mirrorer/internal/testutil"
)

func TestWalkClassifiesAtomicReplacement(t *testing.T) {
	for _, kind := range []string{"regular", "symlink", "fifo", "directory", "writable", "writable-hardlink", "owner", "gone"} {
		t.Run(kind, func(t *testing.T) {
			if kind == "owner" && os.Geteuid() != 0 {
				t.Skip("changing inode ownership requires root")
			}
			root, staging := testutil.TempDir(t), testutil.TempDir(t)
			target, replacement := filepath.Join(root, "FETCH_HEAD"), filepath.Join(staging, "replacement")
			if err := os.WriteFile(target, []byte("old"), 0o600); err != nil {
				t.Fatal(err)
			}
			switch kind {
			case "symlink":
				if err := os.Symlink(staging, replacement); err != nil {
					t.Fatal(err)
				}
			case "fifo":
				if err := syscall.Mkfifo(replacement, 0o600); err != nil {
					t.Fatal(err)
				}
			case "directory":
				if err := os.Mkdir(replacement, 0o700); err != nil {
					t.Fatal(err)
				}
			default:
				if err := os.WriteFile(replacement, []byte("new payload"), 0o600); err != nil {
					t.Fatal(err)
				}
				if kind == "writable" || kind == "writable-hardlink" {
					if err := os.Chmod(replacement, 0o666); err != nil {
						t.Fatal(err)
					}
				}
				if kind == "writable-hardlink" {
					if err := os.Link(replacement, replacement+".alias"); err != nil {
						t.Fatal(err)
					}
				}
				if kind == "owner" {
					if err := os.Chown(replacement, 1, -1); err != nil {
						t.Fatal(err)
					}
				}
			}
			visited := false
			_, err := walk(t.Context(), root, 10, func(string, os.FileInfo) error {
				visited = true
				return nil
			}, func(path string) error {
				if kind == "gone" || kind == "directory" {
					if err := os.Remove(path); err != nil {
						return err
					}
				}
				if kind == "gone" {
					return nil
				}
				// Perform the rename in another goroutine at the Lstat/Open boundary.
				done := make(chan error, 1)
				go func() { done <- os.Rename(replacement, path) }()
				return <-done
			})
			if visited {
				t.Fatal("visited replaced or vanished inode")
			}
			switch kind {
			case "regular":
				if !errors.Is(err, ErrFileChanged) {
					t.Fatalf("expected retryable regular replacement: %v", err)
				}
				var size int64
				if _, err := Walk(t.Context(), root, 10, func(_ string, info os.FileInfo) error {
					size += info.Size()
					return nil
				}); err != nil || size != int64(len("new payload")) {
					t.Fatalf("fresh scan: size=%d err=%v", size, err)
				}
			case "gone":
				if err != nil {
					t.Fatal(err)
				}
			default:
				if err == nil || errors.Is(err, ErrFileChanged) {
					t.Fatalf("unsafe replacement must not be retryable: %v", err)
				}
			}
		})
	}
}

func TestCleanupStopsAtEntryBudgetAndDeadline(t *testing.T) {
	root := testutil.TempDir(t)
	for i := range 256 {
		if err := os.WriteFile(filepath.Join(root, fmt.Sprintf("%04d", i)), nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := RemoveTree(t.Context(), root, 10); err == nil || !strings.Contains(err.Error(), "entry budget") {
		t.Fatalf("cleanup bound: %v", err)
	}
	entries, err := os.ReadDir(root)
	if err != nil || len(entries) != 246 {
		t.Fatalf("cleanup exceeded budget: %d %v", len(entries), err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), time.Nanosecond)
	defer cancel()
	<-ctx.Done()
	if err := RemoveTree(ctx, root, 1000); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("cleanup deadline: %v", err)
	}
	entries, err = os.ReadDir(root)
	if err != nil || len(entries) != 246 {
		t.Fatalf("canceled cleanup mutated tree: %d %v", len(entries), err)
	}
	if err := RemoveTree(t.Context(), root, 1000); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(root); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("cleanup left root: %v", err)
	}
}

func TestCleanupNeverFollowsSymlinkAndWalkLimitsDepth(t *testing.T) {
	outside, root := testutil.TempDir(t), testutil.TempDir(t)
	target := filepath.Join(outside, "required")
	if err := os.WriteFile(target, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "link")); err != nil {
		t.Fatal(err)
	}
	if err := RemoveTree(t.Context(), root, 1000); err == nil {
		t.Fatal("cleanup followed link")
	}
	if got, err := os.ReadFile(target); err != nil || string(got) != "keep" {
		t.Fatalf("outside path changed: %q %v", got, err)
	}
	root = testutil.TempDir(t)
	path := root
	for range 129 {
		path = filepath.Join(path, "d")
		if err := os.Mkdir(path, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := Walk(t.Context(), root, 1000, nil); err == nil || !strings.Contains(err.Error(), "depth limit") {
		t.Fatalf("depth bound: %v", err)
	}
}

func TestPayloadHardlinksStillRequireTrustedInode(t *testing.T) {
	root, external := testutil.TempDir(t), testutil.TempDir(t)
	source := filepath.Join(external, "payload")
	if err := os.WriteFile(source, []byte("payload"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Link(source, filepath.Join(root, "payload")); err != nil {
		t.Fatal(err)
	}
	if _, err := Walk(t.Context(), root, 10, nil); err != nil {
		t.Fatalf("trusted LFS payload hardlink: %v", err)
	}
	if err := os.Chmod(source, 0o666); err != nil {
		t.Fatal(err)
	}
	if _, err := Walk(t.Context(), root, 10, nil); err == nil {
		t.Fatal("accepted writable payload inode through hardlink")
	}
}
