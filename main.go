package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/zekihan/go-commons/environment"
	"github.com/zekihan/go-commons/utils"
	"github.com/zekihan/go-commons/web/health"

	"github.com/zekihan/git-mirrorer/config"
	"github.com/zekihan/git-mirrorer/mirror"
)

var (
	cfg             *config.Config
	defaultTimeout  = 10 * time.Second
	shutdownTimeout = 5 * time.Second
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
		Writer:    nil,
		Level:     cfg.LogConfig.Level,
		AddSource: cfg.LogConfig.AddSource,
	})
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	slog.InfoContext(ctx, "Starting git mirrorer server")
	utils.LogVersionInfo(ctx)

	if cfg.RunOnce {
		mirror.Reconcile(ctx)

		return
	}

	var srv *http.Server

	if cfg.WebConfig.Enabled {
		http.HandleFunc("/", health.GetHealth)

		srv = &http.Server{
			Addr:                         fmt.Sprintf(":%d", cfg.WebConfig.Port),
			Handler:                      nil,
			DisableGeneralOptionsHandler: false,
			TLSConfig:                    nil,
			ReadTimeout:                  defaultTimeout,
			ReadHeaderTimeout:            0,
			WriteTimeout:                 defaultTimeout,
			IdleTimeout:                  0,
			MaxHeaderBytes:               0,
			MaxHeaderValueCount:          0,
			TLSNextProto:                 nil,
			ConnState:                    nil,
			ErrorLog:                     nil,
			BaseContext:                  nil,
			ConnContext:                  nil,
			HTTP2:                        nil,
			Protocols:                    nil,
			DisableClientPriority:        false,
		}

		go func() {
			err := srv.ListenAndServe()
			if err != nil && !errors.Is(err, http.ErrServerClosed) {
				slog.ErrorContext(ctx, "Failed to start web server", slog.Any("error", err))
				os.Exit(1)
			}
		}()
	}

	mirror.StartReconcileLoop(ctx)

	<-ctx.Done()

	stop()
	mirror.StopReconcileLoop()

	if srv != nil {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
		defer cancel()

		err := srv.Shutdown(shutdownCtx)
		if err != nil {
			slog.ErrorContext(shutdownCtx, "Failed to shut down web server", slog.Any("error", err))
		}
	}
}
