package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestDefault(t *testing.T) {
	cfg := Default()
	if cfg.ClipboardClearSeconds != 15 {
		t.Errorf("expected ClipboardClearSeconds=15, got %d", cfg.ClipboardClearSeconds)
	}
	if cfg.AutoLockMinutes != 5 {
		t.Errorf("expected AutoLockMinutes=5, got %d", cfg.AutoLockMinutes)
	}
	if cfg.PasswordLength != 20 {
		t.Errorf("expected PasswordLength=20, got %d", cfg.PasswordLength)
	}
}

func TestClipboardClearDuration(t *testing.T) {
	tests := []struct {
		name    string
		seconds int
		want    time.Duration
	}{
		{"default", 15, 15 * time.Second},
		{"custom", 30, 30 * time.Second},
		{"zero disables", 0, 0},
		{"negative falls back", -1, 15 * time.Second},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := Config{ClipboardClearSeconds: tt.seconds}
			if got := cfg.ClipboardClearDuration(); got != tt.want {
				t.Errorf("ClipboardClearDuration() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestAutoLockDuration(t *testing.T) {
	tests := []struct {
		name    string
		minutes int
		want    time.Duration
	}{
		{"default", 5, 5 * time.Minute},
		{"custom", 10, 10 * time.Minute},
		{"zero disables", 0, 0},
		{"negative falls back", -1, 5 * time.Minute},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := Config{AutoLockMinutes: tt.minutes}
			if got := cfg.AutoLockDuration(); got != tt.want {
				t.Errorf("AutoLockDuration() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestPasswordLen(t *testing.T) {
	tests := []struct {
		name   string
		length int
		want   int
	}{
		{"default", 20, 20},
		{"custom", 32, 32},
		{"minimum enforced", 4, 20},
		{"exactly 8", 8, 8},
		{"zero falls back", 0, 20},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := Config{PasswordLength: tt.length}
			if got := cfg.PasswordLen(); got != tt.want {
				t.Errorf("PasswordLen() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestPasswordCharsetDefaults(t *testing.T) {
	cfg := Default()
	charset := cfg.PasswordCharset()
	if !strings.Contains(charset, "a") {
		t.Error("default charset missing lowercase")
	}
	if !strings.Contains(charset, "A") {
		t.Error("default charset missing uppercase")
	}
	if !strings.Contains(charset, "0") {
		t.Error("default charset missing digits")
	}
	if !strings.Contains(charset, "!") {
		t.Error("default charset missing symbols")
	}
}

func TestPasswordCharsetCustom(t *testing.T) {
	f := false
	tr := true
	cfg := Config{
		PasswordUppercase: &f,
		PasswordLowercase: &tr,
		PasswordDigits:    &f,
		PasswordSymbols:   &f,
	}
	charset := cfg.PasswordCharset()
	if strings.ContainsAny(charset, "ABCDEFGHIJKLMNOPQRSTUVWXYZ") {
		t.Error("charset should not contain uppercase")
	}
	if !strings.Contains(charset, "a") {
		t.Error("charset should contain lowercase")
	}
	if strings.ContainsAny(charset, "0123456789") {
		t.Error("charset should not contain digits")
	}
	if strings.ContainsAny(charset, "!@#$%^&*()-_=+") {
		t.Error("charset should not contain symbols")
	}
}

func TestPasswordCharsetAllDisabledFallback(t *testing.T) {
	f := false
	cfg := Config{
		PasswordUppercase: &f,
		PasswordLowercase: &f,
		PasswordDigits:    &f,
		PasswordSymbols:   &f,
	}
	charset := cfg.PasswordCharset()
	if charset != "abcdefghijklmnopqrstuvwxyz" {
		t.Errorf("all-disabled charset = %q, want lowercase fallback", charset)
	}
}

func TestString(t *testing.T) {
	cfg := Default()
	s := cfg.String()
	if s == "" {
		t.Error("String() returned empty string")
	}
	// String() now returns JSON
	var parsed Config
	if err := json.Unmarshal([]byte(s), &parsed); err != nil {
		t.Fatalf("String() returned invalid JSON: %v", err)
	}
	if parsed.ClipboardClearSeconds != 15 {
		t.Errorf("ClipboardClearSeconds = %d, want 15", parsed.ClipboardClearSeconds)
	}
	if parsed.AutoLockMinutes != 5 {
		t.Errorf("AutoLockMinutes = %d, want 5", parsed.AutoLockMinutes)
	}
	if parsed.PasswordLength != 20 {
		t.Errorf("PasswordLength = %d, want 20", parsed.PasswordLength)
	}
}

func TestPath(t *testing.T) {
	p := Path()
	if p == "" {
		t.Error("Path() returned empty string")
	}
	if filepath.Base(p) != "config.json" {
		t.Errorf("Path() base = %q, want config.json", filepath.Base(p))
	}
}

func TestSaveAndLoad(t *testing.T) {
	tmpDir := t.TempDir()

	cfg := Config{
		ClipboardClearSeconds: 30,
		AutoLockMinutes:       10,
		PasswordLength:        32,
	}

	// Write config JSON to a temp file and verify it round-trips
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	path := filepath.Join(tmpDir, "config.json")
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatalf("write: %v", err)
	}

	// Read it back
	readData, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}

	var loaded Config
	if err := json.Unmarshal(readData, &loaded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if loaded.ClipboardClearSeconds != 30 {
		t.Errorf("ClipboardClearSeconds = %d, want 30", loaded.ClipboardClearSeconds)
	}
	if loaded.AutoLockMinutes != 10 {
		t.Errorf("AutoLockMinutes = %d, want 10", loaded.AutoLockMinutes)
	}
	if loaded.PasswordLength != 32 {
		t.Errorf("PasswordLength = %d, want 32", loaded.PasswordLength)
	}
}

func TestThemeDefault(t *testing.T) {
	cfg := Default()
	if got := cfg.Theme(); got != "auto" {
		t.Errorf("Theme() = %q, want %q", got, "auto")
	}
}

func TestSanitizeClamps(t *testing.T) {
	tests := []struct {
		name      string
		cfg       Config
		wantClip  int
		wantLock  int
		wantPWLen int
	}{
		{"negative clipboard", Config{ClipboardClearSeconds: -5, AutoLockMinutes: 5, PasswordLength: 20}, 15, 5, 20},
		{"too low clipboard", Config{ClipboardClearSeconds: 3, AutoLockMinutes: 5, PasswordLength: 20}, 15, 5, 20},
		{"zero clipboard ok", Config{ClipboardClearSeconds: 0, AutoLockMinutes: 5, PasswordLength: 20}, 0, 5, 20},
		{"negative auto-lock", Config{ClipboardClearSeconds: 15, AutoLockMinutes: -1, PasswordLength: 20}, 15, 5, 20},
		{"too low auto-lock", Config{ClipboardClearSeconds: 15, AutoLockMinutes: 2, PasswordLength: 20}, 15, 5, 20},
		{"zero auto-lock ok", Config{ClipboardClearSeconds: 15, AutoLockMinutes: 0, PasswordLength: 20}, 15, 0, 20},
		{"short password", Config{ClipboardClearSeconds: 15, AutoLockMinutes: 5, PasswordLength: 4}, 15, 5, 20},
		{"valid values unchanged", Config{ClipboardClearSeconds: 30, AutoLockMinutes: 10, PasswordLength: 32}, 30, 10, 32},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.cfg.Sanitize()
			if tt.cfg.ClipboardClearSeconds != tt.wantClip {
				t.Errorf("ClipboardClearSeconds = %d, want %d", tt.cfg.ClipboardClearSeconds, tt.wantClip)
			}
			if tt.cfg.AutoLockMinutes != tt.wantLock {
				t.Errorf("AutoLockMinutes = %d, want %d", tt.cfg.AutoLockMinutes, tt.wantLock)
			}
			if tt.cfg.PasswordLength != tt.wantPWLen {
				t.Errorf("PasswordLength = %d, want %d", tt.cfg.PasswordLength, tt.wantPWLen)
			}
		})
	}
}

func TestThemeCustom(t *testing.T) {
	cfg := Config{ThemeName: "light"}
	if got := cfg.Theme(); got != "light" {
		t.Errorf("Theme() = %q, want %q", got, "light")
	}
}

func TestConfirmOnQuitDefault(t *testing.T) {
	cfg := Default()
	if !cfg.ConfirmOnQuit() {
		t.Error("ConfirmOnQuit() should be true by default")
	}
}

func TestConfirmOnQuitEnabled(t *testing.T) {
	tr := true
	cfg := Config{ConfirmQuit: &tr}
	if !cfg.ConfirmOnQuit() {
		t.Error("ConfirmOnQuit() should be true when set")
	}
}

func TestConfirmOnQuitJSON(t *testing.T) {
	for _, tt := range []struct {
		name string
		json string
		want bool
	}{
		{"omitted", `{}`, true},
		{"null", `{"confirm_quit":null}`, true},
		{"enabled", `{"confirm_quit":true}`, true},
		{"disabled", `{"confirm_quit":false}`, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			cfg := Default()
			if err := json.Unmarshal([]byte(tt.json), &cfg); err != nil {
				t.Fatal(err)
			}
			if got := cfg.ConfirmOnQuit(); got != tt.want {
				t.Errorf("ConfirmOnQuit() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestBackupEnabledDefault(t *testing.T) {
	cfg := Default()
	if cfg.BackupEnabled() {
		t.Error("BackupEnabled() should be false by default")
	}
}

func TestBackupEnabledSet(t *testing.T) {
	tr := true
	cfg := Config{BackupOnSave: &tr}
	if !cfg.BackupEnabled() {
		t.Error("BackupEnabled() should be true when set")
	}
}

func TestPresetDefault(t *testing.T) {
	cfg := Default()
	if got := cfg.Preset(); got != "Strong" {
		t.Errorf("Preset() = %q, want %q", got, "Strong")
	}
}

func TestPresetCustom(t *testing.T) {
	cfg := Config{DefaultPreset: "PIN"}
	if got := cfg.Preset(); got != "PIN" {
		t.Errorf("Preset() = %q, want %q", got, "PIN")
	}
}

func isolateConfig(t *testing.T) string {
	t.Helper()
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)
	t.Setenv("HOME", filepath.Join(tmp, "home"))
	p := filepath.Join(tmp, "keetui", "config.json")
	if err := os.MkdirAll(filepath.Dir(p), 0700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	return p
}

func TestLoadTypeErrorReturnsCleanDefaults(t *testing.T) {
	p := isolateConfig(t)
	// clipboard_clear_seconds decodes before the type error on theme; the
	// partial value (below the 15s minimum) must not leak through.
	if err := os.WriteFile(p, []byte(`{"clipboard_clear_seconds": 3, "theme": 5}`), 0600); err != nil {
		t.Fatalf("write: %v", err)
	}
	cfg, err := Load()
	if err == nil {
		t.Fatal("expected error for type-invalid config")
	}
	if cfg.ClipboardClearSeconds != Default().ClipboardClearSeconds {
		t.Errorf("clipboard seconds = %d, want default %d (no partial decode)",
			cfg.ClipboardClearSeconds, Default().ClipboardClearSeconds)
	}
}

func TestLoadReadErrorReported(t *testing.T) {
	p := isolateConfig(t)
	// A directory at the config path makes ReadFile fail with a non-
	// not-exist error, which must be surfaced instead of swallowed.
	if err := os.Mkdir(p, 0700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if _, err := Load(); err == nil {
		t.Error("expected error when the config file is unreadable")
	}
}

func TestLoadConflictingKeybindingsFallBack(t *testing.T) {
	p := isolateConfig(t)
	if err := os.WriteFile(p, []byte(`{"keybindings": {"restore": "d"}}`), 0600); err != nil {
		t.Fatalf("write: %v", err)
	}
	cfg, err := Load()
	if err == nil {
		t.Fatal("expected validation error: restore bound to the delete key")
	}
	if cfg.KeyBindingsPtr != nil {
		t.Error("conflicting keybindings must fall back to defaults")
	}
}

func TestConfigPathHonorsXDG(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)
	t.Setenv("HOME", filepath.Join(tmp, "home"))

	want := filepath.Join(tmp, "keetui", "config.json")
	if got := configPath(); got != want {
		t.Errorf("configPath() = %q, want %q (XDG_CONFIG_HOME must be honored)", got, want)
	}
}

func TestConfigPathDefaultsToDotConfig(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("HOME", tmp)

	want := filepath.Join(tmp, ".config", "keetui", "config.json")
	if got := configPath(); got != want {
		t.Errorf("configPath() = %q, want %q", got, want)
	}
}

func TestConfigPathLegacyFallback(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("HOME", tmp)

	legacyDir, err := os.UserConfigDir()
	if err != nil {
		t.Skipf("UserConfigDir: %v", err)
	}
	legacy := filepath.Join(legacyDir, "keetui", "config.json")
	documented := filepath.Join(tmp, ".config", "keetui", "config.json")
	if legacy == documented {
		t.Skip("platform has no distinct legacy config location")
	}

	// A config written by an older version at the platform-native location
	// keeps being used...
	if err := os.MkdirAll(filepath.Dir(legacy), 0700); err != nil {
		t.Fatalf("mkdir legacy: %v", err)
	}
	if err := os.WriteFile(legacy, []byte("{}"), 0600); err != nil {
		t.Fatalf("write legacy: %v", err)
	}
	if got := configPath(); got != legacy {
		t.Errorf("configPath() = %q, want legacy %q when only it exists", got, legacy)
	}

	// ...but the documented path wins once a file exists there.
	if err := os.MkdirAll(filepath.Dir(documented), 0700); err != nil {
		t.Fatalf("mkdir documented: %v", err)
	}
	if err := os.WriteFile(documented, []byte("{}"), 0600); err != nil {
		t.Fatalf("write documented: %v", err)
	}
	if got := configPath(); got != documented {
		t.Errorf("configPath() = %q, want documented %q when both exist", got, documented)
	}
}

func TestOSC52Mode(t *testing.T) {
	for _, tc := range []struct{ stored, mode, sanitized string }{
		{"", OSC52Auto, ""},
		{"auto", OSC52Auto, "auto"},
		{"always", OSC52Always, "always"},
		{"never", OSC52Never, "never"},
		{"Always", OSC52Auto, ""},
		{"yes", OSC52Auto, ""},
	} {
		c := Default()
		c.ClipboardOSC52 = tc.stored
		if got := c.OSC52Mode(); got != tc.mode {
			t.Errorf("OSC52Mode(%q) = %q, want %q", tc.stored, got, tc.mode)
		}
		c.Sanitize()
		if c.ClipboardOSC52 != tc.sanitized {
			t.Errorf("Sanitize(%q) = %q, want %q", tc.stored, c.ClipboardOSC52, tc.sanitized)
		}
	}
}

func TestSaveReplacesTheConfigWhole(t *testing.T) {
	path := isolateConfig(t)
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`{"auto_lock_minutes": 10}`), 0644); err != nil {
		t.Fatal(err)
	}
	cfg := Default()
	cfg.AutoLockMinutes = 30
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}
	loaded, err := Load()
	if err != nil || loaded.AutoLockMinutes != 30 {
		t.Fatalf("saved config reads back as %+v, %v", loaded, err)
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0600 {
		t.Errorf("config mode = %v (%v), want 0600", info.Mode().Perm(), err)
	}
	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil || len(entries) != 1 {
		t.Errorf("config directory holds %d files, want only config.json (no temporary files): %v", len(entries), entries)
	}
}

func TestSaveKeepsASymlinkedConfig(t *testing.T) {
	path := isolateConfig(t)
	dotfiles := filepath.Join(t.TempDir(), "keetui.json")
	if err := os.WriteFile(dotfiles, []byte("{}"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(dotfiles, path); err != nil {
		t.Fatal(err)
	}
	cfg := Default()
	cfg.PasswordLength = 32
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}
	if info, err := os.Lstat(path); err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("the symlink was replaced by a file (%v)", err)
	}
	if data, err := os.ReadFile(dotfiles); err != nil || !strings.Contains(string(data), `"password_length": 32`) {
		t.Errorf("the linked file was not updated: %s, %v", data, err)
	}
}

func TestFailedSaveKeepsTheOldConfig(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root writes into read-only directories")
	}
	path := isolateConfig(t)
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	old := []byte(`{"auto_lock_minutes": 10}`)
	if err := os.WriteFile(path, old, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0700) })

	if err := Default().Save(); err == nil {
		t.Fatal("saving into a read-only directory reported success")
	}
	if data, err := os.ReadFile(path); err != nil || string(data) != string(old) {
		t.Errorf("a failed save changed the config: %q, %v", data, err)
	}
}

func TestIsColor(t *testing.T) {
	for value, want := range map[string]bool{
		"": true, "#89B4FA": true, "#fff": true, "0": true, "15": true, "255": true,
		"256": false, "-1": false, "05": false, "#12345": false, "#GGGGGG": false, "blue": false,
	} {
		if got := IsColor(value); got != want {
			t.Errorf("IsColor(%q) = %v, want %v", value, got, want)
		}
	}
}

func TestColorsAreValidatedOnLoadAndSurviveSaving(t *testing.T) {
	path := isolateConfig(t)
	data := `{"colors": {
		"dark": {"primary": "#89B4FA", "label": "245", "text": "", "muted": "grey", "sparkle": "#FFF"},
		"light": {"warn": "#12345"}
	}}`
	if err := os.WriteFile(path, []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load()
	if err == nil || !strings.Contains(err.Error(), `dark.muted: "grey" is not a color`) || !strings.Contains(err.Error(), "dark.sparkle: unknown role") {
		t.Errorf("Load error = %v, want the dropped colors named", err)
	}
	want := map[string]map[string]string{"dark": {"primary": "#89B4FA", "label": "245", "text": ""}}
	if !reflect.DeepEqual(cfg.Colors, want) {
		t.Fatalf("colors = %v, want %v", cfg.Colors, want)
	}

	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}
	again, err := Load()
	if err != nil || !reflect.DeepEqual(again.Colors, want) {
		t.Errorf("after saving: colors = %v, err %v", again.Colors, err)
	}
}
