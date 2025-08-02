package git

import (
	"github.com/zekihan/git-mirrorer/config"
	"os"
	"path/filepath"
	"testing"
)

func Test_getRepoURLFromPath(t *testing.T) {
	cfg := config.GetConfig()
	base := t.TempDir()
	cfg.RepoPath = base
	repoPath := filepath.Join(base, "myrepo.git")
	_ = os.MkdirAll(repoPath, 0o755)
	got := getRepoURLFromPath(repoPath)
	want := "/myrepo.git"
	if got != want {
		t.Errorf("getRepoURLFromPath() = %q, want %q", got, want)
	}

	// Path not containing base
	got = getRepoURLFromPath("/other/path/myrepo.git")
	if got != "" {
		t.Errorf("getRepoURLFromPath() with unrelated path = %q, want \"\"", got)
	}

	// Unescapable path
	got = getRepoURLFromPath("%zz")
	if got != "" {
		t.Errorf("getRepoURLFromPath() with bad escape = %q, want \"\"", got)
	}
}

func Test_getLatest(t *testing.T) {
	setupRepos(t)
	ctx := t.Context()
	val := config.GetConfig().Values[0]
	repo := val.Source

	// Remove repo if exists
	_ = os.RemoveAll(repo.GetRepoPath())

	// Should clone
	if err := getLatest(ctx, repo); err != nil {
		t.Fatalf("getLatest() clone failed: %v", err)
	}
	if _, err := os.Stat(repo.GetRepoPath()); err != nil {
		t.Errorf("getLatest() did not create repo: %v", err)
	}

	// Should fetch (repo exists)
	if err := getLatest(ctx, repo); err != nil {
		t.Errorf("getLatest() fetch failed: %v", err)
	}
}

func Test_HandleMirror(t *testing.T) {
	setupRepos(t)
	ctx := t.Context()
	val := config.GetConfig().Values[0]
	source := val.Source
	mirror := val.Mirrors[0]

	// Should succeed
	if err := HandleMirror(ctx, source, mirror); err != nil {
		t.Errorf("HandleMirror() error = %v, want nil", err)
	}

	// Same URL, should do nothing
	if err := HandleMirror(ctx, source, source); err != nil {
		t.Errorf("HandleMirror() with same URLs error = %v, want nil", err)
	}
}
