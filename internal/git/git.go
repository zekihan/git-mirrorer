package git

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/zekihan/git-mirrorer/internal/cachefs"
)

type Client struct{ runner *Runner }

type Changes struct {
	Created   int `json:"created"`
	Updated   int `json:"updated"`
	Deleted   int `json:"deleted"`
	Unchanged int `json:"unchanged"`
	Rejected  int `json:"rejected"`
}

func (c *Client) command(ctx context.Context, dir string, input io.Reader, output io.Writer, args ...string) error {
	options := []string{"-c", "maintenance.auto=false", "-c", "gc.auto=0", "-c", "submodule.recurse=false"}
	return c.runner.Run(ctx, dir, input, output, append(options, args...)...)
}

func (c *Client) CheckLFS(ctx context.Context) error {
	return c.command(ctx, "", nil, nil, "lfs", "version")
}

func (c *Client) Clone(ctx context.Context, source, path string) error {
	if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
		return errors.New("clone destination must not exist")
	}
	first := true
	return c.retry(ctx, func(ctx context.Context) error {
		if !first {
			cleanupCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
			defer cancel()
			if err := cachefs.RemoveTree(cleanupCtx, path, 1_000_000); err != nil {
				return err
			}
		}
		first = false
		return c.command(ctx, "", nil, nil, "clone", "--mirror", "--no-local", "--config", "core.hooksPath="+os.DevNull, "--", source, path)
	})
}

func (c *Client) Validate(ctx context.Context, path, source string) error {
	var output bytes.Buffer
	if err := c.command(ctx, path, nil, &output, "rev-parse", "--is-bare-repository", "--is-shallow-repository"); err != nil {
		return err
	}
	if output.String() != "true\nfalse\n" {
		return errors.New("cache must be a full bare repository")
	}
	output.Reset()
	if err := c.command(ctx, path, nil, &output, "config", "--get", "remote.origin.url"); err != nil {
		return err
	}
	if strings.TrimSpace(output.String()) != source {
		return errors.New("cache origin does not match configured source")
	}
	output.Reset()
	err := c.command(ctx, path, nil, &output, "config", "--get-regexp", `^(extensions\.partialclone|remote\..*\.promisor)$`)
	var commandErr *CommandError
	if err == nil || output.Len() > 0 {
		return errors.New("partial/promisor caches are unsupported")
	}
	if !errors.As(err, &commandErr) || commandErr.ExitCode != 1 {
		return err
	}
	if _, err := os.Stat(filepath.Join(path, "objects", "info", "alternates")); err == nil || !errors.Is(err, os.ErrNotExist) {
		return errors.New("cache object alternates are unsupported")
	}
	return c.command(ctx, path, nil, nil, "config", "core.hooksPath", os.DevNull)
}

func (c *Client) Fetch(ctx context.Context, path, source string) error {
	return c.retry(ctx, func(ctx context.Context) error {
		return c.command(ctx, path, nil, nil, "fetch", "--atomic", "--prune", "--no-tags", "--no-recurse-submodules", "--", source, "+refs/*:refs/*")
	})
}

func (c *Client) Push(ctx context.Context, path, destination string, atomic bool) (Changes, error) {
	var local bytes.Buffer
	if err := c.command(ctx, path, nil, &local, "for-each-ref", "--count=1", "--format=%(refname)", "refs/"); err != nil {
		return Changes{}, err
	}
	if local.Len() == 0 {
		// Git reports an error for an empty-to-empty push. Verify that no refs
		// need deleting instead of treating that already-matching state as failure.
		remote := &nonemptyWriter{}
		if err := c.retry(ctx, func(ctx context.Context) error {
			remote.nonempty = false
			return c.command(ctx, path, nil, remote, "ls-remote", "--refs", "--", destination)
		}); err != nil {
			return Changes{}, err
		}
		if !remote.nonempty {
			return Changes{}, nil
		}
	}
	args := []string{"push", "--mirror", "--porcelain", "--no-recurse-submodules"}
	if atomic {
		args = append(args, "--atomic")
	}
	args = append(args, "--", destination)
	var parser *pushParser
	err := c.retry(ctx, func(ctx context.Context) error {
		parser = &pushParser{}
		err := c.command(ctx, path, nil, parser, args...)
		parser.finish()
		if parser.changes.Rejected > 0 {
			if err != nil {
				return fmt.Errorf("destination rejected refs: %w", err)
			}
			return errors.New("destination rejected refs")
		}
		return err
	})
	if parser == nil {
		return Changes{}, err
	}
	return parser.changes, err
}

type nonemptyWriter struct{ nonempty bool }

func (w *nonemptyWriter) Write(data []byte) (int, error) {
	w.nonempty = w.nonempty || len(data) > 0
	return len(data), nil
}

// Parse incrementally, retaining only the status prefix of each porcelain line.
type pushParser struct {
	changes Changes
	prefix  [2]byte
	length  int
}

func (p *pushParser) Write(data []byte) (int, error) {
	for _, b := range data {
		if b == '\n' {
			p.finish()
			continue
		}
		if p.length < len(p.prefix) {
			p.prefix[p.length] = b
			p.length++
		}
	}
	return len(data), nil
}

func (p *pushParser) finish() {
	if p.length == 2 && p.prefix[1] == '\t' {
		switch p.prefix[0] {
		case '*':
			p.changes.Created++
		case '+', ' ':
			p.changes.Updated++
		case '-':
			p.changes.Deleted++
		case '=':
			p.changes.Unchanged++
		case '!':
			p.changes.Rejected++
		}
	}
	p.length = 0
}

func (c *Client) LFSFetch(ctx context.Context, path, source string) error {
	if err := c.prepareLFS(ctx, path); err != nil {
		return err
	}
	return c.retry(ctx, func(ctx context.Context) error {
		return c.command(ctx, path, nil, nil, "-c", "remote.mirrorer-lfs.url="+source,
			"-c", "lfs.concurrenttransfers=1",
			"-c", "lfs.skipdownloaderrors=false", "lfs", "fetch", "--all", "mirrorer-lfs")
	})
}

func (c *Client) LFSPush(ctx context.Context, path, destination string) error {
	if err := c.prepareLFS(ctx, path); err != nil {
		return err
	}
	// A temporary ref list bounds memory and avoids platform argument-size limits.
	refs, err := os.CreateTemp(filepath.Dir(path), ".lfs-refs-*")
	if err != nil {
		return err
	}
	defer os.Remove(refs.Name())
	defer refs.Close()
	if err := c.command(ctx, path, nil, refs, "for-each-ref", "--format=%(refname)", "refs/"); err != nil {
		return err
	}
	size, err := refs.Seek(0, io.SeekCurrent)
	if err != nil {
		return err
	}
	if size == 0 {
		return nil
	}
	return c.retry(ctx, func(ctx context.Context) error {
		if _, err := refs.Seek(0, io.SeekStart); err != nil {
			return err
		}
		return c.command(ctx, path, refs, nil, "-c", "remote.mirrorer-lfs.url="+destination,
			"-c", "lfs.concurrenttransfers=1",
			"-c", "lfs.allowincompletepush=false", "lfs", "push", "--all", "mirrorer-lfs", "--stdin")
	})
}

func (c *Client) prepareLFS(ctx context.Context, path string) error {
	if err := c.checkLFSConfig(ctx, path); err != nil {
		return err
	}
	// Command-scoped storage config also reaches the standalone-file transport's
	// remote process. Store it locally so remote payload paths remain independent.
	return c.command(ctx, path, nil, nil, "config", "--local", "lfs.storage", filepath.Join(path, "lfs"))
}

func (c *Client) checkLFSConfig(ctx context.Context, path string) error {
	var output bytes.Buffer
	err := c.command(ctx, path, nil, &output, "config", "--name-only", "--get-regexp",
		`^(lfs\.(url|pushurl|standalonetransferagent|customtransfer\..*)|remote\..*\.(lfsurl|lfspushurl|pushurl))$`)
	var commandErr *CommandError
	if err == nil {
		return errors.New("custom LFS endpoint/transfer configuration is unsupported")
	}
	if !errors.As(err, &commandErr) || commandErr.ExitCode != 1 {
		return err
	}
	// Git LFS can read .lfsconfig from HEAD even in a bare repository.
	output.Reset()
	err = c.command(ctx, path, nil, &output, "cat-file", "-e", "HEAD:.lfsconfig")
	if err == nil {
		return errors.New("repository .lfsconfig is unsupported; use standard LFS endpoint discovery")
	}
	if !errors.As(err, &commandErr) || commandErr.ExitCode != 128 {
		return fmt.Errorf("check LFS repository configuration: %w", err)
	}
	return nil
}

// Reclaim runs only while the caller holds the source lease, before fan-out.
// No external Git process may write the managed cache.
func (c *Client) Reclaim(ctx context.Context, path string) error {
	if err := c.command(ctx, path, nil, nil, "reflog", "expire", "--expire=now", "--all"); err != nil {
		return err
	}
	return c.command(ctx, path, nil, nil, "-c", "pack.threads=1", "-c", "pack.windowMemory=64m", "-c", "gc.cruftPacks=false", "-c", "gc.autoDetach=false", "-c", "gc.detach=false", "gc", "--prune=now")
}
