package util

import (
	log "github.com/sirupsen/logrus"
	"github.com/snowzach/rotatefilehook"
	"github.com/zekihan/git-mirrorer/config"
	"time"
)

func InitLogger(cfg *config.Config) {
	logLevel, err := log.ParseLevel(cfg.Log.Level)
	if err != nil {
		log.Warning("Invalid log level: ", err)
		logLevel = log.InfoLevel
	}

	rotateFileHook, err := rotatefilehook.NewRotateFileHook(rotatefilehook.RotateFileConfig{
		Filename:   cfg.Log.Path,
		MaxSize:    50, // megabytes
		MaxBackups: 3,
		MaxAge:     28, //days
		Level:      logLevel,
		Formatter: &log.JSONFormatter{
			TimestampFormat: time.RFC822,
		},
	})

	if err != nil {
		log.Fatalf("Failed to initialize file rotate hook: %v", err)
	}

	log.SetLevel(logLevel)
	log.SetFormatter(&log.TextFormatter{
		ForceColors:     true,
		FullTimestamp:   true,
		TimestampFormat: time.RFC822,
	})
	log.AddHook(rotateFileHook)
}
