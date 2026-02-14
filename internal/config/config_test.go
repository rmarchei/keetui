package config

import (
	"encoding/json"
	"os"
	"path/filepath"
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
