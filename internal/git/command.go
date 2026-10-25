package git

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/zekihan/git-mirrorer/internal/redact"
)

const diagnosticLimit = 32 * 1024

type CommandError struct {
	ExitCode int
	Detail   string
	Cause    error
}

func (e *CommandError) Error() string {
	if e.Detail == "" {
		return e.Cause.Error()
	}
	return fmt.Sprintf("%v: %s", e.Cause, e.Detail)
}

func (e *CommandError) Unwrap() error { return e.Cause }

type Runner struct {
	Executable string
	Timeout    time.Duration
	Env        []string
}

func New(ctx context.Context, timeout time.Duration) (*Client, error) {
	path, err := exec.LookPath("git")
	if err != nil {
		return nil, errors.New("git executable is required on PATH")
	}
	runner := &Runner{Executable: path, Timeout: timeout, Env: cleanEnvironment(os.Environ())}
	ssh := os.Getenv("GIT_SSH_COMMAND")
	if ssh == "" {
		if path := os.Getenv("GIT_SSH"); path != "" {
			ssh = "'" + strings.ReplaceAll(path, "'", "'\"'\"'") + "'"
		} else {
			var output bytes.Buffer
			err = runner.Run(ctx, "", nil, &output, "config", "--get", "core.sshCommand")
			var commandErr *CommandError
			if err != nil && (!errors.As(err, &commandErr) || commandErr.ExitCode != 1) {
				return nil, fmt.Errorf("read SSH configuration: %w", err)
			}
			ssh = strings.TrimSpace(output.String())
		}
	}
	if ssh == "" {
		ssh = "ssh"
	}
	// A supplied command is trusted shell syntax, as in Git itself. Preserve its
	// options and make noninteractive OpenSSH behavior explicit.
	runner.Env = append(runner.Env, "GIT_SSH_COMMAND="+ssh+" -o BatchMode=yes", "GIT_SSH_VARIANT=ssh")
	return &Client{runner: runner}, nil
}

func cleanEnvironment(env []string) []string {
	blocked := map[string]bool{
		"GIT_DIR": true, "GIT_WORK_TREE": true, "GIT_COMMON_DIR": true,
		"GIT_NAMESPACE": true, "GIT_INDEX_FILE": true, "GIT_OBJECT_DIRECTORY": true,
		"GIT_ALTERNATE_OBJECT_DIRECTORIES": true, "GIT_CONFIG": true,
		"GIT_CONFIG_PARAMETERS": true, "GIT_CONFIG_COUNT": true,
		"GIT_SSH_COMMAND": true, "GIT_SSH_VARIANT": true, "GIT_SSH": true,
		"GIT_TERMINAL_PROMPT": true, "GCM_INTERACTIVE": true, "LC_ALL": true,
		"GIT_LFS_SKIP_SMUDGE": true,
		"GIT_LFS_SKIP_PUSH":   true, "GIT_LFS_SKIP_DOWNLOAD_ERRORS": true,
	}
	result := make([]string, 0, len(env)+5)
	for _, entry := range env {
		name, _, _ := strings.Cut(entry, "=")
		if !blocked[name] && !strings.HasPrefix(name, "GIT_CONFIG_KEY_") && !strings.HasPrefix(name, "GIT_CONFIG_VALUE_") {
			result = append(result, entry)
		}
	}
	return append(result, "GIT_TERMINAL_PROMPT=0", "GCM_INTERACTIVE=never", "LC_ALL=C", "GIT_LFS_SKIP_SMUDGE=1")
}

func (r *Runner) Run(ctx context.Context, dir string, input io.Reader, output io.Writer, args ...string) error {
	ctx, cancel := context.WithTimeout(ctx, r.Timeout)
	defer cancel()
	if err := ctx.Err(); err != nil {
		return err
	}
	command := exec.Command(r.Executable, args...)
	command.Dir, command.Env, command.Stdin = dir, r.Env, input
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	command.WaitDelay = 250 * time.Millisecond
	tail := &tailBuffer{}
	command.Stderr = tail
	command.Stdout = tail
	if output != nil {
		command.Stdout = io.MultiWriter(output, tail)
	}
	if err := command.Start(); err != nil {
		return fmt.Errorf("start git: %w", err)
	}
	done := make(chan struct{})
	var waitErr error
	go func() {
		waitErr = command.Wait()
		close(done)
	}()
	var err error
	select {
	case <-done:
		err = waitErr
		if err != nil || ctx.Err() != nil {
			_ = syscall.Kill(-command.Process.Pid, syscall.SIGKILL)
		}
	case <-ctx.Done():
		_ = syscall.Kill(-command.Process.Pid, syscall.SIGTERM)
		timer := time.NewTimer(200 * time.Millisecond)
		select {
		case <-done:
		case <-timer.C:
		}
		timer.Stop()
		// The parent may already have exited while helpers still own its pipes.
		_ = syscall.Kill(-command.Process.Pid, syscall.SIGKILL)
		// WaitDelay bounds pipe cleanup. Cancellation is authoritative, but
		// the child still must be reaped before returning.
		<-done
		err = ctx.Err()
	}
	if ctx.Err() != nil {
		err = ctx.Err()
	}
	if err == nil {
		return nil
	}
	exitCode := -1
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		exitCode = exitErr.ExitCode()
	}
	return &CommandError{ExitCode: exitCode, Detail: redact.Text(strings.TrimSpace(tail.String())), Cause: err}
}

type tailBuffer struct {
	mu      sync.Mutex
	data    []byte
	partial bool // the retained bytes start inside a truncated LF-delimited record
}

func (b *tailBuffer) Write(data []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	n := len(data)
	// Track the byte immediately before the retained tail. Redaction requires
	// the entire record, including any credential label or URL scheme.
	if overflow := len(b.data) + n - diagnosticLimit; overflow > 0 {
		if overflow <= len(b.data) {
			b.partial = b.data[overflow-1] != '\n'
		} else {
			b.partial = data[overflow-len(b.data)-1] != '\n'
		}
	}
	if len(data) >= diagnosticLimit {
		b.data = append(b.data[:0], data[len(data)-diagnosticLimit:]...)
	} else {
		if overflow := len(b.data) + len(data) - diagnosticLimit; overflow > 0 {
			b.data = append(b.data[:0], b.data[overflow:]...)
		}
		b.data = append(b.data, data...)
	}
	return n, nil
}

func (b *tailBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	data := b.data
	if b.partial {
		// A long record without a newline is omitted entirely. Never expose a
		// suffix whose redaction context may have been discarded.
		if end := bytes.IndexByte(data, '\n'); end >= 0 {
			data = data[end+1:]
		} else {
			return ""
		}
	}
	return string(data)
}
