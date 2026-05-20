package config

import (
	"errors"
	"fmt"
	"os"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

type Config struct {
	AppID      string        `yaml:"app_id"`
	Channel    string        `yaml:"channel"`
	BaseURL    string        `yaml:"base_url"`
	InstallDir string        `yaml:"install_dir"`
	SigningKey string        `yaml:"signing_key"`
	PublicKey  string        `yaml:"public_key"`
	Backend    BackendConfig `yaml:"backend"`
}

type BackendConfig struct {
	Type           string `yaml:"type"`
	Path           string `yaml:"path"`
	Bucket         string `yaml:"bucket"`
	Region         string `yaml:"region"`
	Endpoint       string `yaml:"endpoint"`
	Prefix         string `yaml:"prefix"`
	ForcePathStyle bool   `yaml:"force_path_style"`
}

var envVarPattern = regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*)\}`)

func Load(path string) (*Config, error) {
	if strings.TrimSpace(path) == "" {
		return &Config{}, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return &Config{}, nil
		}
		return nil, err
	}
	expanded := interpolateEnv(string(data))
	var cfg Config
	if err := yaml.Unmarshal([]byte(expanded), &cfg); err != nil {
		return nil, fmt.Errorf("parse config %s: %w", path, err)
	}
	return &cfg, nil
}

func interpolateEnv(s string) string {
	return envVarPattern.ReplaceAllStringFunc(s, func(match string) string {
		name := match[2 : len(match)-1]
		return os.Getenv(name)
	})
}
