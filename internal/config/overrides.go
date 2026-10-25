package config

import (
	"encoding/json"
	"fmt"
	"slices"
)

// OverridesKey names the database custom data item in which keetui keeps a
// database's settings.
const OverridesKey = "KEETUI_SETTINGS"

// Overrides are settings stored in a database, encrypted with it, and
// layered over the global configuration while that database is open. Only
// settings that belong to a vault can be set there: the theme, the strength
// display and keybindings stay global. A nil field inherits the global
// setting. The JSON keys are those of the config file.
type Overrides struct {
	ClipboardClearSeconds *int    `json:"clipboard_clear_seconds,omitempty"`
	ClipboardOSC52        *string `json:"clipboard_osc52,omitempty"`
	AutoLockMinutes       *int    `json:"auto_lock_minutes,omitempty"`
	PasswordLength        *int    `json:"password_length,omitempty"`
	PasswordUppercase     *bool   `json:"password_uppercase,omitempty"`
	PasswordLowercase     *bool   `json:"password_lowercase,omitempty"`
	PasswordDigits        *bool   `json:"password_digits,omitempty"`
	PasswordSymbols       *bool   `json:"password_symbols,omitempty"`
	DefaultPreset         *string `json:"default_preset,omitempty"`
	ConfirmQuit           *bool   `json:"confirm_quit,omitempty"`
	BackupOnSave          *bool   `json:"backup_on_save,omitempty"`
}

// ParseOverrides reads the overrides stored in a database. Keys it does not
// know are ignored, and values are repaired as Sanitize repairs the config
// file: too small a delay becomes the minimum, and a value with no valid
// reading inherits. Invalid JSON is an error and overrides nothing.
func ParseOverrides(data string) (Overrides, error) {
	var o Overrides
	if data == "" {
		return o, nil
	}
	if err := json.Unmarshal([]byte(data), &o); err != nil {
		return Overrides{}, fmt.Errorf("database settings: %w", err)
	}
	o.sanitize()
	return o, nil
}

func (o *Overrides) sanitize() {
	delay := func(p **int, minimum int) {
		switch {
		case *p == nil:
		case **p < 0:
			*p = nil
		case **p > 0 && **p < minimum:
			v := minimum
			*p = &v
		}
	}
	delay(&o.ClipboardClearSeconds, 15)
	delay(&o.AutoLockMinutes, 5)
	if o.PasswordLength != nil && *o.PasswordLength < 8 {
		o.PasswordLength = nil
	}
	if o.ClipboardOSC52 != nil && !slices.Contains(OSC52Modes, *o.ClipboardOSC52) {
		o.ClipboardOSC52 = nil
	}
	if o.DefaultPreset != nil && *o.DefaultPreset == "" {
		o.DefaultPreset = nil
	}
}

// IsZero reports whether no setting is overridden.
func (o Overrides) IsZero() bool {
	return o == Overrides{}
}

// Encode returns the overrides as stored in a database: "" when none are
// set, which removes the item.
func (o Overrides) Encode() string {
	if o.IsZero() {
		return ""
	}
	data, err := json.Marshal(o)
	if err != nil {
		panic(err) // plain values always marshal
	}
	return string(data)
}

// Apply returns c with the overrides set over it. Character sets are
// applied only together, and only when at least one stays enabled.
func (c Config) Apply(o Overrides) Config {
	if o.ClipboardClearSeconds != nil {
		c.ClipboardClearSeconds = *o.ClipboardClearSeconds
	}
	if o.ClipboardOSC52 != nil {
		c.ClipboardOSC52 = *o.ClipboardOSC52
	}
	if o.AutoLockMinutes != nil {
		c.AutoLockMinutes = *o.AutoLockMinutes
	}
	if o.PasswordLength != nil {
		c.PasswordLength = *o.PasswordLength
	}
	if o.DefaultPreset != nil {
		c.DefaultPreset = *o.DefaultPreset
	}
	if o.ConfirmQuit != nil {
		c.ConfirmQuit = boolPtr(*o.ConfirmQuit)
	}
	if o.BackupOnSave != nil {
		c.BackupOnSave = boolPtr(*o.BackupOnSave)
	}
	pick := func(override *bool, global bool) bool {
		if override != nil {
			return *override
		}
		return global
	}
	upper := pick(o.PasswordUppercase, c.PasswordHasUppercase())
	lower := pick(o.PasswordLowercase, c.PasswordHasLowercase())
	digits := pick(o.PasswordDigits, c.PasswordHasDigits())
	symbols := pick(o.PasswordSymbols, c.PasswordHasSymbols())
	if upper || lower || digits || symbols {
		c.PasswordUppercase, c.PasswordLowercase = boolPtr(upper), boolPtr(lower)
		c.PasswordDigits, c.PasswordSymbols = boolPtr(digits), boolPtr(symbols)
	}
	return c
}

func boolPtr(v bool) *bool { return &v }
