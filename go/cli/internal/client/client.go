// Package client wraps the generated SDK with sensible defaults and
// credential management.
package client

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"

	"github.com/fuegoio/sunred/go/sdk/sunred"
)

// DefaultBaseURL is the default API base URL used when the config file is
// absent or has no base_url set. Points at the hosted API so the CLI works
// out of the box against sunred.app; self-hosters override it via
// `sunred config set base_url <url>` or `--url`.
const DefaultBaseURL = "https://api.sunred.app"

// Config holds the CLI client configuration, loaded from the config file.
type Config struct {
	BaseURL string `json:"base_url"`
	Token   string `json:"token"`
}

// configDir returns the directory where CLI config is stored.
func configDir() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("user config dir: %w", err)
	}
	return filepath.Join(dir, "sunred"), nil
}

// ConfigPath returns the path to the CLI config file.
func ConfigPath() (string, error) {
	dir, err := configDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "config.json"), nil
}

// LoadConfig reads the config file from the user's config directory.
// If the file doesn't exist, it returns a default config.
func LoadConfig() (*Config, error) {
	path, err := ConfigPath()
	if err != nil {
		return nil, err
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &Config{BaseURL: DefaultBaseURL}, nil
		}
		return nil, fmt.Errorf("read config: %w", err)
	}

	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}
	if cfg.BaseURL == "" {
		cfg.BaseURL = DefaultBaseURL
	}
	return &cfg, nil
}

// SaveConfig writes the config to the config file.
func SaveConfig(cfg *Config) error {
	dir, err := configDir()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}

	path, err := ConfigPath()
	if err != nil {
		return err
	}

	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}

	if err := os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("write config: %w", err)
	}
	return nil
}

// New creates a new SDK client with responses from the given config.
func New(cfg *Config) (*sunred.ClientWithResponses, error) {
	var opts []sunred.ClientOption

	if cfg.Token != "" {
		opts = append(opts, sunred.WithRequestEditorFn(func(_ context.Context, req *http.Request) error {
			req.Header.Set("Authorization", "Bearer "+cfg.Token)
			return nil
		}))
	}

	return sunred.NewClientWithResponses(cfg.BaseURL, opts...)
}
