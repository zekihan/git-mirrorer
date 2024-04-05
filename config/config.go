package config

import (
	"encoding/json"
	log "github.com/sirupsen/logrus"
	"io"
	"os"
	"path"
	"regexp"
	"strings"
)

const defaultConfigPath = "config.jsonc"

var config *Config

func ReadConfig() *Config {
	configPath := os.Getenv("CONFIG_PATH")
	if configPath == "" {
		cwd, err := os.Getwd()
		if err != nil {
			log.Fatal(err)
		}
		configPath = path.Join(cwd, defaultConfigPath)
	}

	file, err := os.Open(configPath)
	if err != nil {
		log.Fatal(err)
	}
	defer file.Close()

	sanitized := removeComment(file)
	jsonParser := json.NewDecoder(sanitized)
	config = &Config{}
	err = jsonParser.Decode(&config)
	if err != nil {
		log.Fatal(err)
	}
	if config == nil {
		log.Fatal("Config is empty")
	}
	valid, err := config.Valid()
	if !valid && err != nil {
		log.Fatal(err)
	}
	config.PostProcess()
	return config
}

func GetConfig() *Config {
	if config == nil {
		return ReadConfig()
	}
	return config
}

func removeComment(reader io.Reader) (newReader io.Reader) {
	bs, err := io.ReadAll(reader)
	if err != nil {
		log.Fatalf("%v", err)
	}
	s := string(bs)
	re1 := regexp.MustCompile(`(?im)^\s+//.*$`)
	s = re1.ReplaceAllString(s, "")
	re2 := regexp.MustCompile(`(?im)//[^"\[\]]+$`)
	s = re2.ReplaceAllString(s, "")
	newReader = strings.NewReader(s)
	return
}
