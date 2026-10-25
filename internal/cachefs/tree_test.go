package cachefs

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestCleanupStopsAtEntryBudgetAndDeadline(t *testing.T) {
	root := t.TempDir()
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
	outside, root := t.TempDir(), t.TempDir()
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
	root = t.TempDir()
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
	root, external := t.TempDir(), t.TempDir()
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
