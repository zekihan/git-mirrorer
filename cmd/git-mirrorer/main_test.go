package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/zekihan/git-mirrorer/internal/mirror"
	"github.com/zekihan/git-mirrorer/internal/testutil"
)

func writeConfig(t *testing.T, source, dest string) (string, string) {
	t.Helper()
	dir := t.TempDir()
	filename := filepath.Join(dir, "config.yaml")
	state := filepath.Join(dir, "state")
	content := fmt.Sprintf("stateDir: %q\nrepositories:\n  - name: source\n    source: %q\n    destinations:\n      - name: destination\n        url: %q\n", state, source, dest)
	if err := os.WriteFile(filename, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return filename, state
}

func TestCLIReportsAndExitCodes(t *testing.T) {
	testutil.Isolate(t)
	source, dest := testutil.Repository(t, false), testutil.Repository(t, true)
	testutil.Commit(t, source, "a", "one")
	filename, _ := writeConfig(t, source, dest)
	var stdout, stderr bytes.Buffer
	code := run(t.Context(), []string{"--config", filename, "--format", "json"}, &stdout, &stderr)
	var report mirror.Report
	if err := json.Unmarshal(stdout.Bytes(), &report); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, &stdout)
	}
	if code != 0 || report.Totals.Succeeded != 1 || report.SchemaVersion != 1 {
		t.Fatalf("code=%d report=%+v stderr=%s", code, report, &stderr)
	}
	if strings.Contains(stdout.String(), "Refreshing") || !strings.Contains(stderr.String(), "Refreshing") {
		t.Fatal("logs and report not separated")
	}
	stdout.Reset()
	stderr.Reset()
	if code := run(t.Context(), []string{"--config", filename}, &stdout, &stderr); code != 0 || !strings.Contains(stdout.String(), "succeeded") {
		t.Fatalf("text: %d %s", code, &stdout)
	}
	if err := os.Rename(source, source+"-offline"); err != nil {
		t.Fatal(err)
	}
	stdout.Reset()
	stderr.Reset()
	if code := run(t.Context(), []string{"--config", filename, "--format", "json"}, &stdout, &stderr); code != 1 {
		t.Fatalf("failure code=%d", code)
	}
	if err := json.Unmarshal(stdout.Bytes(), &report); err != nil || report.Totals.Skipped != 1 {
		t.Fatalf("failure report: %v %+v", err, report)
	}
}

func TestCLIValidationAndNoLFSDependency(t *testing.T) {
	testutil.Isolate(t)
	source, dest := testutil.Repository(t, false), testutil.Repository(t, true)
	testutil.Commit(t, source, "a", "one")
	filename, state := writeConfig(t, source, dest)
	gitPath, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	bin := t.TempDir()
	t.Setenv("PATH", bin)
	var stdout, stderr bytes.Buffer
	if code := run(t.Context(), []string{"--config", filename, "--check"}, &stdout, &stderr); code != 0 {
		t.Fatalf("offline check: %d %s", code, &stderr)
	}
	if _, err := os.Stat(state); !os.IsNotExist(err) {
		t.Fatal("check created state")
	}
	if code := run(t.Context(), []string{"--config", filename}, &stdout, &stderr); code != 2 {
		t.Fatalf("missing git: %d", code)
	}
	if err := os.Symlink(gitPath, filepath.Join(bin, "git")); err != nil {
		t.Fatal(err)
	}
	stdout.Reset()
	stderr.Reset()
	if code := run(t.Context(), []string{"--config", filename}, &stdout, &stderr); code != 0 {
		t.Fatalf("ordinary Git needs LFS: %d %s %s", code, &stdout, &stderr)
	}
	content, err := os.ReadFile(filename)
	if err != nil {
		t.Fatal(err)
	}
	content = []byte(strings.Replace(string(content), "    source:", "    lfs: true\n    source:", 1))
	if err := os.WriteFile(filename, content, 0o600); err != nil {
		t.Fatal(err)
	}
	if code := run(t.Context(), []string{"--config", filename}, &stdout, &stderr); code != 2 {
		t.Fatalf("missing LFS: %d", code)
	}
}

func TestCLIInvalidFlagsAndConfig(t *testing.T) {
	for _, args := range [][]string{{"--unknown"}, {"positional"}, {"--format", "xml"}, {"--check", "--version"}, {"--config", "/nonexistent/config"}} {
		var output bytes.Buffer
		if code := run(t.Context(), args, &output, &output); code != 2 {
			t.Errorf("%v: code %d", args, code)
		}
	}
	for _, flag := range []string{"--help", "--version"} {
		var output bytes.Buffer
		if code := run(t.Context(), []string{flag}, &output, &output); code != 0 || output.Len() == 0 {
			t.Errorf("%s: %d %q", flag, code, &output)
		}
	}
}

func TestCLIHelper(t *testing.T) {
	if os.Getenv("MIRROR_CLI_HELPER") != "1" {
		return
	}
	os.Args = []string{"git-mirrorer", "--config", os.Getenv("MIRROR_CONFIG"), "--format", "json"}
	os.Exit(mainWithSignals())
}

func TestSignalExitCodesAndCloneCleanup(t *testing.T) {
	testutil.Isolate(t)
	for _, signal := range []syscall.Signal{syscall.SIGINT, syscall.SIGTERM} {
		t.Run(signal.String(), func(t *testing.T) {
			dir := t.TempDir()
			ready := filepath.Join(dir, "ready")
			ssh := filepath.Join(dir, "ssh")
			if err := os.WriteFile(ssh, []byte("#!/bin/sh\ntrap '' TERM\necho $$ > \"$MIRROR_READY\"\nexec sleep 60\n"), 0o700); err != nil {
				t.Fatal(err)
			}
			filename, state := writeConfig(t, "ssh://git@example.invalid/repo.git", filepath.Join(dir, "dest"))
			executable, err := os.Executable()
			if err != nil {
				t.Fatal(err)
			}
			cmd := exec.Command(executable, "-test.run=^TestCLIHelper$")
			cmd.Env = append(os.Environ(), "MIRROR_CLI_HELPER=1", "MIRROR_CONFIG="+filename, "MIRROR_READY="+ready, "GIT_SSH_COMMAND="+ssh)
			var stdout, stderr bytes.Buffer
			cmd.Stdout, cmd.Stderr = &stdout, &stderr
			if err := cmd.Start(); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = cmd.Process.Kill() })
			waitForFile(t, ready)
			if err := cmd.Process.Signal(signal); err != nil {
				t.Fatal(err)
			}
			done := make(chan error, 1)
			go func() { done <- cmd.Wait() }()
			select {
			case <-done:
			case <-time.After(5 * time.Second):
				t.Fatal("CLI failed to exit on signal")
			}
			want := 128 + int(signal)
			if cmd.ProcessState.ExitCode() != want {
				t.Fatalf("exit=%d want=%d stderr=%s", cmd.ProcessState.ExitCode(), want, &stderr)
			}
			var result mirror.Report
			if err := json.Unmarshal(stdout.Bytes(), &result); err != nil || result.Totals.Canceled != 1 {
				t.Fatalf("signal report: %v %s", err, &stdout)
			}
			entries, err := os.ReadDir(filepath.Join(state, "repositories"))
			if err != nil || len(entries) != 0 {
				t.Fatalf("interrupted clone leaked: %v %v", entries, err)
			}
			pidBytes, _ := os.ReadFile(ready)
			pid, _ := strconv.Atoi(strings.TrimSpace(string(pidBytes)))
			if pid <= 0 {
				t.Fatal("missing SSH pid")
			}
		})
	}
}

func waitForFile(t *testing.T, filename string) {
	t.Helper()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	for {
		if data, err := os.ReadFile(filename); err == nil && len(data) > 0 {
			return
		}
		select {
		case <-ticker.C:
		case <-ctx.Done():
			t.Fatal("helper never became ready")
		}
	}
}
