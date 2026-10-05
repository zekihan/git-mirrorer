package git

import (
	"context"
	"errors"
	"math/rand/v2"
	"strings"
	"time"
)

func (c *Client) retry(ctx context.Context, operation func(context.Context) error) error {
	ctx, cancel := context.WithTimeout(ctx, c.runner.Timeout)
	defer cancel()
	for attempt := 0; ; attempt++ {
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
		if err == nil || attempt == 2 || !transientConnectionError(err) {
			return err
		}
		delay := time.Second << attempt
		timer := time.NewTimer(delay + time.Duration(rand.Int64N(int64(delay/2))))
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
}

func transientConnectionError(err error) bool {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	var commandErr *CommandError
	if !errors.As(err, &commandErr) {
		return false
	}
	detail := strings.ToLower(err.Error())
	for _, permanent := range []string{
		"permission denied", "authentication failed", "host key verification failed",
		"repository not found", "remote rejected", "destination rejected refs", "missing necessary objects",
		"atomic transaction failed", "protected branch", "non-fast-forward",
		"object corrupt", "bad object", "invalid sha1", "could not resolve host",
	} {
		if strings.Contains(detail, permanent) {
			return false
		}
	}
	for _, transient := range []string{
		"connection reset", "connection refused", "connection timed out",
		"connection closed", "broken pipe", "network is unreachable",
		"no route to host", "temporary failure in name resolution",
		"remote end hung up unexpectedly", "unexpected disconnect",
		"early eof", "tls connection was non-properly terminated",
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
