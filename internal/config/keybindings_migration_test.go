package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestLoadRepairsOnlyConflictingOverrides(t *testing.T) {
	for _, tc := range []struct {
		name        string
		input, want KeyBindings
		warning     bool
	}{
		{"unrelated override", KeyBindings{Restore: "d", Delete: "d", ShowStats: "Z"}, KeyBindings{ShowStats: "Z"}, true},
		{"reserved key", KeyBindings{Edit: "j", Delete: "#"}, KeyBindings{Delete: "#"}, true},
		{"valid swap", KeyBindings{Delete: "n", NewEntry: "d"}, KeyBindings{Delete: "n", NewEntry: "d"}, false},
		{"cascading reset", KeyBindings{Delete: "n", NewEntry: "f1", ShowStats: "Z"}, KeyBindings{ShowStats: "Z"}, true},
		{"legacy implicit HIBP", KeyBindings{CopyUsername: "b", Delete: "#"}, KeyBindings{CopyUsername: "b", Delete: "#", HIBPCheck: "5"}, true},
		{"legacy and reserved", KeyBindings{CopyUsername: "b", Edit: "j"}, KeyBindings{CopyUsername: "b", HIBPCheck: "5"}, true},
		{"explicit old HIBP", KeyBindings{HIBPCheck: "5", CopyUsername: "b"}, KeyBindings{HIBPCheck: "5", CopyUsername: "b"}, false},
		{"legacy unavailable", KeyBindings{CopyUsername: "b", CopyURL: "5", Delete: "#"}, KeyBindings{CopyURL: "5", Delete: "#"}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			t.Setenv("XDG_CONFIG_HOME", dir)
			path := filepath.Join(dir, "keetui", "config.json")
			if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
				t.Fatal(err)
			}
			cfg := Default()
			cfg.KeyBindingsPtr = &tc.input
			cfg.PasswordLength = 32
			data, err := json.Marshal(cfg)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, data, 0600); err != nil {
				t.Fatal(err)
			}
			loaded, err := Load()
			if (err != nil) != tc.warning {
				t.Fatalf("warning = %v, want %v", err, tc.warning)
			}
			if loaded.Keys() != tc.want.Merge(DefaultKeyBindings()) || loaded.PasswordLength != 32 {
				t.Fatalf("unrelated settings lost: %+v", loaded)
			}
			if err := loaded.Keys().Validate(); err != nil {
				t.Fatal(err)
			}
			after, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if string(after) != string(data) {
				t.Fatal("loading rewrote the config file")
			}
		})
	}
}

func TestKeyBindingsOverridesAreSparse(t *testing.T) {
	kb := DefaultKeyBindings()
	if kb.Overrides() != nil {
		t.Fatal("defaults serialized as overrides")
	}
	kb.Delete = "#"
	if got := kb.Overrides(); got == nil || *got != (KeyBindings{Delete: "#"}) {
		t.Fatalf("unexpected overrides: %+v", got)
	}
}

func TestRepairBindingsUsesHistoricalDefaultsGenerically(t *testing.T) {
	for _, tc := range []struct {
		name    string
		input   KeyBindings
		history []KeyBindings
		want    KeyBindings
	}{
		{"another action", KeyBindings{Delete: "i", ShowQR: "V"}, []KeyBindings{{ShowStats: "Z"}}, KeyBindings{Delete: "i", ShowStats: "Z", ShowQR: "V"}},
		{"multiple changes", KeyBindings{Delete: "i", CopyUsername: "b"}, []KeyBindings{{ShowStats: "Z", HIBPCheck: "5"}}, KeyBindings{Delete: "i", ShowStats: "Z", CopyUsername: "b", HIBPCheck: "5"}},
		{"reserved historical key", KeyBindings{Delete: "i"}, []KeyBindings{{ShowStats: "f1"}, {ShowStats: "Z"}}, KeyBindings{Delete: "i", ShowStats: "Z"}},
		{"occupied historical key", KeyBindings{Delete: "i", NewEntry: "Z"}, []KeyBindings{{ShowStats: "Z"}}, KeyBindings{NewEntry: "Z"}},
		{"explicit choice must not migrate", KeyBindings{Delete: "i", ShowStats: "i"}, []KeyBindings{{ShowStats: "Z"}}, KeyBindings{}},
		{"no history", KeyBindings{Delete: "i", ShowQR: "V"}, nil, KeyBindings{ShowQR: "V"}},
		{"later reset conflicts with restored key", KeyBindings{Delete: "i", NewEntry: "f1"}, []KeyBindings{{ShowStats: "n"}}, KeyBindings{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			actual, warning := repairBindingsWithHistory(tc.input, tc.history)
			if warning == nil {
				t.Fatal("repair should explain adjustments")
			}
			cfg := Default()
			cfg.KeyBindingsPtr = actual
			if got := cfg.Keys(); got != tc.want.Merge(DefaultKeyBindings()) {
				t.Fatalf("unexpected repair: %+v", got)
			}
			if err := cfg.Keys().Validate(); err != nil {
				t.Fatal(err)
			}
		})
	}
}
