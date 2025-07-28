package main

import (
	"context"
	"fmt"
	log "github.com/sirupsen/logrus"
	"github.com/zekihan/git-mirrorer/config"
	"github.com/zekihan/git-mirrorer/mirror"
	"github.com/zekihan/go-commons/environment"
	"github.com/zekihan/go-commons/utils"
	"github.com/zekihan/go-commons/web/health"
	"net/http"
	"os"
	"time"
)

var cfg *config.Config

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

	go func() {
		mirror.StartReconcileLoop()
	}()

	if cfg.WebConfig.Enabled {
		http.HandleFunc("/", health.GetHealth)

		err := http.ListenAndServe(fmt.Sprintf(":%d", cfg.WebConfig.Port), nil)
		if err != nil {
			log.Error(err)
		}
	} else {
		for {
			time.Sleep(time.Hour)
		}
	}

	mirror.StopReconcileLoop()
}
