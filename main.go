package main

import (
	"fmt"
	log "github.com/sirupsen/logrus"
	"github.com/zekihan/git-mirrorer/config"
	"github.com/zekihan/git-mirrorer/mirror"
	"github.com/zekihan/git-mirrorer/util"
	"net/http"
)

func main() {
	cfg := config.GetConfig()
	util.InitLogger(cfg)

	go func() {
		mirror.StartReconcileLoop()
	}()

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

	mirror.StopReconcileLoop()
}
