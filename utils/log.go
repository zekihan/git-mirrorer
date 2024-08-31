package utils

import (
	log "github.com/sirupsen/logrus"
	"github.com/snowzach/rotatefilehook"
	"github.com/zekihan/git-mirrorer/config"
	"os"
	"time"
)

func InitLogger(cfg *config.Config) {
	log.SetLevel(cfg.LogConfig.Level)
	log.SetFormatter(&log.TextFormatter{
		ForceColors:     true,
		FullTimestamp:   true,
		TimestampFormat: time.RFC822,
	})
	log.SetReportCaller(config.GetConfig().LogConfig.AddMethodName)
	log.SetOutput(os.Stdout)
	if cfg.LogConfig.Path != "" {
		AddFileHook(cfg)
	}
}

func AddFileHook(cfg *config.Config) {
	rotateFileHook, err := rotatefilehook.NewRotateFileHook(rotatefilehook.RotateFileConfig{
		Filename:   cfg.LogConfig.Path,
		MaxSize:    50, // megabytes
		MaxBackups: 3,
		MaxAge:     28, //days
		Level:      cfg.LogConfig.Level,
		Formatter: &log.JSONFormatter{
			TimestampFormat: time.RFC822,
		},
	})
	if err != nil {
		log.Fatalf("Failed to initialize file rotate hook: %v", err)
	}
	log.AddHook(rotateFileHook)
}
