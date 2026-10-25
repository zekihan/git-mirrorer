package cache

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/zekihan/git-mirrorer/internal/git"
	"github.com/zekihan/git-mirrorer/internal/testutil"
)

func TestLockHelper(t *testing.T) {
	if os.Getenv("MIRROR_LOCK_HELPER") != "1" {
		return
	}
	lease, err := Acquire(os.Getenv("MIRROR_STATE"), "source")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	defer lease.Close()
	fmt.Println("locked")
	_, _ = bufio.NewReader(os.Stdin).ReadByte()
}

func TestCrossProcessLockAndCrashRecovery(t *testing.T) {
	state := t.TempDir()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.CommandContext(t.Context(), executable, "-test.run=^TestLockHelper$")
	cmd.Env = append(os.Environ(), "MIRROR_LOCK_HELPER=1", "MIRROR_STATE="+state)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	defer stdin.Close()
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cmd.Process.Kill() })
	ready := make(chan string, 1)
	go func() { line, _ := bufio.NewReader(stdout).ReadString('\n'); ready <- line }()
	select {
	case line := <-ready:
		if line != "locked\n" {
			t.Fatalf("helper: %q", line)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("lock helper timeout")
	}
	if lease, err := Acquire(state, "source"); err == nil {
		lease.Close()
		t.Fatal("acquired another process's lock")
	}
	if err := cmd.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	_ = cmd.Wait()
	lease, err := Acquire(state, "source")
	if err != nil {
		t.Fatalf("lock did not recover after crash: %v", err)
	}
	if err := lease.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestClonePublicationAndInvalidEntries(t *testing.T) {
	testutil.Isolate(t)
	source := testutil.Repository(t, false)
	testutil.Commit(t, source, "a", "one")
	client, err := git.New(t.Context(), 10*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	state := t.TempDir()
	lease, err := Acquire(state, source)
	if err != nil {
		t.Fatal(err)
	}
	defer lease.Close()
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := lease.Prepare(ctx, client); err == nil {
		t.Fatal("canceled clone succeeded")
	}
	entries, err := os.ReadDir(filepath.Join(state, "repositories"))
	if err != nil || len(entries) != 0 {
		t.Fatalf("temporary clone leaked: %v %v", entries, err)
	}
	cloned, err := lease.Prepare(t.Context(), client)
	if err != nil || !cloned {
		t.Fatalf("clone: %v %v", cloned, err)
	}
	cloned, err = lease.Prepare(t.Context(), client)
	if err != nil || cloned {
		t.Fatalf("reuse: %v %v", cloned, err)
	}
	target := filepath.Join(lease.Path, "config")
	if err := os.Rename(target, target+".real"); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target+".real", target); err != nil {
		t.Fatal(err)
	}
	if _, err := lease.Prepare(t.Context(), client); err == nil || !strings.Contains(err.Error(), "symlink") {
		t.Fatalf("accepted symlink: %v", err)
	}
	other, err := Acquire(state, source+"-changed")
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()
	if other.Path == lease.Path {
		t.Fatal("source change reused cache identity")
	}
}

func TestRejectSymlinkLocksAndDirectories(t *testing.T) {
	for _, kind := range []string{"lock", "directory"} {
		t.Run(kind, func(t *testing.T) {
			state := t.TempDir()
			if err := os.Mkdir(filepath.Join(state, "locks"), 0o700); err != nil {
				t.Fatal(err)
			}
			target := filepath.Join(state, "locks", Key("source")+".lock")
			if kind == "directory" {
				target = filepath.Join(state, "repositories")
			}
			if err := os.Symlink(t.TempDir(), target); err != nil {
				t.Fatal(err)
			}
			if lease, err := Acquire(state, "source"); err == nil {
				lease.Close()
				t.Fatal("accepted symlink")
			}
		})
	}
}

func TestRejectUntrustedCacheAndExecutableConfig(t *testing.T) {
	testutil.Isolate(t)
	source := testutil.Repository(t, false)
	testutil.Commit(t, source, "a", "one")
	client, err := git.New(t.Context(), 10*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	state := t.TempDir()
	lease, err := Acquire(state, source)
	if err != nil {
		t.Fatal(err)
	}
	defer lease.Close()
	if _, err := lease.Prepare(t.Context(), client); err != nil {
		t.Fatal(err)
	}
	sentinel := filepath.Join(t.TempDir(), "helper-executed")
	testutil.Git(t, lease.Path, "config", "credential.helper", "!touch '"+sentinel+"'")
	for _, target := range []string{state, filepath.Dir(state), filepath.Join(state, "locks"), filepath.Join(state, "repositories"), lease.entry, lease.Path, filepath.Join(lease.Path, "config")} {
		info, err := os.Stat(target)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(target, 0o777); err != nil {
			t.Fatal(err)
		}
		if _, err := lease.Prepare(t.Context(), client); err == nil {
			t.Fatalf("accepted writable %s", target)
		}
		if err := os.Chmod(target, info.Mode()); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := lease.Prepare(t.Context(), client); err == nil || !strings.Contains(err.Error(), "repository-local configuration") {
		t.Fatalf("accepted shell helper: %v", err)
	}
	if _, err := os.Stat(sentinel); !os.IsNotExist(err) {
		t.Fatal("credential helper executed")
	}
	testutil.Git(t, lease.Path, "config", "--unset", "credential.helper")
	if err := os.Rename(lease.entry, lease.entry+".old"); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(lease.entry, 0o700); err != nil {
		t.Fatal(err)
	}
	testutil.Git(t, "", "clone", "--mirror", "--no-local", source, lease.Path)
	data, err := os.ReadFile(filepath.Join(lease.entry+".old", "metadata.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(lease.entry, "metadata.json"), data, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := lease.Prepare(t.Context(), client); err == nil {
		t.Fatal("accepted replaced matching cache")
	}
}
