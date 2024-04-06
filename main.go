package main

import (
	"fmt"
	log "github.com/sirupsen/logrus"
	"github.com/zekihan/git-mirrorer/config"
	"github.com/zekihan/git-mirrorer/mirror"
	"github.com/zekihan/git-mirrorer/util"
	"net/http"
	"time"
)

func main() {
	cfg := config.GetConfig()
	util.InitLogger(cfg)

	if cfg.RunOnce {
		mirror.Reconcile()
		return
	}

	go func() {
		mirror.StartReconcileLoop()
	}()

	if cfg.Web.Enabled {
		http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
			_, err := w.Write([]byte("OK"))
			if err != nil {
				log.Error(err)
			}
		})

		err := http.ListenAndServe(fmt.Sprintf(":%d", config.ReadConfig().Web.Port), nil)
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
