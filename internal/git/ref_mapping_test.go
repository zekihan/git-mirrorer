package git

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/zekihan/git-mirrorer/internal/testutil"
)

func mappedFixtureRefs(refs string) string {
	lines := strings.Split(strings.ReplaceAll(refs, "refs/pull/", "refs/upstream/github/pull/"), "\n")
	sort.Strings(lines)
	return strings.Join(lines, "\n")
}

func TestMappedMirrorPreservesSourceAndPrunesMappedRefs(t *testing.T) {
	testutil.Isolate(t)
	source, dest, plain := testutil.Repository(t, false), testutil.Repository(t, true), testutil.Repository(t, true)
	first := testutil.Commit(t, source, "a", "one")
	testutil.Git(t, source, "update-ref", "refs/pull/1/head", first)
	testutil.Git(t, source, "update-ref", "refs/reviewable/test", first)
	testutil.Git(t, source, "tag", "-a", "v1", "-m", "annotated")
	testutil.Git(t, dest, "config", "receive.hideRefs", "refs/pull/")
	c := newClient(t)
	cache := filepath.Join(testutil.TempDir(t), "mirror.git")
	if err := c.Clone(t.Context(), source, cache); err != nil {
		t.Fatal(err)
	}
	mapping := RefMapping{From: "refs/pull/", To: "refs/upstream/github/pull/"}
	before := testutil.Refs(t, cache)
	if _, err := c.Push(t.Context(), cache, dest, true, mapping); err != nil {
		t.Fatal(err)
	}
	if got := testutil.Refs(t, dest); got != mappedFixtureRefs(before) {
		t.Fatalf("mapped refs differ: %s", got)
	}
	if testutil.Refs(t, cache) != before {
		t.Fatal("mapping changed source snapshot")
	}
	if _, err := c.Push(t.Context(), cache, plain, true); err != nil {
		t.Fatal(err)
	}
	testutil.EqualRefs(t, source, plain)
	second := testutil.Commit(t, source, "a", "two")
	testutil.Git(t, source, "update-ref", "refs/pull/2/head", second)
	testutil.Git(t, source, "update-ref", "-d", "refs/pull/1/head")
	if err := c.Fetch(t.Context(), cache, source); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Push(t.Context(), cache, dest, true, mapping); err != nil {
		t.Fatal(err)
	}
	if got := testutil.Refs(t, dest); got != mappedFixtureRefs(testutil.Refs(t, source)) {
		t.Fatalf("mapped update/prune differs: %s", got)
	}
	if err := c.Validate(t.Context(), cache, source); err != nil {
		t.Fatal(err)
	}
}

func TestMappedRefCollisionDoesNotWriteDestination(t *testing.T) {
	testutil.Isolate(t)
	source, dest := testutil.Repository(t, false), testutil.Repository(t, true)
	first := testutil.Commit(t, source, "a", "one")
	testutil.Git(t, source, "update-ref", "refs/pull/1/head", first)
	testutil.Git(t, source, "update-ref", "refs/upstream/github/pull/1/head", first)
	c := newClient(t)
	cache := filepath.Join(testutil.TempDir(t), "mirror.git")
	if err := c.Clone(t.Context(), source, cache); err != nil {
		t.Fatal(err)
	}
	_, err := c.Push(t.Context(), cache, dest, true, RefMapping{From: "refs/pull/", To: "refs/upstream/github/pull/"})
	if err == nil || !strings.Contains(err.Error(), "collide") {
		t.Fatalf("collision not rejected: %v", err)
	}
	if testutil.Refs(t, dest) != "" {
		t.Fatal("collision wrote destination")
	}
}

func TestMappedMirrorCleansScratch(t *testing.T) {
	testutil.Isolate(t)
	scratch := testutil.TempDir(t)
	t.Setenv("TMPDIR", scratch)
	source, dest := testutil.Repository(t, false), testutil.Repository(t, true)
	first := testutil.Commit(t, source, "a", "one")
	testutil.Git(t, source, "update-ref", "refs/pull/1/head", first)
	c := newClient(t)
	cache := filepath.Join(testutil.TempDir(t), "mirror.git")
	if err := c.Clone(t.Context(), source, cache); err != nil {
		t.Fatal(err)
	}
	testutil.Git(t, dest, "config", "receive.denyNonFastForwards", "true")
	for _, failed := range []bool{false, true} {
		target := dest
		if failed {
			target = filepath.Join(scratch, "missing")
		}
		_, err := c.Push(t.Context(), cache, target, true, RefMapping{From: "refs/pull/", To: "refs/upstream/github/pull/"})
		if (err != nil) != failed {
			t.Fatalf("unexpected push result: %v", err)
		}
		entries, err := os.ReadDir(filepath.Dir(cache))
		if err != nil {
			t.Fatal(err)
		}
		for _, entry := range entries {
			if strings.HasPrefix(entry.Name(), ".mapped-refs-") {
				t.Fatal("scratch retained")
			}
		}
	}
}

func TestRejectAmbiguousRefMappings(t *testing.T) {
	for _, mappings := range [][]RefMapping{
		{{From: "refs/pull/", To: "refs/pull/"}},
		{{From: "refs/", To: "refs/upstream/"}},
		{{From: "refs/pull", To: "refs/upstream/"}},
		{{From: "refs/pull/", To: "refs/upstream/../"}},
		{{From: "refs/pull/", To: "refs/foo.lock/"}},
		{{From: "refs/pull/", To: "refs/foo*/"}},
		{{From: "refs/pull/", To: "refs/foo/"}, {From: "refs/pull/sub/", To: "refs/bar/"}},
		{{From: "refs/pull/", To: "refs/foo/"}, {From: "refs/bar/", To: "refs/foo/sub/"}},
		{{From: "refs/pull/", To: "refs/foo/"}, {From: "refs/foo/", To: "refs/bar/"}},
	} {
		if err := ValidateRefMappings(mappings); err == nil {
			t.Fatalf("accepted invalid mappings: %+v", mappings)
		}
	}
}

func TestMappedRefNamespaceCollisionDoesNotWriteDestination(t *testing.T) {
	testutil.Isolate(t)
	source, dest := testutil.Repository(t, false), testutil.Repository(t, true)
	first := testutil.Commit(t, source, "a", "one")
	for _, ref := range []string{"refs/pull/1/head", "refs/upstream/github/pull/1", "refs/upstream/github/pull/1-other"} {
		testutil.Git(t, source, "update-ref", ref, first)
	}
	c := newClient(t)
	cache := filepath.Join(testutil.TempDir(t), "mirror.git")
	if err := c.Clone(t.Context(), source, cache); err != nil {
		t.Fatal(err)
	}
	_, err := c.Push(t.Context(), cache, dest, false, RefMapping{From: "refs/pull/", To: "refs/upstream/github/pull/"})
	if err == nil || !strings.Contains(err.Error(), "collide") {
		t.Fatalf("namespace collision not rejected: %v", err)
	}
	if testutil.Refs(t, dest) != "" {
		t.Fatal("collision wrote destination")
	}
}

func TestMappedMirrorIgnoresInheritedRefFormat(t *testing.T) {
	testutil.Isolate(t)
	source, dest := testutil.Repository(t, false), testutil.Repository(t, true)
	first := testutil.Commit(t, source, "a", "one")
	for _, ref := range []string{"refs/pull/1/head", "refs/heads/feature", "refs/tags/v1"} {
		testutil.Git(t, source, "update-ref", ref, first)
	}
	c := newClient(t)
	cache := filepath.Join(testutil.TempDir(t), "mirror.git")
	if err := c.Clone(t.Context(), source, cache); err != nil {
		t.Fatal(err)
	}
	mapping := RefMapping{From: "refs/pull/", To: "refs/upstream/github/pull/"}
	if _, err := c.Push(t.Context(), cache, dest, false, mapping); err != nil {
		t.Fatal(err)
	}
	before := testutil.Refs(t, dest)
	t.Setenv("GIT_DEFAULT_REF_FORMAT", "reftable")
	c = newClient(t)
	if _, err := c.Push(t.Context(), cache, dest, false, mapping); err != nil {
		t.Fatal(err)
	}
	if testutil.Refs(t, dest) != before {
		t.Fatal("inherited ref format changed destination")
	}
}

func TestMappedMirrorRejectsMisreadViewBeforePush(t *testing.T) {
	testutil.Isolate(t)
	source, dest := testutil.Repository(t, false), testutil.Repository(t, true)
	first := testutil.Commit(t, source, "a", "one")
	testutil.Git(t, source, "update-ref", "refs/pull/1/head", first)
	c := newClient(t)
	cache := filepath.Join(testutil.TempDir(t), "mirror.git")
	if err := c.Clone(t.Context(), source, cache); err != nil {
		t.Fatal(err)
	}
	mapping := RefMapping{From: "refs/pull/", To: "refs/upstream/github/pull/"}
	if _, err := c.Push(t.Context(), cache, dest, false, mapping); err != nil {
		t.Fatal(err)
	}
	before := testutil.Refs(t, dest)
	wrapper := filepath.Join(testutil.TempDir(t), "git-wrapper")
	if err := os.WriteFile(wrapper, []byte(`#!/bin/sh
case "$PWD" in
    */.mapped-refs-*)
        for arg in "$@"; do
            case "$arg" in for-each-ref) exit 0 ;; esac
        done
        ;;
esac
exec "$REAL_GIT" "$@"
`), 0o700); err != nil {
		t.Fatal(err)
	}
	c.runner.Env = append(c.runner.Env, "REAL_GIT="+c.runner.Executable)
	c.runner.Executable = wrapper
	_, err := c.Push(t.Context(), cache, dest, false, mapping)
	if err == nil || !strings.Contains(err.Error(), "does not match source inventory") {
		t.Fatalf("misread view not rejected: %v", err)
	}
	if testutil.Refs(t, dest) != before {
		t.Fatal("misread view changed destination")
	}
}
