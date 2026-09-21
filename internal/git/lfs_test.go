package git

import (
	"crypto/sha256"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zekihan/git-mirrorer/internal/testutil"
)

func TestLFSTransferAllSnapshotRefs(t *testing.T) {
	if _, err := exec.LookPath("git-lfs"); err != nil {
		t.Skip("Git LFS is not installed")
	}
	testutil.Isolate(t)
	source, dest := testutil.Repository(t, false), testutil.Repository(t, true)
	testutil.Git(t, source, "lfs", "install", "--local")
	testutil.Commit(t, source, ".gitattributes", "*.bin filter=lfs diff=lfs merge=lfs -text\n")
	testutil.Commit(t, source, "asset.bin", "historical-payload")
	main := testutil.Commit(t, source, "asset.bin", "current-payload")
	custom := testutil.Commit(t, source, "asset.bin", "custom-ref-payload")
	testutil.Git(t, source, "update-ref", "refs/custom/assets", custom)
	testutil.Git(t, source, "reset", "--hard", main)
	c := newClient(t)
	if err := c.CheckLFS(t.Context()); err != nil {
		t.Fatal(err)
	}
	cache := filepath.Join(t.TempDir(), "mirror.git")
	if err := c.Clone(t.Context(), source, cache); err != nil {
		t.Fatal(err)
	}
	if err := c.LFSFetch(t.Context(), cache, source); err != nil {
		t.Fatal(err)
	}
	if err := c.LFSPush(t.Context(), cache, dest); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Push(t.Context(), cache, dest, true); err != nil {
		t.Fatal(err)
	}
	testutil.EqualRefs(t, source, dest)
	for _, payload := range []string{"historical-payload", "current-payload", "custom-ref-payload"} {
		oid := fmt.Sprintf("%x", sha256.Sum256([]byte(payload)))
		path := filepath.Join(dest, "lfs", "objects", oid[:2], oid[2:4], oid)
		got, err := os.ReadFile(path)
		if err != nil || string(got) != payload {
			t.Fatalf("destination payload %s: %q %v", payload, got, err)
		}
	}
	// A fresh cache cannot fetch a source payload that has been removed.
	oid := fmt.Sprintf("%x", sha256.Sum256([]byte("historical-payload")))
	if err := os.Remove(filepath.Join(cache, "lfs", "objects", oid[:2], oid[2:4], oid)); err != nil {
		t.Fatal(err)
	}
	testutil.Git(t, cache, "config", "lfs.allowincompletepush", "true")
	if err := c.LFSPush(t.Context(), cache, testutil.Repository(t, true)); err == nil {
		t.Fatal("missing upload accepted despite required complete transfer")
	}
	if err := os.Remove(filepath.Join(source, ".git", "lfs", "objects", oid[:2], oid[2:4], oid)); err != nil {
		t.Fatal(err)
	}
	second := filepath.Join(t.TempDir(), "mirror.git")
	if err := c.Clone(t.Context(), source, second); err != nil {
		t.Fatal(err)
	}
	if err := c.LFSFetch(t.Context(), second, source); err == nil {
		t.Fatal("missing payload accepted")
	}
}

func TestLFSEmptySource(t *testing.T) {
	if _, err := exec.LookPath("git-lfs"); err != nil {
		t.Skip("Git LFS is not installed")
	}
	testutil.Isolate(t)
	source, dest := testutil.Repository(t, true), testutil.Repository(t, true)
	c := newClient(t)
	cache := filepath.Join(t.TempDir(), "mirror.git")
	if err := c.Clone(t.Context(), source, cache); err != nil {
		t.Fatal(err)
	}
	if err := c.LFSFetch(t.Context(), cache, source); err != nil {
		t.Fatal(err)
	}
	if err := c.LFSPush(t.Context(), cache, dest); err != nil {
		t.Fatal(err)
	}
}

func TestLFSRejectsEndpointOverrides(t *testing.T) {
	testutil.Isolate(t)
	source := testutil.Repository(t, false)
	testutil.Commit(t, source, "a", "one")
	c := newClient(t)
	cache := filepath.Join(t.TempDir(), "mirror.git")
	if err := c.Clone(t.Context(), source, cache); err != nil {
		t.Fatal(err)
	}
	testutil.Git(t, cache, "config", "lfs.url", "https://user:SECRET@example.invalid/objects")
	err := c.LFSFetch(t.Context(), cache, source)
	if err == nil || strings.Contains(err.Error(), "SECRET") {
		t.Fatalf("unsafe override: %v", err)
	}
	testutil.Git(t, cache, "config", "--unset", "lfs.url")
	testutil.Commit(t, source, ".lfsconfig", "[lfs]\nurl = https://example.invalid/objects\n")
	if err := c.Fetch(t.Context(), cache, source); err != nil {
		t.Fatal(err)
	}
	if err := c.LFSFetch(t.Context(), cache, source); err == nil {
		t.Fatal(".lfsconfig accepted")
	}
}
