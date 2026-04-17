package main

import (
	"os"

	"gopkg.in/yaml.v3"
)

type FilterConfig struct {
	From    string `yaml:"from"`
	Subject string `yaml:"subject"`
}

type Config struct {
	PollIntervalSeconds int            `yaml:"poll_interval_seconds"`
	Filters             []FilterConfig `yaml:"filters"`
	TokenFile           string         `yaml:"token_file"`
	CredentialsFile     string         `yaml:"credentials_file"`
}

func defaultConfig() *Config {
	return &Config{
		PollIntervalSeconds: 30,
		TokenFile:           "token.json",
		CredentialsFile:     "credentials.json",
		Filters:             []FilterConfig{},
	}
}

func loadConfig(path string) (*Config, error) {
	cfg := defaultConfig()

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, nil
		}
		return nil, err
	}

	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, err
	}

	if cfg.PollIntervalSeconds <= 0 {
		cfg.PollIntervalSeconds = 30
	}
	if cfg.TokenFile == "" {
		cfg.TokenFile = "token.json"
	}
	if cfg.CredentialsFile == "" {
		cfg.CredentialsFile = "credentials.json"
	}

	return cfg, nil
}
