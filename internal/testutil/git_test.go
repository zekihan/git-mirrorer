package testutil

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/zekihan/git-mirrorer/internal/safepath"
)

func TestTempDirWithRelativeTMPDIR(t *testing.T) {
	t.Chdir(TempDir(t))
	t.Setenv("TMPDIR", ".")
	t.Run("trusted fixture", func(t *testing.T) {
		dir := TempDir(t)
		if _, err := safepath.Capture(dir); err != nil {
			t.Fatalf("fixture directory %q is unusable: %v", dir, err)
		}
	})
}

func TestIsolateKeepsFixtureGitConfigLocal(t *testing.T) {
	callerConfig := filepath.Join(t.TempDir(), "caller.config")
	const original = "[user]\n\tname = Caller\n\temail = caller@example.invalid\n"
	if err := os.WriteFile(callerConfig, []byte(original), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GIT_CONFIG", callerConfig)
	Isolate(t)
	if _, ok := os.LookupEnv("GIT_CONFIG"); ok {
		t.Error("Isolate left the caller's GIT_CONFIG set")
	}
	repo := Repository(t, false)
	got, err := os.ReadFile(callerConfig)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != original {
		t.Errorf("fixture setup rewrote caller config to %q", got)
	}
	if name := Git(t, "", "-C", repo, "config", "--local", "--get", "user.name"); name != "Mirror Test" {
		t.Errorf("fixture user.name = %q, want Mirror Test", name)
	}
}
