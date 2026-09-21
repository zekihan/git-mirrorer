package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/zekihan/git-mirrorer/internal/config"
	"github.com/zekihan/git-mirrorer/internal/git"
	"github.com/zekihan/git-mirrorer/internal/mirror"
	"github.com/zekihan/git-mirrorer/internal/redact"
	"github.com/zekihan/git-mirrorer/internal/report"
)

var version = "dev"
var commit = "unknown"
var date = "unknown"

type signalError struct{ signal os.Signal }

func (e signalError) Error() string { return e.signal.String() }

func main() { os.Exit(mainWithSignals()) }

func mainWithSignals() int {
	ctx, cancel := context.WithCancelCause(context.Background())
	defer cancel(nil)
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(signals)
	go func() {
		select {
		case sig := <-signals:
			cancel(signalError{sig})
		case <-ctx.Done():
		}
	}()
	return run(ctx, os.Args[1:], os.Stdout, os.Stderr)
}

func run(ctx context.Context, args []string, stdout, stderr io.Writer) (code int) {
	defer func() {
		var sig signalError
		if errors.As(context.Cause(ctx), &sig) {
			if sig.signal == syscall.SIGTERM {
				code = 143
			} else {
				code = 130
			}
		}
	}()
	flags := flag.NewFlagSet("git-mirrorer", flag.ContinueOnError)
	flags.SetOutput(stderr)
	filename := flags.String("config", "git-mirrorer.yaml", "YAML configuration path")
	format := flags.String("format", "text", "Final report format: text or json")
	check := flags.Bool("check", false, "Validate configuration without repository access")
	showVersion := flags.Bool("version", false, "Print version and exit")
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if flags.NArg() != 0 || (*format != "text" && *format != "json") || (*check && *showVersion) {
		fmt.Fprintln(stderr, "invalid arguments: no positional arguments; format must be text or json; check and version are exclusive")
		return 2
	}
	if *showVersion {
		if _, err := fmt.Fprintf(stdout, "git-mirrorer %s (commit %s, built %s)\n", version, commit, date); err != nil {
			return 1
		}
		return 0
	}
	cfg, err := config.Load(*filename)
	if err != nil {
		fmt.Fprintln(stderr, redact.Text(err.Error()))
		return 2
	}
	if *check {
		message := "Configuration valid"
		if *format == "json" {
			message = `{"valid":true}`
		}
		if _, err := fmt.Fprintln(stdout, message); err != nil {
			return 1
		}
		return 0
	}
	if cfg.RunTimeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, cfg.RunTimeout)
		defer cancel()
	}
	var level slog.Level
	_ = level.UnmarshalText([]byte(cfg.LogLevel))
	logger := slog.New(slog.NewTextHandler(stderr, &slog.HandlerOptions{Level: level}))
	client, err := git.New(ctx, cfg.OperationTimeout)
	if err != nil {
		if ctx.Err() != nil {
			return canceledRun(ctx, cfg, *format, stdout, stderr)
		}
		fmt.Fprintln(stderr, redact.Text(err.Error()))
		return 2
	}
	for _, repo := range cfg.Repositories {
		if repo.LFS {
			if err := client.CheckLFS(ctx); err != nil {
				if ctx.Err() != nil {
					return canceledRun(ctx, cfg, *format, stdout, stderr)
				}
				fmt.Fprintln(stderr, "Git LFS is required for lfs-enabled repositories:", redact.Text(err.Error()))
				return 2
			}
			break
		}
	}
	runner := mirror.Runner{Config: cfg, Git: mirror.CachedGit{Client: client, StateDir: cfg.StateDir}, Logger: logger}
	result := runner.Run(ctx)
	if err := report.Write(stdout, *format, result); err != nil {
		fmt.Fprintln(stderr, "write report:", redact.Text(err.Error()))
		return 1
	}
	if result.Outcome != mirror.Succeeded {
		return 1
	}
	return 0
}

func canceledRun(ctx context.Context, cfg config.Config, format string, stdout, stderr io.Writer) int {
	result := (&mirror.Runner{Config: cfg}).Run(ctx)
	if err := report.Write(stdout, format, result); err != nil {
		fmt.Fprintln(stderr, "write report:", err)
	}
	return 1
}
