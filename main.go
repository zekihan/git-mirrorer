package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/zekihan/go-commons/environment"
	"github.com/zekihan/go-commons/utils"
	"github.com/zekihan/go-commons/web/health"

	log "github.com/sirupsen/logrus"

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
		log.Errorf("Error loading environment file: %v", err)
		os.Exit(1)
	}

	// Init config.
	cfg = config.GetConfig()

	// Init logger.
	err = utils.InitLogger(&utils.LoggerConfig{
		Path:          cfg.LogConfig.Path,
		Level:         cfg.LogConfig.Level,
		AddMethodName: cfg.LogConfig.AddMethodName,
	})
	if err != nil {
		log.Errorf("Error initializing logger: %v", err)
		os.Exit(1)
	}
}

func main() {
	log.Info("Starting git mirrorer server")
	log.Info(utils.GetStartupInfo())

	ctx := context.Background()

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
				log.Errorf("Failed to start server: %v", err)
				os.Exit(1)
			}
		}()
	}

	mirror.StartReconcileLoop()
	mirror.StopReconcileLoop()
}
