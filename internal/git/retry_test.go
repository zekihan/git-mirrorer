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
	fastConnectBackoff(t)
	for _, succeed := range []bool{false, true} {
		c := &Client{runner: &Runner{Timeout: 10 * time.Second}}
		calls := 0
		err := c.retry(t.Context(), "", func(context.Context) error {
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
		err := c.retry(ctx, "", func(context.Context) error {
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
	err := c.retry(t.Context(), "", func(context.Context) error { calls++; return want })
	if calls != 1 || err != want {
		t.Fatalf("calls=%d err=%v", calls, err)
	}
}

func TestPushRetryResetsCounts(t *testing.T) {
	dir := testutil.TempDir(t)
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
	dir := testutil.TempDir(t)
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

func fastConnectBackoff(t *testing.T) {
	t.Helper()
	base, limit, standard := connectBackoffBase, connectBackoffMax, retryBackoffBase
	connectBackoffBase, connectBackoffMax, retryBackoffBase = 2*time.Millisecond, 10*time.Millisecond, 2*time.Millisecond
	t.Cleanup(func() { connectBackoffBase, connectBackoffMax, retryBackoffBase = base, limit, standard })
}

func connectClient(budget time.Duration) *Client {
	c := &Client{runner: &Runner{Timeout: 30 * time.Second}}
	c.SetConnectRetry(budget)
	return c
}

func TestConnectFailureClassification(t *testing.T) {
	for _, tc := range []struct {
		detail string
		want   bool
	}{
		{"ssh: Could not resolve hostname github.com: Temporary failure in name resolution", true},
		{"ssh: Could not resolve hostname github.com: Name or service not known", true},
		{"ssh: Could not resolve hostname gitlab.com: Try again", true},
		{"ssh: Could not resolve hostname gitlab.com: Name does not resolve", true},
		{"fatal: unable to access 'https://x/y.git/': Could not resolve host: x", true},
		{"ssh: connect to host git.noqer.com port 31501: Connection refused", true},
		{"ssh: connect to host github.com port 22: Connection timed out", true},
		{"ssh: connect to host github.com port 22: Network is unreachable", true},
		{"kex_exchange_identification: Connection closed by remote host", true},
		{"kex_exchange_identification: read: Connection reset by peer", true},
		{"Connection timed out during banner exchange", true},
		{"fatal: unable to access 'https://x/': Failed to connect to x port 443 after 3 ms: Couldn't connect to server", true},
		{"dial tcp 1.2.3.4:443: connect: connection refused", true},
		{"Host key verification failed.", false},
		{"@@@ WARNING: REMOTE HOST IDENTIFICATION HAS CHANGED! @@@ Connection refused", false},
		{"git@github.com: Permission denied (publickey).", false},
		{"fatal: Could not read from remote repository.", false},
		{"fatal: early EOF", false},
		{"Connection reset by peer", false},
	} {
		t.Run(tc.detail, func(t *testing.T) {
			if got := connectFailure(connectionError(tc.detail)); got != tc.want {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
		})
	}
	if connectFailure(context.DeadlineExceeded) || connectFailure(errors.New("start git: boom")) {
		t.Fatal("non-command errors are not connection failures")
	}
}

func TestRemoteKey(t *testing.T) {
	for address, want := range map[string]string{
		"git@github.com:owner/repo.git":           "github.com:22",
		"git@GitLab.com:owner/repo.git":           "gitlab.com:22",
		"host:repo.git":                           "host:22",
		"git@[2001:db8::1]:owner/repo.git":        "[2001:db8::1]:22",
		"ssh://git@git.noqer.com:31501/owner/r":   "git.noqer.com:31501",
		"ssh://git@git.noqer.com/owner/r":         "git.noqer.com:22",
		"https://github.com/owner/repo.git":       "github.com:443",
		"https://example.com:8443/owner/repo.git": "example.com:8443",
		"/srv/repos/repo.git":                     "",
		"file:///srv/repos/repo.git":              "",
		"":                                        "",
	} {
		if got := remoteKey(address); got != want {
			t.Errorf("remoteKey(%q) = %q, want %q", address, got, want)
		}
	}
}

func TestConnectRetryOutlastsStandardRetries(t *testing.T) {
	fastConnectBackoff(t)
	c := connectClient(5 * time.Second)
	calls := 0
	err := c.retry(t.Context(), "git@github.com:o/r.git", func(context.Context) error {
		calls++
		if calls == 12 {
			return nil
		}
		return connectionError("ssh: connect to host github.com port 22: Connection timed out")
	})
	if err != nil || calls != 12 {
		t.Fatalf("calls=%d err=%v", calls, err)
	}
}

func TestConnectRetryIsBoundedAndShared(t *testing.T) {
	fastConnectBackoff(t)
	c := connectClient(100 * time.Millisecond)
	refused := func(calls *int) func(context.Context) error {
		return func(context.Context) error {
			*calls++
			return connectionError("ssh: connect to host github.com port 22: Connection refused")
		}
	}
	first, second := 0, 0
	start := time.Now()
	if err := c.retry(t.Context(), "git@github.com:o/a.git", refused(&first)); err == nil {
		t.Fatal("expected failure")
	}
	if elapsed := time.Since(start); elapsed < 100*time.Millisecond || elapsed > 5*time.Second {
		t.Fatalf("elapsed %v", elapsed)
	}
	if first < 4 {
		t.Fatalf("expected many attempts, got %d", first)
	}
	// The host's budget is spent, so another repository there falls back to the standard policy.
	if err := c.retry(t.Context(), "git@github.com:o/b.git", refused(&second)); err == nil {
		t.Fatal("expected failure")
	}
	if second != 3 {
		t.Fatalf("calls=%d, want standard 3", second)
	}
	// Different hosts have independent budgets.
	third := 0
	if err := c.retry(t.Context(), "git@gitlab.com:o/c.git", refused(&third)); err == nil || third < 4 {
		t.Fatalf("calls=%d err=%v", third, err)
	}
}

func TestConnectBudgetEndsAfterFirstContact(t *testing.T) {
	fastConnectBackoff(t)
	c := connectClient(time.Hour)
	if err := c.retry(t.Context(), "git@github.com:o/a.git", func(context.Context) error { return nil }); err != nil {
		t.Fatal(err)
	}
	calls := 0
	err := c.retry(t.Context(), "git@github.com:o/a.git", func(context.Context) error {
		calls++
		return connectionError("ssh: connect to host github.com port 22: Connection refused")
	})
	if err == nil || calls != 3 {
		t.Fatalf("calls=%d err=%v", calls, err)
	}
}

func TestConnectRetryDoesNotRetryPermanentOrLocal(t *testing.T) {
	fastConnectBackoff(t)
	c := connectClient(time.Hour)
	for _, tc := range []struct{ remote, detail string }{
		{"git@github.com:o/r.git", "Host key verification failed. Connection refused"},
		{"git@github.com:o/r.git", "git@github.com: Permission denied (publickey)."},
		{"git@github.com:o/r.git", "ERROR: Repository not found."},
	} {
		calls := 0
		err := c.retry(t.Context(), tc.remote, func(context.Context) error { calls++; return connectionError(tc.detail) })
		if err == nil || calls != 1 {
			t.Fatalf("%q: calls=%d err=%v", tc.detail, calls, err)
		}
	}
	calls := 0
	err := c.retry(t.Context(), "/srv/repo.git", func(context.Context) error {
		calls++
		return connectionError("Connection refused")
	})
	if err == nil || calls != 3 {
		t.Fatalf("local: calls=%d err=%v", calls, err)
	}
}

func TestConnectRetryRespectsOperationTimeoutAndCancellation(t *testing.T) {
	fastConnectBackoff(t)
	c := connectClient(time.Hour)
	c.runner.Timeout = 80 * time.Millisecond
	start := time.Now()
	err := c.retry(t.Context(), "git@github.com:o/r.git", func(context.Context) error {
		return connectionError("ssh: connect to host github.com port 22: Connection refused")
	})
	if !errors.Is(err, context.DeadlineExceeded) || time.Since(start) > 2*time.Second {
		t.Fatalf("err=%v elapsed=%v", err, time.Since(start))
	}
}

func TestSetConnectRetryDisabled(t *testing.T) {
	c := connectClient(time.Hour)
	c.SetConnectRetry(0)
	if c.connect != nil {
		t.Fatal("zero budget must disable connect retries")
	}
}

// A fake ssh transport driven by real Git proves the whole path: Git invokes
// ssh, ssh output reaches the classifier, and the budget keeps retrying.
func TestFetchRetriesUnreachableSSHRemote(t *testing.T) {
	fastConnectBackoff(t)
	dir := testutil.TempDir(t)
	log := filepath.Join(dir, "ssh.log")
	ssh := filepath.Join(dir, "ssh")
	body := `#!/bin/sh
echo "$*" >> "$SSH_LOG"
if [ "$(wc -l < "$SSH_LOG")" -lt 6 ]; then
 echo "ssh: connect to host git.example port 22: Connection refused" >&2
else
 echo "git@git.example: Permission denied (publickey)." >&2
fi
exit 255
`
	if err := os.WriteFile(ssh, []byte(body), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GIT_SSH_COMMAND", ssh)
	t.Setenv("SSH_LOG", log)
	c := newClient(t)
	c.SetConnectRetry(time.Minute)
	repo := filepath.Join(dir, "repo.git")
	testutil.Git(t, dir, "init", "--bare", repo)
	err := c.Fetch(t.Context(), repo, "git@git.example:owner/repo.git")
	if err == nil || !strings.Contains(err.Error(), "Permission denied") {
		t.Fatalf("err=%v", err)
	}
	data, readErr := os.ReadFile(log)
	if readErr != nil {
		t.Fatal(readErr)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 6 || !strings.Contains(lines[0], "BatchMode=yes") || !strings.Contains(lines[0], "ConnectTimeout=20") {
		t.Fatalf("ssh invocations: %q", lines)
	}
}
