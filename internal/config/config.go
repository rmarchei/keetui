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
	ClipboardClearSeconds int          `json:"clipboard_clear_seconds"`
	AutoLockMinutes       int          `json:"auto_lock_minutes"`
	PasswordLength        int          `json:"password_length"`
	ShowPasswordStrength  bool         `json:"show_password_strength"`
	PasswordUppercase     *bool        `json:"password_uppercase,omitempty"`
	PasswordLowercase     *bool        `json:"password_lowercase,omitempty"`
	PasswordDigits        *bool        `json:"password_digits,omitempty"`
	PasswordSymbols       *bool        `json:"password_symbols,omitempty"`
	DefaultPreset         string       `json:"default_preset,omitempty"`
	ThemeName             string       `json:"theme,omitempty"`
	ConfirmQuit           *bool        `json:"confirm_quit,omitempty"`
	BackupOnSave          *bool        `json:"backup_on_save,omitempty"`
	KeyBindingsPtr        *KeyBindings `json:"keybindings,omitempty"`
}

// Default returns the default configuration.
func Default() Config {
	return Config{
		ClipboardClearSeconds: 15,
		AutoLockMinutes:       5,
		PasswordLength:        20,
		ShowPasswordStrength:  true,
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

// PasswordHasUppercase returns whether uppercase letters are included in generated passwords.
func (c Config) PasswordHasUppercase() bool {
	return c.PasswordUppercase == nil || *c.PasswordUppercase
}

// PasswordHasLowercase returns whether lowercase letters are included in generated passwords.
func (c Config) PasswordHasLowercase() bool {
	return c.PasswordLowercase == nil || *c.PasswordLowercase
}

// PasswordHasDigits returns whether digits are included in generated passwords.
func (c Config) PasswordHasDigits() bool { return c.PasswordDigits == nil || *c.PasswordDigits }

// PasswordHasSymbols returns whether symbols are included in generated passwords.
func (c Config) PasswordHasSymbols() bool { return c.PasswordSymbols == nil || *c.PasswordSymbols }

// PasswordCharset builds the character set for password generation based on config.
// If all sets are disabled, falls back to lowercase letters.
func (c Config) PasswordCharset() string {
	var charset string
	if c.PasswordHasLowercase() {
		charset += "abcdefghijklmnopqrstuvwxyz"
	}
	if c.PasswordHasUppercase() {
		charset += "ABCDEFGHIJKLMNOPQRSTUVWXYZ"
	}
	if c.PasswordHasDigits() {
		charset += "0123456789"
	}
	if c.PasswordHasSymbols() {
		charset += "!@#$%^&*()-_=+"
	}
	if charset == "" {
		charset = "abcdefghijklmnopqrstuvwxyz"
	}
	return charset
}

// ConfirmOnQuit returns whether the user should always be prompted before quitting.
func (c Config) ConfirmOnQuit() bool {
	return c.ConfirmQuit != nil && *c.ConfirmQuit
}

// BackupEnabled returns whether a .bak copy should be created before saving.
func (c Config) BackupEnabled() bool {
	return c.BackupOnSave != nil && *c.BackupOnSave
}

// Preset returns the configured default preset name, defaulting to "Strong".
func (c Config) Preset() string {
	if c.DefaultPreset == "" {
		return "Strong"
	}
	return c.DefaultPreset
}

// Theme returns the configured theme name, defaulting to "dark".
func (c Config) Theme() string {
	if c.ThemeName == "" {
		return "dark"
	}
	return c.ThemeName
}

// Keys returns the merged keybindings (custom overrides + defaults).
// If no custom keybindings are set, all defaults are returned.
func (c Config) Keys() KeyBindings {
	defaults := DefaultKeyBindings()
	if c.KeyBindingsPtr == nil {
		return defaults
	}
	return c.KeyBindingsPtr.Merge(defaults)
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
	// Ensure permissions are 0600 even if the file already existed with
	// looser permissions (os.WriteFile only sets mode on new files).
	if err := os.Chmod(p, 0600); err != nil {
		return fmt.Errorf("set config file permissions: %w", err)
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

// Sanitize clamps config values to safe ranges, matching the validation
// performed by the settings UI. This prevents malicious or malformed config
// files from producing unexpected behavior.
func (c *Config) Sanitize() {
	if c.ClipboardClearSeconds < 0 {
		c.ClipboardClearSeconds = 15
	}
	if c.ClipboardClearSeconds > 0 && c.ClipboardClearSeconds < 15 {
		c.ClipboardClearSeconds = 15
	}
	if c.AutoLockMinutes < 0 {
		c.AutoLockMinutes = 5
	}
	if c.AutoLockMinutes > 0 && c.AutoLockMinutes < 5 {
		c.AutoLockMinutes = 5
	}
	if c.PasswordLength < 8 {
		c.PasswordLength = 20
	}
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
	cfg.Sanitize()
	return cfg, nil
}
