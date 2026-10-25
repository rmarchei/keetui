package config

import (
	"encoding/json"
	"testing"
)

func TestDefaultKeyBindingsNonEmpty(t *testing.T) {
	kb := DefaultKeyBindings()

	if kb.NavUp == "" {
		t.Error("NavUp should not be empty")
	}
	if kb.NavDown == "" {
		t.Error("NavDown should not be empty")
	}
	if kb.Quit == "" {
		t.Error("Quit should not be empty")
	}
	if kb.Delete == "" {
		t.Error("Delete should not be empty")
	}
	if kb.TogglePassword == "" {
		t.Error("TogglePassword should not be empty")
	}
}

func TestMergeUsesDefaults(t *testing.T) {
	custom := KeyBindings{}
	defaults := DefaultKeyBindings()
	merged := custom.Merge(defaults)

	if merged.NavUp != defaults.NavUp {
		t.Errorf("NavUp = %q, want %q", merged.NavUp, defaults.NavUp)
	}
	if merged.Quit != defaults.Quit {
		t.Errorf("Quit = %q, want %q", merged.Quit, defaults.Quit)
	}
}

func TestMergePreservesCustom(t *testing.T) {
	custom := KeyBindings{
		NavUp:  "w",
		Delete: "D",
	}
	merged := custom.Merge(DefaultKeyBindings())

	if merged.NavUp != "w" {
		t.Errorf("NavUp = %q, want %q", merged.NavUp, "w")
	}
	if merged.Delete != "D" {
		t.Errorf("Delete = %q, want %q", merged.Delete, "D")
	}
	// Other fields should still be defaults
	if merged.NavDown != "j" {
		t.Errorf("NavDown = %q, want %q", merged.NavDown, "j")
	}
}

func TestValidateNoDuplicates(t *testing.T) {
	kb := DefaultKeyBindings()
	if err := kb.Validate(); err != nil {
		t.Errorf("default keybindings should not have duplicates: %v", err)
	}
}

func TestValidateSharedScreenBindings(t *testing.T) {
	for _, tc := range []struct {
		name      string
		overrides KeyBindings
		valid     bool
	}{
		{"history copy on former Back", KeyBindings{Back: "B", CopyPassword: "h"}, true},
		{"former navigation keys are free", KeyBindings{NavDown: "J", NavUp: "K", Edit: "j", CopyPassword: "k"}, true},
		{"former quit key is free", KeyBindings{Quit: "X", CopyPassword: "q"}, true},
		{"arrow aliases", KeyBindings{NavDown: "down", NavUp: "up"}, true},
		{"detail action shadows navigation", KeyBindings{NavDown: "e"}, false},
		{"attachment add shadows quit", KeyBindings{Quit: "+"}, false},
		{"attachment add shadows save", KeyBindings{Save: "+"}, false},
		{"help tabs shadow quit", KeyBindings{Quit: "tab"}, false},
		{"help paging shadows navigation", KeyBindings{NavUp: "pgup"}, false},
		{"stats paging shadows Back", KeyBindings{Back: "home"}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.overrides.Merge(DefaultKeyBindings()).Validate()
			if (err == nil) != tc.valid {
				t.Fatalf("Validate() = %v, want valid=%v", err, tc.valid)
			}
		})
	}
}

func TestValidateDetectsDuplicates(t *testing.T) {
	kb := DefaultKeyBindings()
	kb.NewEntry = "d" // same as Delete
	err := kb.Validate()
	if err == nil {
		t.Error("should detect duplicate binding")
	}
}

func TestConfigKeysDefaultWhenNil(t *testing.T) {
	cfg := Default()
	keys := cfg.Keys()

	if keys.NavUp != "k" {
		t.Errorf("NavUp = %q, want %q", keys.NavUp, "k")
	}
}

func TestConfigKeysWithCustom(t *testing.T) {
	cfg := Default()
	custom := &KeyBindings{NavUp: "w"}
	cfg.KeyBindingsPtr = custom

	keys := cfg.Keys()
	if keys.NavUp != "w" {
		t.Errorf("NavUp = %q, want %q", keys.NavUp, "w")
	}
	if keys.NavDown != "j" {
		t.Errorf("NavDown = %q, want %q (should use default)", keys.NavDown, "j")
	}
}

func TestConfigJSONRoundTrip(t *testing.T) {
	cfg := Default()
	custom := &KeyBindings{Delete: "D", ShowQR: "Z"}
	cfg.KeyBindingsPtr = custom

	data, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var loaded Config
	if err := json.Unmarshal(data, &loaded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	keys := loaded.Keys()
	if keys.Delete != "D" {
		t.Errorf("Delete = %q, want %q", keys.Delete, "D")
	}
	if keys.ShowQR != "Z" {
		t.Errorf("ShowQR = %q, want %q", keys.ShowQR, "Z")
	}
	if keys.NavUp != "k" {
		t.Errorf("NavUp = %q, want %q (should use default)", keys.NavUp, "k")
	}
}

func TestConfigBackwardCompatibility(t *testing.T) {
	// Old config without keybindings key
	data := []byte(`{"clipboard_clear_seconds":15,"auto_lock_minutes":5,"password_length":20,"show_password_strength":true}`)

	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	keys := cfg.Keys()
	if keys.NavUp != "k" {
		t.Errorf("NavUp = %q, want %q", keys.NavUp, "k")
	}
	if keys.Quit != "q" {
		t.Errorf("Quit = %q, want %q", keys.Quit, "q")
	}
}
