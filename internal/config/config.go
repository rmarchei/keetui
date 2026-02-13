// Package config provides configuration loading for keetui.
package config

import (
	"encoding/json"
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

// ClipboardClearDuration returns the clipboard clear delay as a duration.
func (c Config) ClipboardClearDuration() time.Duration {
	if c.ClipboardClearSeconds <= 0 {
		return 15 * time.Second
	}
	return time.Duration(c.ClipboardClearSeconds) * time.Second
}

// AutoLockDuration returns the auto-lock timeout as a duration.
func (c Config) AutoLockDuration() time.Duration {
	if c.AutoLockMinutes <= 0 {
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

// configPath returns ~/.config/keetui/config.json.
func configPath() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		dir = filepath.Join(os.Getenv("HOME"), ".config")
	}
	return filepath.Join(dir, "keetui", "config.json")
}

// Load reads config from ~/.config/keetui/config.json, falling back to defaults.
func Load() Config {
	cfg := Default()
	data, err := os.ReadFile(configPath())
	if err != nil {
		return cfg
	}
	_ = json.Unmarshal(data, &cfg)
	return cfg
}
