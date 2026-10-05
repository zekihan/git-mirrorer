package git

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func connectionError(detail string) error {
	return &CommandError{ExitCode: 128, Detail: detail, Cause: errors.New("exit status 128")}
}

func TestTransientConnectionError(t *testing.T) {
	for _, tc := range []struct {
		detail string
		want   bool
	}{
		{"ssh: connect to host example port 22: Connection timed out", true},
		{"fatal: early EOF", true},
		{"fatal: unable to access URL: The requested URL returned error: 503", true},
		{"Temporary failure in name resolution", true},
		{"Permission denied (publickey). Connection closed", false},
		{"[remote rejected] (missing necessary objects)", false},
		{"fatal: bad object; remote end hung up unexpectedly", false},
		{"fatal: Could not read from remote repository.", false},
		{"Authentication failed", false},
	} {
		t.Run(tc.detail, func(t *testing.T) {
			if got := transientConnectionError(connectionError(tc.detail)); got != tc.want {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
		})
	}
}

func TestRetryBoundAndRecovery(t *testing.T) {
	for _, succeed := range []bool{false, true} {
		c := &Client{runner: &Runner{Timeout: 10 * time.Second}}
		calls := 0
		err := c.retry(t.Context(), func(context.Context) error {
			calls++
			if succeed && calls == 3 {
				return nil
			}
			return connectionError("Connection reset by peer")
		})
		if calls != 3 || (err == nil) != succeed {
			t.Fatalf("calls=%d err=%v", calls, err)
		}
	}
}

func TestRetryDeadlineAndCancellation(t *testing.T) {
	for _, canceled := range []bool{false, true} {
		ctx, cancel := context.WithCancel(t.Context())
		c := &Client{runner: &Runner{Timeout: 50 * time.Millisecond}}
		calls := 0
		start := time.Now()
		err := c.retry(ctx, func(context.Context) error {
			calls++
			if canceled {
				cancel()
			}
			return connectionError("Connection refused")
		})
		cancel()
		want := context.DeadlineExceeded
		if canceled {
			want = context.Canceled
		}
		if !errors.Is(err, want) || calls != 1 || time.Since(start) > time.Second {
			t.Fatalf("calls=%d err=%v", calls, err)
		}
	}
}

func TestRetryPermanentFailure(t *testing.T) {
	c := &Client{runner: &Runner{Timeout: time.Second}}
	calls := 0
	want := connectionError("missing necessary objects")
	err := c.retry(t.Context(), func(context.Context) error { calls++; return want })
	if calls != 1 || err != want {
		t.Fatalf("calls=%d err=%v", calls, err)
	}
}

func TestPushRetryResetsCounts(t *testing.T) {
	dir := t.TempDir()
	script := filepath.Join(dir, "git")
	state := filepath.Join(dir, "attempt")
	body := `#!/bin/sh
case "$*" in
 *for-each-ref*) echo refs/heads/main; exit 0;;
esac
if [ ! -f "$ATTEMPT_FILE" ]; then
 touch "$ATTEMPT_FILE"
 printf '*\trefs/heads/main:refs/heads/main\t[new branch]\n'
 echo 'Connection reset by peer' >&2
 exit 128
fi
printf '=\trefs/heads/main:refs/heads/main\t[up to date]\n'
`
	if err := os.WriteFile(script, []byte(body), 0o700); err != nil {
		t.Fatal(err)
	}
	c := &Client{runner: &Runner{Executable: script, Timeout: 5 * time.Second, Env: append(os.Environ(), "ATTEMPT_FILE="+state)}}
	changes, err := c.Push(t.Context(), dir, "unused", true)
	if err != nil || changes.Created != 0 || changes.Unchanged != 1 {
		t.Fatalf("changes=%+v err=%v", changes, err)
	}
}

func TestCloneRetryCleansPartialDestination(t *testing.T) {
	dir := t.TempDir()
	script := filepath.Join(dir, "git")
	destination := filepath.Join(dir, "clone")
	state := filepath.Join(dir, "attempt")
	body := `#!/bin/sh
for arg do destination="$arg"; done
if [ ! -f "$ATTEMPT_FILE" ]; then
 touch "$ATTEMPT_FILE"
 mkdir -p "$destination"
 touch "$destination/partial"
 echo 'Connection reset by peer' >&2
 exit 128
fi
if [ -e "$destination" ]; then echo 'partial destination survived' >&2; exit 1; fi
mkdir -p "$destination"
`
	if err := os.WriteFile(script, []byte(body), 0o700); err != nil {
		t.Fatal(err)
	}
	c := &Client{runner: &Runner{Executable: script, Timeout: 5 * time.Second, Env: append(os.Environ(), "ATTEMPT_FILE="+state)}}
	if err := c.Clone(t.Context(), "unused", destination); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(destination); err != nil {
		t.Fatal(err)
	}
	if err := c.Clone(t.Context(), "unused", destination); err == nil {
		t.Fatal("existing destination was accepted")
	}
}
