// Package config provides configuration loading for keetui.
package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// Config holds all configurable settings.
type Config struct {
	ClipboardClearSeconds int `json:"clipboard_clear_seconds"`
	AutoLockMinutes       int `json:"auto_lock_minutes"`
	PasswordLength        int `json:"password_length"`
}

// Default returns the default configuration.
func Default() Config {
	return Config{
		ClipboardClearSeconds: 15,
		AutoLockMinutes:       5,
		PasswordLength:        20,
	}
}

// Path returns the config file path (~/.config/keetui/config.json).
func Path() string {
	return configPath()
}

// ClipboardClearDuration returns the clipboard clear delay as a duration.
// Returns 0 when the feature is disabled (value is 0).
func (c Config) ClipboardClearDuration() time.Duration {
	if c.ClipboardClearSeconds == 0 {
		return 0
	}
	if c.ClipboardClearSeconds < 0 {
		return 15 * time.Second
	}
	return time.Duration(c.ClipboardClearSeconds) * time.Second
}

// AutoLockDuration returns the auto-lock timeout as a duration.
// Returns 0 when the feature is disabled (value is 0).
func (c Config) AutoLockDuration() time.Duration {
	if c.AutoLockMinutes == 0 {
		return 0
	}
	if c.AutoLockMinutes < 0 {
		return 5 * time.Minute
	}
	return time.Duration(c.AutoLockMinutes) * time.Minute
}

// PasswordLen returns the configured password length with a sane minimum.
func (c Config) PasswordLen() int {
	if c.PasswordLength < 8 {
		return 20
	}
	return c.PasswordLength
}

// String returns the config as indented JSON.
func (c Config) String() string {
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return fmt.Sprintf("{\"error\": %q}", err)
	}
	return string(data)
}

// Save writes the config to ~/.config/keetui/config.json.
func (c Config) Save() error {
	p := configPath()
	if err := os.MkdirAll(filepath.Dir(p), 0700); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}
	data = append(data, '\n')
	if err := os.WriteFile(p, data, 0600); err != nil {
		return fmt.Errorf("write config: %w", err)
	}
	return nil
}

// configPath returns ~/.config/keetui/config.json.
func configPath() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		dir = filepath.Join(os.Getenv("HOME"), ".config")
	}
	return filepath.Join(dir, "keetui", "config.json")
}

// Load reads config from ~/.config/keetui/config.json, falling back to defaults.
// Returns a non-nil error when the file exists but contains invalid JSON.
func Load() (Config, error) {
	cfg := Default()
	data, err := os.ReadFile(configPath())
	if err != nil || len(data) == 0 {
		return cfg, nil
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		return cfg, fmt.Errorf("invalid config %s: %w", configPath(), err)
	}
	return cfg, nil
}
