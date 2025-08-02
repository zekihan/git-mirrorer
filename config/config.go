//nolint:forbidigo // This package configures log packages.
package config

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net/url"
	"os"
	"path"
	"strings"
	"time"

	"github.com/sethvargo/go-envconfig"
	"github.com/sirupsen/logrus"
	"gopkg.in/yaml.v3"
)

const (
	EnvKeyPrefix      = "GM__"
	defaultConfigFile = "config.yaml"
	defaultInterval   = 15 * time.Minute
	defaultPort       = 8080
)

var (
	config           *Config
	errURLIsRequired = errors.New("url is required")
)

type Config struct {
	RepoPath  string    `yaml:"repoPath" env:"REPO_PATH"`
	LogConfig LogConfig `yaml:"log"      env:", prefix=LOG_"`
	Values    []Value   `yaml:"values"`
	WebConfig WebConfig `yaml:"web"      env:", prefix=WEB_"`
	Schedule  Schedule  `yaml:"schedule" env:", prefix=SCHEDULE_"`
	RunOnce   bool      `yaml:"runOnce"  env:"RUN_ONCE"`
}

type Value struct {
	Source  GitSource   `yaml:"source"`
	Mirrors []GitSource `yaml:"mirrors"`
}

type GitSource struct {
	URL string `yaml:"url"`
}

func (g *GitSource) GetRepoPath() string {
	return path.Join(GetConfig().RepoPath, url.PathEscape(g.URL))
}

func (g *GitSource) GetOwnerEntity() (string, string) {
	parts := strings.Split(g.URL, ".git")
	parts = strings.Split(parts[0], ":")
	parts = strings.Split(parts[1], "/")

	return parts[0], parts[1]
}

type WebConfig struct {
	APIKey  string `yaml:"apiKey"  env:"API_KEY"`
	Port    int    `yaml:"port"    env:"PORT"`
	Enabled bool   `yaml:"enabled" env:"ENABLED"`
}

type Schedule struct {
	Interval time.Duration `yaml:"interval" env:"INTERVAL"`
}

type LogConfig struct {
	LevelStr      string       `yaml:"level"         env:"LEVEL"`
	Path          string       `yaml:"path"          env:"PATH"`
	Level         logrus.Level `yaml:"-"`
	AddMethodName bool         `yaml:"addMethodName" env:"ADD_METHOD_NAME"`
}

func GetConfig() *Config {
	if config == nil {
		cfg := GetConfigFromFile()

		ctx := context.Background()

		err := envconfig.ProcessWith(ctx, &envconfig.Config{
			Target:           cfg,
			Lookuper:         envconfig.PrefixLookuper(EnvKeyPrefix, envconfig.OsLookuper()),
			DefaultOverwrite: true,
		})
		if err != nil {
			log.Fatalf("Failed to process env config: %v", err)
		}

		if cfg.LogConfig.LevelStr != "" {
			level, err := logrus.ParseLevel(cfg.LogConfig.LevelStr)
			if err != nil {
				log.Fatalf("Failed to parse log level: %v", err)
			}

			cfg.LogConfig.Level = level
		}

		config = cfg
	}

	return config
}

func GetConfigFromFile() *Config {
	newConfig := GetDefaultConfig()

	file, err := os.Open(getConfigFilePath())
	if err != nil {
		log.Fatalf("Failed to open config file: %v", err)
	}
	defer func(file *os.File) {
		fileErr := file.Close()
		if fileErr != nil {
			fmt.Printf("Failed to close config file: %v", fileErr)
		}
	}(file)

	content, err := io.ReadAll(file)
	if err != nil {
		fileErr := file.Close()
		if fileErr != nil {
			fmt.Printf("Failed to close config file: %v", fileErr)
		}

		log.Fatalf("Failed to read config file: %v", fileErr) //nolint:gocritic
	}

	err = yaml.Unmarshal(content, newConfig)
	if err != nil {
		log.Fatalf("Failed to parse config file: %v", err)
	}

	return newConfig
}

func getConfigFilePath() string {
	configPath := os.Getenv("CONFIG_PATH")
	if configPath != "" {
		return configPath
	}

	configDir := os.Getenv("CONFIG_DIR")
	if configDir == "" {
		cwd, err := os.Getwd()
		if err != nil {
			log.Fatalf("%v", err)
		}

		configDir = cwd
	}

	configPath = path.Join(configDir, defaultConfigFile)

	return configPath
}

func GetDefaultConfig() *Config {
	return &Config{
		RunOnce:  false,
		RepoPath: "tmp/repos",
		Schedule: Schedule{
			Interval: defaultInterval,
		},
		WebConfig: WebConfig{
			Enabled: false,
			Port:    defaultPort,
			APIKey:  "",
		},
		LogConfig: LogConfig{
			Level:         logrus.WarnLevel,
			AddMethodName: false,
		},
		Values: []Value{},
	}
}

func (g *GitSource) Valid() (bool, error) {
	foundErrors := make([]error, 0)
	if g.URL == "" {
		foundErrors = append(foundErrors, errURLIsRequired)
	}

	if len(foundErrors) > 0 {
		return false, errors.Join(foundErrors...)
	}

	return true, nil
}

func (c *Config) Valid() (bool, error) {
	foundErrors := make([]error, 0)

	for _, value := range c.Values {
		valid, err := value.Source.Valid()
		if !valid {
			foundErrors = append(foundErrors, err)
		}

		for _, mirror := range value.Mirrors {
			valid, err := mirror.Valid()
			if !valid {
				foundErrors = append(foundErrors, err)
			}
		}
	}

	if len(foundErrors) > 0 {
		return false, errors.Join(foundErrors...)
	}

	return true, nil
}
