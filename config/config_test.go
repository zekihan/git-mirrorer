package config

import (
	"log/slog"
	"os"
	"path"
	"testing"
	"time"
)

func TestGetDefaultConfig(t *testing.T) {
	cfg := GetDefaultConfig()
	if cfg.RunOnce != false {
		t.Errorf("expected RunOnce to be false, got %v", cfg.RunOnce)
	}

	if cfg.RepoPath != "tmp/repos" {
		t.Errorf("expected RepoPath to be 'tmp/repos', got %v", cfg.RepoPath)
	}

	if cfg.Schedule.Interval != 15*time.Minute {
		t.Errorf("expected Schedule.Interval to be 15m, got %v", cfg.Schedule.Interval)
	}

	if cfg.WebConfig.Port != 8080 {
		t.Errorf("expected WebConfig.Port to be 8080, got %v", cfg.WebConfig.Port)
	}

	if cfg.LogConfig.Level != slog.LevelWarn {
		t.Errorf("expected LogConfig.Level to be WarnLevel, got %v", cfg.LogConfig.Level)
	}
}

func TestGitSource_Valid(t *testing.T) {
	gs := &GitSource{URL: ""}

	valid, err := gs.Valid()
	if valid || err == nil {
		t.Errorf("expected invalid GitSource with empty URL")
	}

	gs.URL = "https://github.com/user/repo.git"

	valid, err = gs.Valid()
	if !valid || err != nil {
		t.Errorf("expected valid GitSource, got error: %v", err)
	}
}

func TestConfig_Valid(t *testing.T) {
	cfg := GetDefaultConfig()
	cfg.Values = []Value{
		{
			Source:  GitSource{URL: ""},
			Mirrors: []GitSource{{URL: ""}},
		},
	}

	valid, err := cfg.Valid()
	if valid || err == nil {
		t.Errorf("expected invalid config due to empty URLs")
	}

	cfg.Values = []Value{
		{
			Source:  GitSource{URL: "https://github.com/user/repo.git"},
			Mirrors: []GitSource{{URL: "https://gitlab.com/user/repo.git"}},
		},
	}

	valid, err = cfg.Valid()
	if !valid || err != nil {
		t.Errorf("expected valid config, got error: %v", err)
	}
}

func TestGitSource_GetRepoPath(t *testing.T) {
	cfg := GetDefaultConfig()
	cfg.RepoPath = "/tmp/testrepos"
	config = cfg // set global config for test
	gs := &GitSource{URL: "https://github.com/user/repo.git"}
	expected := path.Join(cfg.RepoPath, "https:%2F%2Fgithub.com%2Fuser%2Frepo.git")

	got := gs.GetRepoPath()
	if got != expected {
		t.Errorf("expected %v, got %v", expected, got)
	}
}

func TestGetConfigFilePath(t *testing.T) {
	t.Setenv("CONFIG_PATH", "/tmp/myconfig.yaml")

	if got := getConfigFilePath(); got != "/tmp/myconfig.yaml" {
		t.Errorf("expected /tmp/myconfig.yaml, got %v", got)
	}

	t.Setenv("CONFIG_PATH", "")
	t.Setenv("CONFIG_DIR", "/tmp/mydir")

	expected := path.Join("/tmp/mydir", defaultConfigFile)
	if got := getConfigFilePath(); got != expected {
		t.Errorf("expected %v, got %v", expected, got)
	}

	t.Setenv("CONFIG_DIR", "")

	wd, _ := os.Getwd()

	expected = path.Join(wd, defaultConfigFile)
	if got := getConfigFilePath(); got != expected {
		t.Errorf("expected %v, got %v", expected, got)
	}
}

func TestGetConfig_EnvOverrideAndLogLevel(t *testing.T) {
	tmpfile, err := os.CreateTemp(t.TempDir(), "goodconfig*.yaml")
	if err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}

	_, err = tmpfile.WriteString("log:\n  level: info\n")
	if err != nil {
		t.Fatalf("failed to write to temp file: %v", err)
	}

	err = tmpfile.Close()
	if err != nil {
		t.Fatalf("failed to close temp file: %v", err)
	}

	t.Setenv("CONFIG_PATH", tmpfile.Name())
	t.Setenv("GM__LOG_LEVEL", "error")
	t.Setenv("GM__WEB_PORT", "1234")

	config = nil // reset global config

	cfg := GetConfig()
	if cfg.LogConfig.Level != slog.LevelError {
		t.Errorf("expected log level error, got %v", cfg.LogConfig.Level)
	}

	if cfg.WebConfig.Port != 1234 {
		t.Errorf("expected web port 1234, got %v", cfg.WebConfig.Port)
	}
}

func TestGitSource_Valid_NonEmptyURL(t *testing.T) {
	gs := &GitSource{URL: "ssh://git@github.com/user/repo.git"}

	valid, err := gs.Valid()
	if !valid || err != nil {
		t.Errorf("expected valid GitSource, got error: %v", err)
	}
}

func TestConfig_Valid_MultipleValues(t *testing.T) {
	cfg := GetDefaultConfig()
	cfg.Values = []Value{
		{
			Source: GitSource{URL: "https://github.com/user/repo1.git"},
			Mirrors: []GitSource{
				{URL: "https://gitlab.com/user/repo1.git"},
				{URL: "https://bitbucket.org/user/repo1.git"},
			},
		},
		{
			Source:  GitSource{URL: "https://github.com/user/repo2.git"},
			Mirrors: []GitSource{{URL: "https://gitlab.com/user/repo2.git"}},
		},
	}

	valid, err := cfg.Valid()
	if !valid || err != nil {
		t.Errorf("expected valid config, got error: %v", err)
	}
}
