package testutil

import (
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

func TempDir(t *testing.T) string {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for path := dir; ; path = filepath.Dir(path) {
		info, statErr := os.Stat(path)
		if statErr != nil {
			t.Fatal(statErr)
		}
		stat, ok := info.Sys().(*syscall.Stat_t)
		if !ok || (stat.Uid != uint32(os.Geteuid()) && stat.Uid != 0) || info.Mode().Perm()&0o022 != 0 {
			account, userErr := user.Current()
			if userErr != nil {
				t.Fatal(userErr)
			}
			home, homeErr := filepath.EvalSymlinks(account.HomeDir)
			if homeErr != nil {
				t.Fatal(homeErr)
			}
			dir, homeErr = os.MkdirTemp(home, ".git-mirrorer-test-")
			if homeErr != nil {
				t.Fatal(homeErr)
			}
			t.Cleanup(func() { _ = os.RemoveAll(dir) })
			return dir
		}
		if path == filepath.Dir(path) {
			return dir
		}
	}
}

func Isolate(t *testing.T) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_COUNT", "0")
	t.Setenv("GIT_CONFIG_PARAMETERS", "")
	for _, name := range []string{"GIT_DIR", "GIT_WORK_TREE", "GIT_COMMON_DIR", "GIT_NAMESPACE", "GIT_OBJECT_DIRECTORY", "GIT_ALTERNATE_OBJECT_DIRECTORIES", "GIT_INDEX_FILE", "GIT_SSH_COMMAND", "GIT_SSH"} {
		t.Setenv(name, "")
		if err := os.Unsetenv(name); err != nil {
			t.Fatal(err)
		}
	}
}

func Git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), "git", args...)
	cmd.Dir = dir
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, output)
	}
	return strings.TrimSpace(string(output))
}

func Repository(t *testing.T, bare bool) string {
	t.Helper()
	dir := TempDir(t)
	args := []string{"init", "--initial-branch=main"}
	if bare {
		args = append(args, "--bare")
	}
	Git(t, dir, args...)
	Git(t, dir, "config", "user.name", "Mirror Test")
	Git(t, dir, "config", "user.email", "mirror@example.invalid")
	return dir
}

func Commit(t *testing.T, dir, name, contents string) string {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
	Git(t, dir, "add", "--", name)
	Git(t, dir, "commit", "--no-gpg-sign", "-m", "Update fixture")
	return Git(t, dir, "rev-parse", "HEAD")
}

func Refs(t *testing.T, dir string) string {
	t.Helper()
	return Git(t, dir, "for-each-ref", "--format=%(refname) %(objectname)", "refs/")
}

func EqualRefs(t *testing.T, source, destination string) {
	t.Helper()
	want, got := Refs(t, source), Refs(t, destination)
	if want != got {
		t.Fatalf("refs differ\nsource:\n%s\ndestination:\n%s", want, got)
	}
}
