package mirror

import (
	log "github.com/sirupsen/logrus"
	"github.com/zekihan/git-mirrorer/config"
	"github.com/zekihan/git-mirrorer/git"
	"time"
)

var quit chan bool

func Reconcile() {
	values := config.ReadConfig().Values
	for _, value := range values {
		source := value.Source
		for _, mirror := range value.Mirrors {
			err := git.HandleMirror(source.URL, mirror.URL)
			if err != nil {
				log.Error("Error while handling mirror ", mirror.URL, ":", err)
			}
		}
	}
}

func StartReconcileLoop() {
	Reconcile()
	ticker := time.NewTicker(config.ReadConfig().GetScheduleInterval())
	quit = make(chan bool)
	go func() {
		for {
			select {
			case <-ticker.C:
				Reconcile()
			case <-quit:
				ticker.Stop()
				return
			}
		}
	}()
}

func StopReconcileLoop() {
	quit <- true
	close(quit)
}
