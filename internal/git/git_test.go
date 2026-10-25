package git

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/zekihan/git-mirrorer/internal/testutil"
)

func newClient(t *testing.T) *Client {
	t.Helper()
	c, err := New(t.Context(), 10*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestExactMirror(t *testing.T) {
	testutil.Isolate(t)
	source, dest := testutil.Repository(t, false), testutil.Repository(t, true)
	first := testutil.Commit(t, source, "a", "one")
	testutil.Git(t, source, "branch", "old")
	testutil.Git(t, source, "tag", "v1")
	testutil.Git(t, source, "update-ref", "refs/custom/snapshot", first)
	testutil.Git(t, source, "notes", "add", "-m", "note")
	c := newClient(t)
	cache := filepath.Join(t.TempDir(), "mirror.git")
	if err := c.Clone(t.Context(), source, cache); err != nil {
		t.Fatal(err)
	}
	if err := c.Validate(t.Context(), cache, source); err != nil {
		t.Fatal(err)
	}
	changes, err := c.Push(t.Context(), cache, dest, true)
	if err != nil || changes.Created != 5 {
		t.Fatalf("initial push: %+v %v", changes, err)
	}
	testutil.EqualRefs(t, source, dest)
	changes, err = c.Push(t.Context(), cache, dest, true)
	if err != nil || changes.Unchanged != 5 {
		t.Fatalf("unchanged: %+v %v", changes, err)
	}
	second := testutil.Commit(t, source, "a", "two")
	testutil.Git(t, source, "tag", "-f", "v1")
	testutil.Git(t, source, "branch", "-D", "old")
	testutil.Git(t, source, "update-ref", "-d", "refs/custom/snapshot")
	testutil.Git(t, dest, "update-ref", "refs/heads/destination-only", first)
	if err := c.Fetch(t.Context(), cache, source); err != nil {
		t.Fatal(err)
	}
	changes, err = c.Push(t.Context(), cache, dest, true)
	if err != nil || changes.Deleted != 3 || changes.Updated != 2 {
		t.Fatalf("updates: %+v %v", changes, err)
	}
	testutil.EqualRefs(t, source, dest)
	testutil.Git(t, source, "reset", "--hard", first)
	if err := c.Fetch(t.Context(), cache, source); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Push(t.Context(), cache, dest, true); err != nil {
		t.Fatal(err)
	}
	testutil.EqualRefs(t, source, dest)
	if got := testutil.Git(t, dest, "rev-parse", "refs/tags/v1"); got != second {
		t.Fatal("moved tag lost")
	}
}

func TestAtomicAndPartialPush(t *testing.T) {
	testutil.Isolate(t)
	source, dest := testutil.Repository(t, false), testutil.Repository(t, true)
	testutil.Commit(t, source, "a", "one")
	c := newClient(t)
	cache := filepath.Join(t.TempDir(), "mirror.git")
	if err := c.Clone(t.Context(), source, cache); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Push(WithTrustedLocalHooks(t.Context()), cache, dest, true); err != nil {
		t.Fatal(err)
	}
	before := testutil.Refs(t, dest)
	testutil.Commit(t, source, "a", "two")
	testutil.Git(t, source, "branch", "blocked")
	if err := c.Fetch(t.Context(), cache, source); err != nil {
		t.Fatal(err)
	}
	hook := "#!/bin/sh\n[ \"$1\" != refs/heads/blocked ]\n"
	if err := os.WriteFile(filepath.Join(dest, "hooks", "update"), []byte(hook), 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Push(WithTrustedLocalHooks(t.Context()), cache, dest, true); err == nil {
		t.Fatal("atomic push unexpectedly succeeded")
	}
	if got := testutil.Refs(t, dest); got != before {
		t.Fatal("atomic rejection modified refs")
	}
	changes, err := c.Push(WithTrustedLocalHooks(t.Context()), cache, dest, false)
	if err == nil || changes.Rejected == 0 || changes.Updated == 0 {
		t.Fatalf("partial push: %+v %v", changes, err)
	}
	if testutil.Refs(t, dest) == before {
		t.Fatal("non-atomic push did not update allowed branch")
	}
	if err := os.Remove(filepath.Join(dest, "hooks", "update")); err != nil {
		t.Fatal(err)
	}
	testutil.Git(t, dest, "config", "receive.advertiseAtomic", "false")
	if _, err := c.Push(WithTrustedLocalHooks(t.Context()), cache, dest, true); err == nil {
		t.Fatal("atomic unsupported must fail")
	}
	if _, err := c.Push(WithTrustedLocalHooks(t.Context()), cache, dest, false); err != nil {
		t.Fatal(err)
	}
}

func TestEmptySourceAndDefaultBranchPolicy(t *testing.T) {
	testutil.Isolate(t)
	source, dest := testutil.Repository(t, false), testutil.Repository(t, true)
	testutil.Commit(t, source, "a", "one")
	c := newClient(t)
	cache := filepath.Join(t.TempDir(), "mirror.git")
	if err := c.Clone(t.Context(), source, cache); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Push(t.Context(), cache, dest, true); err != nil {
		t.Fatal(err)
	}
	testutil.Git(t, source, "update-ref", "-d", "refs/heads/main")
	if err := c.Fetch(t.Context(), cache, source); err != nil {
		t.Fatal(err)
	}
	testutil.Git(t, dest, "config", "receive.denyDeleteCurrent", "true")
	if _, err := c.Push(t.Context(), cache, dest, true); err == nil {
		t.Fatal("default branch policy was ignored")
	}
	testutil.Git(t, dest, "config", "receive.denyDeleteCurrent", "false")
	if _, err := c.Push(t.Context(), cache, dest, true); err != nil {
		t.Fatal(err)
	}
	if refs := testutil.Refs(t, dest); refs != "" {
		t.Fatalf("refs remain: %s", refs)
	}
	emptyCache := filepath.Join(t.TempDir(), "empty.git")
	if err := c.Clone(t.Context(), source, emptyCache); err != nil {
		t.Fatal(err)
	}
	if err := c.Validate(t.Context(), emptyCache, source); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Push(t.Context(), emptyCache, dest, true); err != nil {
		t.Fatalf("empty to empty: %v", err)
	}
}

func TestCanceledCommandAndBoundedDiagnostics(t *testing.T) {
	r := &Runner{Executable: "/bin/sh", Timeout: 10 * time.Second, Env: os.Environ()}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := r.Run(ctx, "", nil, nil, "-c", "exit 0"); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel: %v", err)
	}
	err := r.Run(t.Context(), "", nil, nil, "-c", "head -c 200000 /dev/zero; echo https://user:SECRET@example.com/a?token=SECRET >&2; exit 1")
	if err == nil || len(err.Error()) > diagnosticLimit+200 || strings.Contains(err.Error(), "SECRET") {
		t.Fatalf("diagnostics not bounded/redacted: %v", err)
	}
}

func TestStreamingPushParser(t *testing.T) {
	p := &pushParser{}
	for _, fragment := range []string{"*", "\tref\tnew\n=\tref\tup", " to date\n!\tref\trejected\n", "+\tref\tforced\n-\tref\tdeleted\n"} {
		_, _ = p.Write([]byte(fragment))
	}
	if p.changes != (Changes{Created: 1, Updated: 1, Deleted: 1, Unchanged: 1, Rejected: 1}) {
		t.Fatalf("%+v", p.changes)
	}
}

func TestLocalReceiveHooksRequireExplicitTrust(t *testing.T) {
	testutil.Isolate(t)
	source := testutil.Repository(t, false)
	testutil.Commit(t, source, "a", "one")
	client := newClient(t)
	cache := filepath.Join(t.TempDir(), "mirror.git")
	if err := client.Clone(t.Context(), source, cache); err != nil {
		t.Fatal(err)
	}
	for _, trusted := range []bool{false, true} {
		destination := testutil.Repository(t, true)
		sentinel := filepath.Join(destination, "hook-executed")
		hook := "#!/bin/sh\nprintf executed > '" + sentinel + "'\n"
		if err := os.WriteFile(filepath.Join(destination, "hooks", "update"), []byte(hook), 0o700); err != nil {
			t.Fatal(err)
		}
		ctx := t.Context()
		if trusted {
			ctx = WithTrustedLocalHooks(ctx)
		}
		if _, err := client.Push(ctx, cache, destination, true); err != nil {
			t.Fatal(err)
		}
		testutil.EqualRefs(t, source, destination)
		_, err := os.Stat(sentinel)
		if trusted && err != nil {
			t.Fatal("explicitly trusted hook did not execute", err)
		}
		if !trusted && !os.IsNotExist(err) {
			t.Fatal("default mode executed destination hook")
		}
	}
}
