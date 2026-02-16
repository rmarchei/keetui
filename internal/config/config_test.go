package config

import (
	"encoding/json"
	"os"
	"path/filepath"
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
	if got := cfg.Theme(); got != "dark" {
		t.Errorf("Theme() = %q, want %q", got, "dark")
	}
}

func TestSanitizeClamps(t *testing.T) {
	tests := []struct {
		name     string
		cfg      Config
		wantClip int
		wantLock int
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
	if cfg.ConfirmOnQuit() {
		t.Error("ConfirmOnQuit() should be false by default")
	}
}

func TestConfirmOnQuitEnabled(t *testing.T) {
	tr := true
	cfg := Config{ConfirmQuit: &tr}
	if !cfg.ConfirmOnQuit() {
		t.Error("ConfirmOnQuit() should be true when set")
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

func contains(s, substr string) bool {
	return len(s) >= len(substr) && searchString(s, substr)
}

func searchString(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
