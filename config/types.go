package config

import (
	"errors"
	"path"
	"time"
)

type GitSource struct {
	URL string `json:"url"`
}

type Log struct {
	Level string `json:"level"`
	Path  string `json:"path"`
}

type Config struct {
	RunOnce  bool     `json:"run_once"`
	RepoPath string   `json:"repo_path"`
	Log      Log      `json:"log"`
	Schedule Schedule `json:"schedule"`
	Web      Web      `json:"web"`
	Values   []Value  `json:"values"`
}

type Web struct {
	Port    int  `json:"port"`
	Enabled bool `json:"enabled"`
}

type Schedule struct {
	Interval int `json:"interval"`
}

type Value struct {
	Source  GitSource   `json:"source"`
	Mirrors []GitSource `json:"mirrors"`
}

func (c *Config) PostProcess() {
	if c.RepoPath == "" {
		c.RepoPath = path.Join("tmp", "repos")
	}
	c.RepoPath = path.Clean(c.RepoPath)
	if c.Log.Path == "" {
		c.Log.Path = path.Join("tmp", "logs")
	}
	c.Log.Path = path.Clean(c.Log.Path)
	if c.Schedule.Interval == 0 {
		c.Schedule.Interval = int(time.Hour.Nanoseconds())
	}
	if c.Web.Port == 0 {
		c.Web.Port = 8080
	}
}

func (c *Config) GetScheduleInterval() time.Duration {
	return time.Duration(c.Schedule.Interval) * time.Second
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
