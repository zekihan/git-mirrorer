package git

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/zekihan/git-mirrorer/internal/safepath"
	"time"

	"github.com/zekihan/git-mirrorer/internal/cachefs"
)

type Client struct {
	runner         *Runner
	connect        *connectTracker
	transientRetry time.Duration
}

type Changes struct {
	Created   int `json:"created"`
	Updated   int `json:"updated"`
	Deleted   int `json:"deleted"`
	Unchanged int `json:"unchanged"`
	Rejected  int `json:"rejected"`
}

func (c *Client) command(ctx context.Context, dir string, input io.Reader, output io.Writer, args ...string) error {
	options := []string{"-c", "maintenance.auto=false", "-c", "gc.auto=0", "-c", "submodule.recurse=false", "-c", "core.hooksPath=" + os.DevNull}
	return c.runner.Run(ctx, dir, input, output, append(options, args...)...)
}

func (c *Client) CheckLFS(ctx context.Context) error {
	return c.command(ctx, "", nil, nil, "lfs", "version")
}

func (c *Client) Clone(ctx context.Context, source, path string) error {
	if err := checkLocalEndpoint(source); err != nil {
		return err
	}
	if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
		return errors.New("clone destination must not exist")
	}
	first := true
	return c.retry(ctx, source, func(ctx context.Context) error {
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
	err := c.command(ctx, path, nil, &output, "config", "--local", "--no-includes", "--name-only", "--get-regexp", `^(credential\..*|include.*|core\.(askpass|sshcommand|gitproxy|fsmonitor)|filter\..*|remote\..*\.(uploadpack|receivepack|vcs)|url\..*|alias\..*)$`)
	var unsafeErr *CommandError
	if err == nil || output.Len() > 0 {
		return errors.New("executable or redirecting repository-local configuration is unsupported")
	}
	if !errors.As(err, &unsafeErr) || unsafeErr.ExitCode != 1 {
		return err
	}
	output.Reset()
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
	err = c.command(ctx, path, nil, &output, "config", "--get-regexp", `^(extensions\.partialclone|remote\..*\.promisor)$`)
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
	if err := checkLocalEndpoint(source); err != nil {
		return err
	}
	return c.retry(ctx, source, func(ctx context.Context) error {
		return c.command(ctx, path, nil, nil, "fetch", "--atomic", "--prune", "--no-tags", "--no-recurse-submodules", "--", source, "+refs/*:refs/*")
	})
}

func (c *Client) Push(ctx context.Context, path, destination string, atomic bool, mappings ...RefMapping) (Changes, error) {
	if len(mappings) > 0 {
		return c.pushMapped(ctx, path, destination, atomic, mappings)
	}
	return c.push(ctx, path, destination, atomic)
}

func (c *Client) push(ctx context.Context, path, destination string, atomic bool) (Changes, error) {
	if err := checkLocalEndpoint(destination); err != nil {
		return Changes{}, err
	}
	var local bytes.Buffer
	if err := c.command(ctx, path, nil, &local, "for-each-ref", "--count=1", "--format=%(refname)", "refs/"); err != nil {
		return Changes{}, err
	}
	args := []string{"push", "--mirror", "--porcelain", "--no-recurse-submodules"}
	if filepath.IsAbs(destination) && ctx.Value(trustedLocalHooksKey{}) != true {
		args = append(args, "--receive-pack=git -c core.hooksPath="+os.DevNull+" receive-pack")
	}
	if atomic {
		args = append(args, "--atomic")
	}
	args = append(args, "--", destination)
	var parser *pushParser
	err := c.retry(ctx, destination, func(ctx context.Context) error {
		parser = &pushParser{}
		err := c.command(ctx, path, nil, parser, args...)
		parser.finish()
		if parser.changes.Rejected > 0 {
			if err != nil {
				return fmt.Errorf("destination rejected refs: %w", err)
			}
			return errors.New("destination rejected refs")
		}
		if local.Len() == 0 && parser.changes == (Changes{}) && emptyMirrorPush(err) {
			return nil
		}
		return err
	})
	if parser == nil {
		return Changes{}, err
	}
	return parser.changes, err
}

func emptyMirrorPush(err error) bool {
	// Only receive-pack advertises the refs available for push. Git's send-pack
	// emits these two lines when the mirror has no source or destination refs
	// to send. Some transports then exit 1 instead of reporting a no-op.
	const noRefs = "\nNo refs in common and none specified; doing nothing.\nPerhaps you should specify a branch.\n"
	var commandErr *CommandError
	return errors.As(err, &commandErr) && commandErr.ExitCode == 1 &&
		strings.Contains("\n"+commandErr.Detail+"\n", noRefs)
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
	if err := checkLocalEndpoint(source); err != nil {
		return err
	}
	if err := c.prepareLFS(ctx, path); err != nil {
		return err
	}
	args := []string{"-c", "remote.mirrorer-lfs.url=" + lfsAddress(source),
		"-c", "lfs.concurrenttransfers=1",
		"-c", "lfs.skipdownloaderrors=false", "lfs", "fetch", "--all", "mirrorer-lfs"}
	err := c.command(ctx, path, nil, nil, "symbolic-ref", "--quiet", "HEAD")
	if err == nil {
		return c.retry(ctx, source, func(ctx context.Context) error {
			return c.command(ctx, path, nil, nil, args...)
		})
	}
	var commandErr *CommandError
	if !errors.As(err, &commandErr) || commandErr.ExitCode != 1 {
		return err
	}
	// An implicit --all scan includes detached HEAD, which may still point to
	// the commit seen at clone time. Fetch only objects reachable from refs/.
	return c.withLFSRefList(ctx, path, "%(objectname)", func(refs *os.File) error {
		return c.retry(ctx, source, func(ctx context.Context) error {
			return c.fetchLFSRefBatches(ctx, path, args, refs)
		})
	})
}

func (c *Client) fetchLFSRefBatches(ctx context.Context, path string, args []string, refs *os.File) error {
	if _, err := refs.Seek(0, io.SeekStart); err != nil {
		return err
	}
	// LFS 3.7 does not support fetch --stdin. Use fixed-size batches of up to
	// 512 object IDs (at most 33 KiB of argument strings for SHA-256).
	const batchSize = 512
	batch := make([]string, 0, len(args)+batchSize)
	batch = append(batch, args...)
	scanner := bufio.NewScanner(refs)
	for scanner.Scan() {
		batch = append(batch, scanner.Text())
		if len(batch) == len(args)+batchSize {
			if err := c.command(ctx, path, nil, nil, batch...); err != nil {
				return err
			}
			batch = batch[:len(args)]
		}
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	if len(batch) > len(args) {
		return c.command(ctx, path, nil, nil, batch...)
	}
	return nil
}

func (c *Client) LFSPush(ctx context.Context, path, destination string) error {
	if err := checkLocalEndpoint(destination); err != nil {
		return err
	}
	if err := c.prepareLFS(ctx, path); err != nil {
		return err
	}
	return c.withLFSRefList(ctx, path, "%(refname)", func(refs *os.File) error {
		size, err := refs.Seek(0, io.SeekCurrent)
		if err != nil || size == 0 {
			return err
		}
		return c.retry(ctx, destination, func(ctx context.Context) error {
			if _, err := refs.Seek(0, io.SeekStart); err != nil {
				return err
			}
			return c.command(ctx, path, refs, nil, "-c", "remote.mirrorer-lfs.url="+lfsAddress(destination),
				"-c", "lfs.concurrenttransfers=1",
				"-c", "lfs.allowincompletepush=false", "lfs", "push", "--all", "mirrorer-lfs", "--stdin")
		})
	})
}

func lfsAddress(address string) string {
	if !filepath.IsAbs(address) {
		return address
	}
	// Git LFS parses local endpoints as URLs. Escape literal URL delimiters.
	return (&url.URL{Scheme: "file", Path: address}).String()
}

func (c *Client) withLFSRefList(ctx context.Context, path, format string, transfer func(*os.File) error) error {
	// A temporary ref list bounds memory and avoids platform argument-size limits.
	refs, err := os.CreateTemp(filepath.Dir(path), ".lfs-refs-*")
	if err != nil {
		return err
	}
	defer os.Remove(refs.Name())
	defer refs.Close()
	if err := c.command(ctx, path, nil, refs, "for-each-ref", "--format="+format, "refs/"); err != nil {
		return err
	}
	return transfer(refs)
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

type trustedLocalHooksKey struct{}

// WithTrustedLocalHooks enables destination-controlled code as the service UID.
// Only use for an explicitly opted-in, trust-equivalent local destination.
func WithTrustedLocalHooks(ctx context.Context) context.Context {
	return context.WithValue(ctx, trustedLocalHooksKey{}, true)
}

func checkLocalEndpoint(address string) error {
	if !filepath.IsAbs(address) {
		return nil
	}
	root, err := safepath.Open(address, false)
	if err != nil {
		return fmt.Errorf("untrusted local repository: %w", err)
	}
	return root.Close()
}

// Reclaim runs only while the caller holds the source lease, before fan-out.
// No external Git process may write the managed cache.
func (c *Client) Reclaim(ctx context.Context, path string) error {
	if err := c.command(ctx, path, nil, nil, "reflog", "expire", "--expire=now", "--all"); err != nil {
		return err
	}
	// The source lease excludes other writers. Git's gc.pid can outlive an
	// interrupted process group and name a host from a previous run.
	return c.command(ctx, path, nil, nil, "-c", "pack.threads=1", "-c", "pack.windowMemory=64m", "-c", "gc.cruftPacks=false", "-c", "gc.autoDetach=false", "-c", "gc.detach=false", "gc", "--force", "--prune=now")
}
