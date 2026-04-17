package main

import (
	"os"

	"gopkg.in/yaml.v3"
)

type FilterConfig struct {
	From    string `yaml:"from"`
	Subject string `yaml:"subject"`
}

type SoundConfig struct {
	Enabled         bool    `yaml:"enabled"`
	File            string  `yaml:"file"`             // 커스텀 사운드 파일 경로 (WAV/MP3)
	BeepFrequency   float64 `yaml:"beep_frequency"`   // Hz (file이 없을 때 사용)
	BeepDurationMs  int     `yaml:"beep_duration_ms"` // 밀리초
}

type Config struct {
	PollIntervalSeconds int            `yaml:"poll_interval_seconds"`
	Filters             []FilterConfig `yaml:"filters"`
	TokenFile           string         `yaml:"token_file"`
	CredentialsFile     string         `yaml:"credentials_file"`
	Sound               SoundConfig    `yaml:"sound"`
}

func defaultConfig() *Config {
	return &Config{
		PollIntervalSeconds: 30,
		TokenFile:           "token.json",
		CredentialsFile:     "credentials.json",
		Filters:             []FilterConfig{},
		Sound: SoundConfig{
			Enabled:        true,
			BeepFrequency:  880,
			BeepDurationMs: 300,
		},
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
	if cfg.Sound.BeepFrequency <= 0 {
		cfg.Sound.BeepFrequency = 880
	}
	if cfg.Sound.BeepDurationMs <= 0 {
		cfg.Sound.BeepDurationMs = 300
	}

	return cfg, nil
}
