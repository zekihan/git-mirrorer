package mirror

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/zekihan/git-mirrorer/config"
	"github.com/zekihan/git-mirrorer/git"
)

var quit chan bool

const threadCount = 8

func Reconcile(ctx context.Context) {
	cfg := config.GetConfig() //nolint:contextcheck // This is a top-level function, so context is not passed down.

	values := cfg.Values
	if len(values) == 0 {
		slog.InfoContext(ctx, "No values to reconcile")

		return
	}

	jobs := make(chan *config.Value, len(values))
	result := make(chan error, len(values))

	for range threadCount {
		go func() {
			for job := range jobs {
				source := job.Source
				errs := make([]error, 0)

				for _, mirror := range job.Mirrors {
					err := git.HandleMirror(ctx, source, mirror)
					errs = append(errs, err)
				}

				result <- errors.Join(errs...)
			}
		}()
	}

	for _, job := range values {
		jobs <- &job
	}

	close(jobs)

	for range values {
		res := <-result
		if res != nil {
			slog.ErrorContext(ctx, "Failed to reconcile mirror", slog.Any("error", res))

			continue
		}
	}
}

func StartReconcileLoop() {
	ctx, cancel := context.WithCancel(context.Background())
	Reconcile(ctx)

	ticker := time.NewTicker(config.GetConfig().Schedule.Interval)
	quit = make(chan bool)

	go func() {
		for {
			select {
			case <-ticker.C:
				Reconcile(ctx)
			case <-quit:
				ticker.Stop()
				cancel()

				return
			}
		}
	}()
}

func StopReconcileLoop() {
	quit <- true

	close(quit)
}
