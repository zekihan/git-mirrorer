package mirror

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/zekihan/git-mirrorer/internal/cache"
	"github.com/zekihan/git-mirrorer/internal/config"
	"github.com/zekihan/git-mirrorer/internal/git"
	"github.com/zekihan/git-mirrorer/internal/redact"
)

type Outcome string

const (
	Succeeded Outcome = "succeeded"
	Failed    Outcome = "failed"
	Skipped   Outcome = "skipped"
	Canceled  Outcome = "canceled"
)

type Failure struct {
	Stage  string `json:"stage"`
	Code   string `json:"code"`
	Detail string `json:"detail"`
}

type DestinationResult struct {
	Name      string        `json:"name"`
	Outcome   Outcome       `json:"outcome"`
	Attempted bool          `json:"attempted"`
	Duration  time.Duration `json:"durationNs"`
	Changes   git.Changes   `json:"changes"`
	Error     *Failure      `json:"error,omitempty"`
}

type SourceResult struct {
	Name         string              `json:"name"`
	Outcome      Outcome             `json:"outcome"`
	Refresh      Outcome             `json:"refresh"`
	Duration     time.Duration       `json:"durationNs"`
	Error        *Failure            `json:"error,omitempty"`
	Destinations []DestinationResult `json:"destinations"`
}

type Totals struct {
	Sources      int `json:"sources"`
	Destinations int `json:"destinations"`
	Attempted    int `json:"attempted"`
	Succeeded    int `json:"succeeded"`
	Failed       int `json:"failed"`
	Skipped      int `json:"skipped"`
	Canceled     int `json:"canceled"`
	Unsuccessful int `json:"unsuccessful"`
}

type Report struct {
	SchemaVersion int            `json:"schemaVersion"`
	Started       time.Time      `json:"started"`
	Finished      time.Time      `json:"finished"`
	Duration      time.Duration  `json:"durationNs"`
	Outcome       Outcome        `json:"outcome"`
	Error         *Failure       `json:"error,omitempty"`
	Totals        Totals         `json:"totals"`
	Sources       []SourceResult `json:"sources"`
}

// Backend owns one locked source snapshot until its release function is called.
type Backend interface {
	Prepare(context.Context, string) (path string, release func() error, stage string, err error)
	Push(context.Context, string, string, bool) (git.Changes, error)
	LFSFetch(context.Context, string, string) error
	LFSPush(context.Context, string, string) error
}

type CachedGit struct {
	Client   *git.Client
	StateDir string
}

func (b CachedGit) Prepare(ctx context.Context, source string) (string, func() error, string, error) {
	lease, err := cache.Acquire(b.StateDir, source)
	if err != nil {
		return "", nil, "lock", err
	}
	cloned, err := lease.Prepare(ctx, b.Client)
	if err != nil {
		var refresh *cache.RefreshError
		if errors.As(err, &refresh) {
			return "", lease.Close, "fetch", err
		}
		return "", lease.Close, "cache", err
	}
	if !cloned {
		if err := b.Client.Fetch(ctx, lease.Path, source); err != nil {
			return "", lease.Close, "fetch", err
		}
	}
	return lease.Path, lease.Close, "", nil
}

func (b CachedGit) Push(ctx context.Context, path, destination string, atomic bool) (git.Changes, error) {
	return b.Client.Push(ctx, path, destination, atomic)
}
func (b CachedGit) LFSFetch(ctx context.Context, path, source string) error {
	return b.Client.LFSFetch(ctx, path, source)
}
func (b CachedGit) LFSPush(ctx context.Context, path, destination string) error {
	return b.Client.LFSPush(ctx, path, destination)
}

type Runner struct {
	Config config.Config
	Git    Backend
	Logger *slog.Logger
}

func failure(stage string, err error) *Failure {
	code := stage
	if errors.Is(err, context.DeadlineExceeded) {
		code = "timeout"
	}
	if errors.Is(err, context.Canceled) {
		code = "canceled"
	}
	return &Failure{Stage: stage, Code: code, Detail: redact.Text(err.Error())}
}

func (r *Runner) Run(ctx context.Context) Report {
	if r.Config.RunTimeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, r.Config.RunTimeout)
		defer cancel()
	}
	report := Report{SchemaVersion: 1, Started: time.Now().UTC(), Outcome: Succeeded, Sources: make([]SourceResult, len(r.Config.Repositories))}
	jobs := make(chan int)
	var workers sync.WaitGroup
	for range min(r.Config.Concurrency, len(r.Config.Repositories)) {
		workers.Go(func() {
			for index := range jobs {
				report.Sources[index] = r.runSource(ctx, r.Config.Repositories[index])
			}
		})
	}
	for index, repo := range r.Config.Repositories {
		if ctx.Err() != nil {
			report.Sources[index] = canceledSource(repo, ctx.Err())
			continue
		}
		select {
		case jobs <- index:
		case <-ctx.Done():
			report.Sources[index] = canceledSource(repo, ctx.Err())
		}
	}
	close(jobs)
	workers.Wait()
	for _, source := range report.Sources {
		report.Totals.Sources++
		if source.Outcome != Succeeded {
			report.Outcome = Failed
		}
		for _, dest := range source.Destinations {
			report.Totals.Destinations++
			if dest.Attempted {
				report.Totals.Attempted++
			}
			switch dest.Outcome {
			case Succeeded:
				report.Totals.Succeeded++
			case Failed:
				report.Totals.Failed++
			case Skipped:
				report.Totals.Skipped++
			case Canceled:
				report.Totals.Canceled++
			}
			if dest.Outcome != Succeeded {
				report.Totals.Unsuccessful++
			}
		}
	}
	if ctx.Err() != nil {
		report.Outcome = Canceled
		report.Error = failure("run", ctx.Err())
	}
	report.Finished = time.Now().UTC()
	report.Duration = report.Finished.Sub(report.Started)
	return report
}

func canceledSource(repo config.Repository, err error) SourceResult {
	result := SourceResult{Name: repo.Name, Outcome: Canceled, Refresh: Canceled, Error: failure("run", err)}
	for _, dest := range repo.Destinations {
		result.Destinations = append(result.Destinations, DestinationResult{Name: dest.Name, Outcome: Canceled, Error: failure("run", err)})
	}
	return result
}

func (r *Runner) runSource(ctx context.Context, repo config.Repository) (result SourceResult) {
	if ctx.Err() != nil {
		return canceledSource(repo, ctx.Err())
	}
	start := time.Now()
	result = SourceResult{Name: repo.Name, Outcome: Succeeded, Refresh: Succeeded, Destinations: make([]DestinationResult, 0, len(repo.Destinations))}
	defer func() { result.Duration = time.Since(start) }()
	if r.Logger != nil {
		r.Logger.InfoContext(ctx, "Refreshing source", "source", repo.Name)
	}
	path, release, stage, err := r.Git.Prepare(ctx, repo.Source)
	if release != nil {
		defer func() {
			if err := release(); err != nil {
				result.Outcome, result.Error = Failed, failure("lock", err)
			}
		}()
	}
	if err == nil && repo.LFS {
		stage, err = "lfs-fetch", r.Git.LFSFetch(ctx, path, repo.Source)
	}
	if err != nil {
		result.Outcome, result.Refresh, result.Error = Failed, Failed, failure(stage, err)
		for _, dest := range repo.Destinations {
			outcome := Skipped
			if ctx.Err() != nil {
				outcome = Canceled
				result.Outcome, result.Refresh = Canceled, Canceled
			}
			result.Destinations = append(result.Destinations, DestinationResult{Name: dest.Name, Outcome: outcome, Error: failure(stage, err)})
		}
		return result
	}
	for _, dest := range repo.Destinations {
		destination := DestinationResult{Name: dest.Name, Outcome: Succeeded}
		if ctx.Err() != nil {
			destination.Outcome, destination.Error = Canceled, failure("run", ctx.Err())
		} else {
			if r.Logger != nil {
				r.Logger.InfoContext(ctx, "Mirroring destination", "source", repo.Name, "destination", dest.Name)
			}
			destStart := time.Now()
			destination.Attempted = true
			stage, err = "push", nil
			if repo.LFS {
				stage, err = "lfs-push", r.Git.LFSPush(ctx, path, dest.URL)
			}
			if err == nil {
				stage = "push"
				destination.Changes, err = r.Git.Push(ctx, path, dest.URL, dest.AtomicEnabled())
			}
			destination.Duration = time.Since(destStart)
			if err != nil {
				destination.Outcome, destination.Error = Failed, failure(stage, err)
				if ctx.Err() != nil {
					destination.Outcome = Canceled
				}
			}
		}
		if destination.Outcome != Succeeded {
			result.Outcome = Failed
		}
		result.Destinations = append(result.Destinations, destination)
	}
	if ctx.Err() != nil {
		result.Outcome = Canceled
	}
	return result
}
