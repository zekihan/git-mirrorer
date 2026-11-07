package git

import (
	"context"
	"errors"
	"math/rand/v2"
	"net"
	"net/url"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Connection-phase backoff. A remote that has not answered yet (fresh pod
// before egress rules or DNS are programmed, remote restarting) is retried
// with bounded exponential backoff until its connect budget is spent.
var (
	connectBackoffBase = time.Second
	connectBackoffMax  = 10 * time.Second
	retryBackoffBase   = time.Second
)

// connectTracker remembers, per remote endpoint, when contact was first
// attempted and whether it ever succeeded, so concurrent workers share one
// connect budget instead of each waiting for the full duration.
type connectTracker struct {
	budget time.Duration
	mu     sync.Mutex
	hosts  map[string]*remoteState
}

type remoteState struct {
	first     time.Time
	contacted bool
}

// SetConnectRetry sets how long connection-phase failures (DNS, refused,
// timed out, unreachable, SSH handshake failures) are retried for a remote
// endpoint that has not yet been reached. Zero disables the extended budget.
// Call before using the client concurrently.
func (c *Client) SetConnectRetry(budget time.Duration) {
	if budget <= 0 {
		c.connect = nil
		return
	}
	c.connect = &connectTracker{budget: budget, hosts: map[string]*remoteState{}}
}

func (t *connectTracker) deadline(key string) (time.Time, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	state := t.hosts[key]
	if state == nil {
		state = &remoteState{first: time.Now()}
		t.hosts[key] = state
	}
	return state.first.Add(t.budget), !state.contacted
}

func (t *connectTracker) markContacted(key string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if state := t.hosts[key]; state != nil {
		state.contacted = true
	} else {
		t.hosts[key] = &remoteState{first: time.Now(), contacted: true}
	}
}

// remoteKey returns "host:port" for remote addresses and "" for local paths.
// Addresses were normalized by configuration loading.
func remoteKey(address string) string {
	if address == "" || filepath.IsAbs(address) {
		return ""
	}
	if strings.Contains(address, "://") {
		u, err := url.Parse(address)
		if err != nil || u.Hostname() == "" {
			return ""
		}
		port := u.Port()
		switch u.Scheme {
		case "ssh":
			if port == "" {
				port = "22"
			}
		case "https":
			if port == "" {
				port = "443"
			}
		default:
			return ""
		}
		return net.JoinHostPort(strings.ToLower(u.Hostname()), port)
	}
	// SCP-style: [user@]host:path or [user@][ipv6]:path.
	rest := address
	if at := strings.IndexByte(rest, '@'); at >= 0 && at < strings.IndexAny(rest, ":/") {
		rest = rest[at+1:]
	}
	var host string
	if strings.HasPrefix(rest, "[") {
		end := strings.IndexByte(rest, ']')
		if end < 0 {
			return ""
		}
		host = rest[1:end]
	} else {
		colon := strings.IndexByte(rest, ':')
		if colon < 0 {
			return ""
		}
		host = rest[:colon]
	}
	if host == "" {
		return ""
	}
	return net.JoinHostPort(strings.ToLower(host), "22")
}

// retry runs a network command against remote. Connection-phase failures are
// retried within the remote's connect budget; other recognized transient
// failures get at most two retries. Everything shares the operation timeout.
func (c *Client) retry(ctx context.Context, remote string, operation func(context.Context) error) error {
	ctx, cancel := context.WithTimeout(ctx, c.runner.Timeout)
	defer cancel()
	key := remoteKey(remote)
	tracker := c.connect
	if key == "" {
		tracker = nil
	}
	for transient, connect := 0, 0; ; {
		if err := ctx.Err(); err != nil {
			return err
		}
		err := operation(ctx)
		if ctx.Err() != nil {
			if errors.Is(err, ctx.Err()) {
				return err
			}
			return ctx.Err()
		}
		if err == nil {
			if tracker != nil {
				tracker.markContacted(key)
			}
			return nil
		}
		var delay time.Duration
		switch {
		case tracker != nil && connectFailure(err) && connectBudgetLeft(tracker, key) > 0:
			delay = connectBackoffBase << min(connect, 16)
			if delay > connectBackoffMax || delay <= 0 {
				delay = connectBackoffMax
			}
			connect++
			delay = min(delay+time.Duration(rand.Int64N(int64(delay/2)+1)), connectBudgetLeft(tracker, key))
		case transient < 2 && transientConnectionError(err):
			delay = retryBackoffBase << transient
			transient++
			delay += time.Duration(rand.Int64N(int64(delay / 2)))
		default:
			return err
		}
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
}

func connectBudgetLeft(t *connectTracker, key string) time.Duration {
	deadline, pending := t.deadline(key)
	if !pending {
		return 0
	}
	return time.Until(deadline)
}

var permanentDiagnostics = []string{
	"permission denied", "authentication failed", "host key verification failed",
	"repository not found", "remote rejected", "destination rejected refs", "missing necessary objects",
	"atomic transaction failed", "protected branch", "non-fast-forward",
	"object corrupt", "bad object", "invalid sha1",
	"remote host identification has changed",
}

// diagnostic returns the lowercased failure text of a Git command error that
// carries no permanent marker.
func diagnostic(err error) (string, bool) {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return "", false
	}
	var commandErr *CommandError
	if !errors.As(err, &commandErr) {
		return "", false
	}
	detail := strings.ToLower(err.Error())
	for _, permanent := range permanentDiagnostics {
		if strings.Contains(detail, permanent) {
			return "", false
		}
	}
	return detail, true
}

// connectFailure reports a failure before a session with the remote existed:
// name resolution, TCP connect, or the SSH/TLS handshake. Such a remote may
// simply not be reachable yet.
func connectFailure(err error) bool {
	detail, ok := diagnostic(err)
	if !ok {
		return false
	}
	for _, marker := range []string{
		"could not resolve host", "name or service not known", "name does not resolve", "temporary failure in name resolution",
		"no such host", "connection refused", "connection timed out", "operation timed out",
		"network is unreachable", "no route to host", "failed to connect to", "couldn't connect to server",
		"kex_exchange_identification", "ssh_exchange_identification", "timed out during banner exchange",
		"dial tcp",
	} {
		if strings.Contains(detail, marker) {
			return true
		}
	}
	return false
}

func transientConnectionError(err error) bool {
	detail, ok := diagnostic(err)
	if !ok {
		return false
	}
	for _, transient := range []string{
		"connection reset", "connection refused", "connection timed out",
		"connection closed", "broken pipe", "network is unreachable",
		"no route to host", "temporary failure in name resolution",
		"remote end hung up unexpectedly", "unexpected disconnect",
		"early eof", "tls connection was non-properly terminated",
		"kex_exchange_identification", "ssh_exchange_identification",
		"http 429", "http 500", "http 502", "http 503", "http 504",
		"requested url returned error: 429", "requested url returned error: 500",
		"requested url returned error: 502", "requested url returned error: 503",
		"requested url returned error: 504",
	} {
		if strings.Contains(detail, transient) {
			return true
		}
	}
	return false
}
