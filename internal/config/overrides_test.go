package config

import (
	"strings"
	"testing"
)

func TestParseOverridesRepairsLikeTheConfigFile(t *testing.T) {
	o, err := ParseOverrides(`{
		"clipboard_clear_seconds": 5,
		"auto_lock_minutes": -1,
		"password_length": 4,
		"clipboard_osc52": "sometimes",
		"default_preset": "",
		"theme": "light",
		"keybindings": {"quit": "x"},
		"backup_on_save": true,
		"unknown_future_setting": 1
	}`)
	if err != nil {
		t.Fatal(err)
	}
	if o.ClipboardClearSeconds == nil || *o.ClipboardClearSeconds != 15 {
		t.Errorf("clipboard clear 5 should clamp to 15, got %v", o.ClipboardClearSeconds)
	}
	for name, dropped := range map[string]bool{
		"auto_lock_minutes -1":      o.AutoLockMinutes == nil,
		"password_length 4":         o.PasswordLength == nil,
		"clipboard_osc52 sometimes": o.ClipboardOSC52 == nil,
		"empty default_preset":      o.DefaultPreset == nil,
	} {
		if !dropped {
			t.Errorf("%s should inherit", name)
		}
	}
	if o.BackupOnSave == nil || !*o.BackupOnSave {
		t.Error("backup_on_save lost")
	}
	// Theme and keybindings are not database settings: re-encoding drops them.
	if enc := o.Encode(); strings.Contains(enc, "theme") || strings.Contains(enc, "keybindings") {
		t.Errorf("encoded overrides carry global-only settings: %s", enc)
	}

	if _, err := ParseOverrides(`{"auto_lock_minutes": "ten"}`); err == nil {
		t.Error("a mistyped value should be an error")
	}
	if o, err := ParseOverrides(""); err != nil || !o.IsZero() {
		t.Errorf("no item = no overrides, got %+v, %v", o, err)
	}
}

func TestOverridesEncodeRoundTrips(t *testing.T) {
	if (Overrides{}).Encode() != "" {
		t.Error("no overrides should encode to nothing, removing the item")
	}
	ten, never, no := 10, "never", false
	o := Overrides{AutoLockMinutes: &ten, ClipboardOSC52: &never, ConfirmQuit: &no}
	back, err := ParseOverrides(o.Encode())
	if err != nil {
		t.Fatal(err)
	}
	if *back.AutoLockMinutes != 10 || *back.ClipboardOSC52 != "never" || *back.ConfirmQuit || back.PasswordLength != nil {
		t.Errorf("round trip changed the overrides: %s", back.Encode())
	}
}

func TestApplyLayersOverridesOnTheGlobalConfig(t *testing.T) {
	global := Default()
	global.ThemeName = "light"
	thirty, length, always, yes := 30, 32, "always", true
	cfg := global.Apply(Overrides{
		ClipboardClearSeconds: &thirty,
		PasswordLength:        &length,
		ClipboardOSC52:        &always,
		BackupOnSave:          &yes,
	})
	if cfg.ClipboardClearSeconds != 30 || cfg.PasswordLength != 32 || cfg.OSC52Mode() != "always" || !cfg.BackupEnabled() {
		t.Errorf("overrides not applied: %+v", cfg)
	}
	if cfg.AutoLockMinutes != global.AutoLockMinutes || cfg.Theme() != "light" {
		t.Error("settings without an override must inherit")
	}
	if global.ClipboardClearSeconds != 15 || global.BackupEnabled() {
		t.Error("Apply changed the global config")
	}

	// Turning every character set off is refused: the global ones stay.
	off := false
	cfg = global.Apply(Overrides{PasswordUppercase: &off, PasswordLowercase: &off, PasswordDigits: &off, PasswordSymbols: &off})
	if cfg.PasswordCharset() != global.PasswordCharset() {
		t.Error("overrides disabling every character set should be ignored")
	}
	cfg = global.Apply(Overrides{PasswordSymbols: &off})
	if cfg.PasswordHasSymbols() || !cfg.PasswordHasDigits() {
		t.Error("a single character set override was not applied")
	}
}
