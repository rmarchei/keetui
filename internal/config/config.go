// Package config provides configuration loading for keetui.
package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

// Config holds all configurable settings.
type Config struct {
	ClipboardClearSeconds int          `json:"clipboard_clear_seconds"`
	ClipboardOSC52        string       `json:"clipboard_osc52,omitempty"`
	AutoLockMinutes       int          `json:"auto_lock_minutes"`
	PrivacyMode           *bool        `json:"privacy_mode,omitempty"`
	PasswordLength        int          `json:"password_length"`
	ShowPasswordStrength  bool         `json:"show_password_strength"`
	PasswordUppercase     *bool        `json:"password_uppercase,omitempty"`
	PasswordLowercase     *bool        `json:"password_lowercase,omitempty"`
	PasswordDigits        *bool        `json:"password_digits,omitempty"`
	PasswordSymbols       *bool        `json:"password_symbols,omitempty"`
	DefaultPreset         string       `json:"default_preset,omitempty"`
	PINLength             int          `json:"pin_length,omitempty"`
	PassphraseWords       int          `json:"passphrase_words,omitempty"`
	PassphraseSeparator   string       `json:"passphrase_separator,omitempty"`
	ThemeName             string       `json:"theme,omitempty"`
	ConfirmQuit           *bool        `json:"confirm_quit,omitempty"`
	BackupOnSave          *bool        `json:"backup_on_save,omitempty"`
	KeyBindingsPtr        *KeyBindings `json:"keybindings,omitempty"`
	// Colors overrides theme colors: theme name, then role (ColorRoles), then
	// a color. "auto" picks the overrides of "dark" or "light".
	Colors map[string]map[string]string `json:"colors,omitempty"`
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

// Terminal clipboard (OSC 52) policies.
const (
	OSC52Auto   = "auto"   // only when no local clipboard is reachable
	OSC52Always = "always" // in addition to the local clipboard
	OSC52Never  = "never"
)

// OSC52Modes lists the valid clipboard_osc52 values.
var OSC52Modes = []string{OSC52Auto, OSC52Always, OSC52Never}

// PresetNames lists the password presets default_preset may name.
var PresetNames = []string{"PIN", "Alphanumeric", "Strong", "Passphrase", "Custom"}

// CanonicalPreset returns the preset a name means, ignoring case.
func CanonicalPreset(name string) (string, bool) {
	for _, preset := range PresetNames {
		if strings.EqualFold(name, preset) {
			return preset, true
		}
	}
	return "", false
}

// OSC52Mode returns the terminal clipboard policy, defaulting to "auto".
// OSC 52 copies cannot be marked as sensitive, and terminal multiplexers
// and session recorders may keep them, so they are used only when needed.
func (c Config) OSC52Mode() string {
	switch c.ClipboardOSC52 {
	case OSC52Always, OSC52Never:
		return c.ClipboardOSC52
	}
	return OSC52Auto
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
// Confirmation is enabled unless explicitly disabled.
func (c Config) ConfirmOnQuit() bool {
	return c.ConfirmQuit == nil || *c.ConfirmQuit
}

// Generator limits. Passphrase words carry about 10 bits each, so the
// default 8 give about 80.
const (
	MinPINLength, MaxPINLength, DefaultPINLength                   = 4, 16, 6
	MinPassphraseWords, MaxPassphraseWords, DefaultPassphraseWords = 4, 12, 8
	MaxSeparatorLength                                             = 3
	DefaultSeparator                                               = "-"
)

// PINLen returns the length of generated PINs.
func (c Config) PINLen() int {
	if c.PINLength < MinPINLength || c.PINLength > MaxPINLength {
		return DefaultPINLength
	}
	return c.PINLength
}

// PassphraseWordCount returns the number of words in generated passphrases.
func (c Config) PassphraseWordCount() int {
	if c.PassphraseWords < MinPassphraseWords || c.PassphraseWords > MaxPassphraseWords {
		return DefaultPassphraseWords
	}
	return c.PassphraseWords
}

// PassphraseSep returns what separates the words of generated passphrases.
func (c Config) PassphraseSep() string {
	if !ValidSeparator(c.PassphraseSeparator) {
		return DefaultSeparator
	}
	return c.PassphraseSeparator
}

// ValidSeparator reports a passphrase separator keetui accepts: one to
// MaxSeparatorLength visible characters, no spaces.
func ValidSeparator(s string) bool {
	n := utf8.RuneCountInString(s)
	return n >= 1 && n <= MaxSeparatorLength && utf8.ValidString(s) &&
		!strings.ContainsFunc(s, func(r rune) bool { return !unicode.IsGraphic(r) || unicode.IsSpace(r) })
}

// HidePasswords reports privacy mode: passwords stay hidden while editing,
// with no brief reveal when typing and generated passwords masked.
func (c Config) HidePasswords() bool {
	return c.PrivacyMode != nil && *c.PrivacyMode
}

// BackupEnabled returns whether a .bak copy should be created before saving.
func (c Config) BackupEnabled() bool {
	return c.BackupOnSave != nil && *c.BackupOnSave
}

// Preset returns the configured default preset name, defaulting to
// "Strong", also for a name that is no preset: falling back to the first
// one would mean a 6-digit PIN.
func (c Config) Preset() string {
	if name, ok := CanonicalPreset(c.DefaultPreset); ok {
		return name
	}
	return "Strong"
}

// Theme returns the configured theme name, defaulting to "auto", which
// follows the terminal's background.
func (c Config) Theme() string {
	if c.ThemeName == "" {
		return "auto"
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
	return writeFileAtomic(p, append(data, '\n'))
}

// writeFileAtomic replaces path with data, readable by the owner only, so
// that an interrupted save (a crash, a full disk) leaves the old file
// whole: data goes to a temporary file in the same directory, reaches the
// disk, and is renamed over path. A symlink at path, as dotfile managers
// make, is followed: the file it points to is replaced and the link kept.
func writeFileAtomic(path string, data []byte) (err error) {
	if target, err := filepath.EvalSymlinks(path); err == nil {
		path = target
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".*.tmp")
	if err != nil {
		return fmt.Errorf("write config: %w", err)
	}
	defer func() {
		if err != nil {
			_ = tmp.Close()
			_ = os.Remove(tmp.Name())
		}
	}()
	// CreateTemp makes the file 0600; Chmod keeps that explicit, whatever
	// the platform's default.
	if err := tmp.Chmod(0600); err != nil {
		return fmt.Errorf("set config file permissions: %w", err)
	}
	if _, err := tmp.Write(data); err != nil {
		return fmt.Errorf("write config: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		return fmt.Errorf("write config: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("write config: %w", err)
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		return fmt.Errorf("write config: %w", err)
	}
	// Persist the rename itself; a directory that cannot be synced still
	// holds the new file.
	if dir, err := os.Open(filepath.Dir(path)); err == nil {
		_ = dir.Sync()
		_ = dir.Close()
	}
	return nil
}

// configPath returns the config file path: $XDG_CONFIG_HOME/keetui/config.json,
// or ~/.config/keetui/config.json when XDG_CONFIG_HOME is unset — on all
// platforms, as documented in the README. If no file exists there but one
// does at the platform-native os.UserConfigDir location (used by older
// keetui versions on macOS: ~/Library/Application Support), the legacy path
// is used so existing configs keep working.
func configPath() string {
	p := filepath.Join(configDir(), "keetui", "config.json")
	if _, err := os.Stat(p); err == nil {
		return p
	}
	if legacyDir, err := os.UserConfigDir(); err == nil {
		legacy := filepath.Join(legacyDir, "keetui", "config.json")
		if legacy != p {
			if _, err := os.Stat(legacy); err == nil {
				return legacy
			}
		}
	}
	return p
}

// configDir returns $XDG_CONFIG_HOME, or ~/.config when unset.
func configDir() string {
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		return xdg
	}
	return filepath.Join(os.Getenv("HOME"), ".config")
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
	if c.ClipboardOSC52 != "" && c.OSC52Mode() != c.ClipboardOSC52 {
		c.ClipboardOSC52 = ""
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
	c.DefaultPreset, _ = CanonicalPreset(c.DefaultPreset) // "": Strong
	if c.PINLength != 0 && c.PINLen() != c.PINLength {
		c.PINLength = 0
	}
	if c.PassphraseWords != 0 && c.PassphraseWordCount() != c.PassphraseWords {
		c.PassphraseWords = 0
	}
	if c.PassphraseSeparator != "" && !ValidSeparator(c.PassphraseSeparator) {
		c.PassphraseSeparator = ""
	}
}

// Load reads config from ~/.config/keetui/config.json, falling back to defaults.
// Returns a non-nil error when the file cannot be read (other than not
// existing), contains invalid JSON, or has conflicting keybindings; in all
// error cases the returned config is safe to use.
func Load() (Config, error) {
	cfg := Default()
	data, err := os.ReadFile(configPath())
	if errors.Is(err, fs.ErrNotExist) || (err == nil && len(data) == 0) {
		return cfg, nil
	}
	if err != nil {
		return cfg, fmt.Errorf("read config %s: %w", configPath(), err)
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		// json.Unmarshal may have partially populated cfg before failing
		// (type errors); return clean defaults instead of a partial decode.
		return Default(), fmt.Errorf("invalid config %s: %w", configPath(), err)
	}
	cfg.Sanitize()
	var problems []error
	if cfg.KeyBindingsPtr != nil {
		cfg.KeyBindingsPtr, err = repairBindings(*cfg.KeyBindingsPtr)
		if err != nil {
			problems = append(problems, fmt.Errorf("config %s: %w", configPath(), err))
		}
	}
	if dropped := cfg.sanitizeColors(); len(dropped) > 0 {
		problems = append(problems, fmt.Errorf("config %s: ignored colors: %s", configPath(), strings.Join(dropped, "; ")))
	}
	return cfg, errors.Join(problems...)
}

// ColorRoles are the theme colors the colors setting can override.
var ColorRoles = []string{
	"primary", "label", "muted", "success", "error", "warn", "text",
	"title_fg", "status_bg", "status_fg", "selection_bg", "selection_fg", "canvas",
}

// IsColor reports whether s is a color the colors setting accepts: "#RGB"
// or "#RRGGBB", an ANSI color number from 0 to 255 (the terminal's palette),
// or "" for the terminal's default color.
func IsColor(s string) bool {
	if s == "" {
		return true
	}
	if hex, ok := strings.CutPrefix(s, "#"); ok {
		if len(hex) != 3 && len(hex) != 6 {
			return false
		}
		_, err := strconv.ParseUint(hex, 16, 32)
		return err == nil
	}
	n, err := strconv.Atoi(s)
	return err == nil && n >= 0 && n <= 255 && strconv.Itoa(n) == s
}

// sanitizeColors drops color overrides with an unknown role or a value that
// is not a color, and describes what it dropped.
func (c *Config) sanitizeColors() []string {
	var dropped []string
	for theme, roles := range c.Colors {
		for role, value := range roles {
			switch {
			case !slices.Contains(ColorRoles, role):
				dropped = append(dropped, fmt.Sprintf("%s.%s: unknown role", theme, role))
			case !IsColor(value):
				dropped = append(dropped, fmt.Sprintf("%s.%s: %q is not a color", theme, role, value))
			default:
				continue
			}
			delete(roles, role)
		}
		if len(roles) == 0 {
			delete(c.Colors, theme)
		}
	}
	slices.Sort(dropped)
	return dropped
}
