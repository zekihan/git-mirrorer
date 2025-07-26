package main

import (
	"fmt"
	"github.com/joho/godotenv"
	log "github.com/sirupsen/logrus"
	"github.com/zekihan/git-mirrorer/config"
	"github.com/zekihan/git-mirrorer/mirror"
	"github.com/zekihan/git-mirrorer/utils"
	"net/http"
	"os"
	"time"
)

var cfg *config.Config

func init() {
	envFile := ".env"
	if f, err := os.Stat(envFile); !(os.IsNotExist(err) || f.IsDir()) {
		err = godotenv.Load()
		if err != nil {
			panic("Error loading .env file")
		}
	}

	cfg = config.GetConfig()
	utils.InitLogger(cfg)
}

func main() {
	log.Info("Starting git mirrorer server")
	log.Info(utils.GetStartupInfo())

	if cfg.RunOnce {
		mirror.Reconcile()
		return
	}

	go func() {
		mirror.StartReconcileLoop()
	}()

	if cfg.WebConfig.Enabled {
		http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
			_, err := w.Write([]byte("OK"))
			if err != nil {
				log.Error(err)
			}
		})

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
