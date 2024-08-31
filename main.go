package main

import (
	"fmt"
	log "github.com/sirupsen/logrus"
	"github.com/zekihan/git-mirrorer/config"
	"github.com/zekihan/git-mirrorer/mirror"
	"github.com/zekihan/git-mirrorer/utils"
	"net/http"
	"time"
)

var cfg *config.Config

func init() {
	cfg = config.GetConfig()
	utils.InitLogger(cfg)
}

func main() {
	log.Infof("Starting git-mirrorer %s in %s", utils.GetVersion(), utils.GetEnvironment())

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
