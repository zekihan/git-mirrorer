package config

import (
	"context"
	"errors"
	"github.com/sethvargo/go-envconfig"
	"github.com/sirupsen/logrus"
	"gopkg.in/yaml.v3"
	"io"
	"log"
	"net/url"
	"os"
	"path"
	"strings"
	"time"
)

var config *Config

type Config struct {
	RunOnce    bool                  `yaml:"run_once" env:"RUN_ONCE"`
	RepoPath   string                `yaml:"repo_path" env:"REPO_PATH"`
	AuthConfig map[string]AuthConfig `yaml:"auth"`
	Schedule   Schedule              `yaml:"schedule" env:", prefix=GM__SCHEDULE_"`
	WebConfig  WebConfig             `yaml:"web" env:", prefix=GM__WEB_"`
	LogConfig  LogConfig             `yaml:"log" env:", prefix=GM__LOG_"`
	Values     []Value               `yaml:"values"`
}

type Value struct {
	Source  GitSource   `yaml:"source"`
	Mirrors []GitSource `yaml:"mirrors"`
}

type AuthType string

const (
	AuthTypeNone   AuthType = "none"
	AuthTypeGithub AuthType = "github"
	AuthTypeGitlab AuthType = "gitlab"
)

type AuthConfig struct {
	AuthType AuthType `yaml:"type"`
	User     string   `yaml:"user"`
	Token    string   `yaml:"token"`
}

type AutoCreateConfig struct {
	Enabled bool   `yaml:"enabled"`
	Auth    string `yaml:"auth"`
	Private bool   `yaml:"private"`
	IsOrg   bool   `yaml:"is_org"`
}

func (a *AutoCreateConfig) GetAuth() (AuthConfig, bool) {
	if a.Auth == "" {
		return AuthConfig{}, false
	}
	auth, ok := GetConfig().AuthConfig[a.Auth]
	return auth, ok
}

type GitSource struct {
	URL              string           `yaml:"url"`
	AutoCreateConfig AutoCreateConfig `yaml:"auto_create"`
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
	Enabled bool   `yaml:"enabled" env:"ENABLED"`
	Port    int    `yaml:"port" env:"PORT"`
	APIKey  string `yaml:"api_key" env:"API_KEY"`
}

type Schedule struct {
	Interval time.Duration `yaml:"interval" env:"INTERVAL"`
}

type LogConfig struct {
	Level         logrus.Level `yaml:"-"`
	LevelStr      string       `yaml:"level" env:"LEVEL"`
	AddMethodName bool         `yaml:"add_method_name" env:"ADD_METHOD_NAME"`
	Path          string       `yaml:"path" env:"PATH"`
}

func GetConfig() *Config {
	if config == nil {
		cfg := GetConfigFromFile()
		ctx := context.Background()
		if err := envconfig.Process(ctx, cfg); err != nil {
			log.Fatal(err)
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
	config := GetDefaultConfig()
	file, err := os.Open("config.yaml")
	if err != nil {
		log.Fatalf("Failed to open config file: %v", err)
	}
	defer file.Close()
	content, err := io.ReadAll(file)
	if err != nil {
		log.Fatalf("Failed to read config file: %v", err)
	}
	err = yaml.Unmarshal(content, config)
	if err != nil {
		log.Fatalf("Failed to parse config file: %v", err)
	}
	return config
}

func GetDefaultConfig() *Config {
	return &Config{
		RunOnce:  false,
		RepoPath: "tmp/repos",
		Schedule: Schedule{
			Interval: 15 * time.Minute,
		},
		WebConfig: WebConfig{
			Enabled: false,
			Port:    8080,
			APIKey:  "",
		},
		LogConfig: LogConfig{
			Level:         logrus.WarnLevel,
			AddMethodName: false,
			Path:          "tmp/logs",
		},
		Values: []Value{},
	}
}

func (g *GitSource) Valid() (bool, error) {
	foundErrors := make([]error, 0)
	if g.URL == "" {
		foundErrors = append(foundErrors, errors.New("url is required"))
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
