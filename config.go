package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

const cwdConfigPath = "quadwatch.yaml"

type Config struct {
	GitHubReleases map[string]string `yaml:"github_releases"`
}

func loadConfig(path string) (Config, error) {
	cfg := Config{GitHubReleases: map[string]string{}}
	explicit := path != ""
	if !explicit {
		candidates, err := defaultConfigPaths()
		if err != nil {
			return cfg, err
		}
		for _, candidate := range candidates {
			if _, err := os.Stat(candidate); err == nil {
				path = candidate
				break
			} else if !os.IsNotExist(err) {
				return cfg, err
			}
		}
		if path == "" {
			return cfg, nil
		}
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if !explicit && os.IsNotExist(err) {
			return cfg, nil
		}
		return cfg, err
	}
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return cfg, fmt.Errorf("parse config %s: %w", path, err)
	}
	if cfg.GitHubReleases == nil {
		cfg.GitHubReleases = map[string]string{}
	}
	for image, repo := range cfg.GitHubReleases {
		if strings.TrimSpace(image) == "" || strings.TrimSpace(repo) == "" {
			return cfg, fmt.Errorf("config %s has an empty github_releases entry", path)
		}
	}
	return cfg, nil
}

func defaultConfigPaths() ([]string, error) {
	configDir, err := xdgConfigDir()
	if err != nil {
		return nil, err
	}
	return []string{
		cwdConfigPath,
		filepath.Join(configDir, "quadwatch", "config.yaml"),
	}, nil
}

func xdgConfigDir() (string, error) {
	if dir := strings.TrimSpace(os.Getenv("XDG_CONFIG_HOME")); dir != "" {
		return dir, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config"), nil
}
