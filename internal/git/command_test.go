package git

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/zekihan/git-mirrorer/internal/testutil"
)

type readyWriter struct {
	ready  chan int
	buffer bytes.Buffer
	sent   bool
}

func (w *readyWriter) Write(data []byte) (int, error) {
	w.buffer.Write(data)
	if !w.sent && strings.Contains(w.buffer.String(), "\n") {
		pid, _ := strconv.Atoi(strings.TrimSpace(w.buffer.String()))
		w.ready <- pid
		w.sent = true
	}
	return len(data), nil
}

func TestCancellationKillsProcessTree(t *testing.T) {
	for _, deadline := range []bool{false, true} {
		t.Run(fmt.Sprint(deadline), func(t *testing.T) {
			r := &Runner{Executable: "/bin/sh", Timeout: 10 * time.Second, Env: os.Environ()}
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			if deadline {
				r.Timeout = 500 * time.Millisecond
			}
			writer := &readyWriter{ready: make(chan int, 1)}
			done := make(chan error, 1)
			go func() {
				done <- r.Run(ctx, "", nil, writer, "-c", `trap '' TERM; sh -c 'trap "" TERM; echo $$; exec sleep 60' & wait`)
			}()
			var pid int
			select {
			case pid = <-writer.ready:
			case <-time.After(5 * time.Second):
				t.Fatal("child never became ready")
			}
			if !deadline {
				cancel()
			}
			select {
			case err := <-done:
				want := context.Canceled
				if deadline {
					want = context.DeadlineExceeded
				}
				if !errors.Is(err, want) {
					t.Fatalf("got %v, want %v", err, want)
				}
			case <-time.After(3 * time.Second):
				t.Fatal("cancellation hung")
			}
			assertProcessStopped(t, pid)
		})
	}
}

func assertProcessStopped(t *testing.T, pid int) {
	t.Helper()
	if pid <= 0 {
		t.Fatalf("invalid child pid: %d", pid)
	}
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	deadline := time.After(3 * time.Second)
	for {
		if err := syscall.Kill(pid, 0); errors.Is(err, syscall.ESRCH) {
			return
		}
		if stat, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid)); err == nil {
			if _, rest, ok := strings.Cut(string(stat), ") "); ok && strings.HasPrefix(rest, "Z ") {
				return
			}
		}
		// A killed orphan may be briefly unreaped by PID 1 in a container.
		output, _ := exec.Command("ps", "-o", "stat=", "-p", strconv.Itoa(pid)).Output()
		if strings.HasPrefix(strings.TrimSpace(string(output)), "Z") {
			return
		}
		select {
		case <-ticker.C:
		case <-deadline:
			t.Fatalf("helper process %d survived cancellation", pid)
		}
	}
}

func TestParentExitWithInheritedPipes(t *testing.T) {
	r := &Runner{Executable: "/bin/sh", Timeout: 5 * time.Second, Env: os.Environ()}
	writer := &readyWriter{ready: make(chan int, 1)}
	err := r.Run(t.Context(), "", nil, writer, "-c", `sh -c 'echo $$; exec sleep 60' & exit 0`)
	if !errors.Is(err, exec.ErrWaitDelay) {
		t.Fatalf("inherited pipe cleanup: %v", err)
	}
	select {
	case pid := <-writer.ready:
		assertProcessStopped(t, pid)
	default:
		t.Fatal("missing helper pid")
	}
}

func TestCommandEnvironmentAndSSHConfiguration(t *testing.T) {
	testutil.Isolate(t)
	global := filepath.Join(t.TempDir(), "gitconfig")
	if err := os.WriteFile(global, []byte("[core]\nsshCommand = ssh -p 2222\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GIT_CONFIG_GLOBAL", global)
	t.Setenv("GIT_DIR", "/invalid")
	t.Setenv("GIT_NAMESPACE", "wrong")
	c := newClient(t)
	env := strings.Join(c.runner.Env, "\n")
	if strings.Contains(env, "GIT_DIR=") || strings.Contains(env, "GIT_NAMESPACE=") || !strings.Contains(env, "GIT_SSH_COMMAND=ssh -p 2222 -o BatchMode=yes") {
		t.Fatal("environment was not isolated or SSH config lost")
	}
	t.Setenv("GIT_SSH_COMMAND", "ssh -i '/path with spaces/key'")
	c = newClient(t)
	if !strings.Contains(strings.Join(c.runner.Env, "\n"), "GIT_SSH_COMMAND=ssh -i '/path with spaces/key' -o BatchMode=yes") {
		t.Fatal("explicit SSH command lost")
	}
}
