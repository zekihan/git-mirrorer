package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/zekihan/go-commons/environment"
	"github.com/zekihan/go-commons/utils"
	"github.com/zekihan/go-commons/web/health"

	"github.com/zekihan/git-mirrorer/config"
	"github.com/zekihan/git-mirrorer/mirror"
)

var (
	cfg            *config.Config
	defaultTimeout = 10 * time.Second
)

func init() {
	err := environment.LoadEnvironmentFile()
	if err != nil {
		slog.ErrorContext(
			context.Background(),
			"Error loading environment file",
			slog.Any("error", err),
		)
		os.Exit(1)
	}

	// Init config.
	cfg = config.GetConfig()

	// Init logger.
	utils.InitLogger(&utils.LoggerConfig{
		Level:     cfg.LogConfig.Level,
		AddSource: cfg.LogConfig.AddSource,
	})
}

func main() {
	ctx := context.Background()

	slog.InfoContext(ctx, "Starting git mirrorer server")
	utils.LogVersionInfo(ctx)

	if cfg.RunOnce {
		mirror.Reconcile(ctx)

		return
	}

	if cfg.WebConfig.Enabled {
		go func() {
			http.HandleFunc("/", health.GetHealth)

			srv := &http.Server{
				Addr:         fmt.Sprintf(":%d", cfg.WebConfig.Port),
				ReadTimeout:  defaultTimeout,
				WriteTimeout: defaultTimeout,
			}

			err := srv.ListenAndServe()
			if err != nil {
				slog.ErrorContext(ctx, "Failed to start web server", slog.Any("error", err))
				os.Exit(1)
			}
		}()
	}

	mirror.StartReconcileLoop()
	mirror.StopReconcileLoop()
}
