package config

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestOverridesPreserveGlobalPolicyAndExplicitFalse(t *testing.T) {
	global := Default()
	yes := true
	global.PrivacyMode = &yes
	global.ThemeName = "terminal"
	global.Colors = map[string]map[string]string{"terminal": {"primary": "4"}}
	global.KeyBindingsPtr = &KeyBindings{Quit: "X"}
	before := global.String()
	o, err := ParseOverrides(`{
		"clipboard_clear_seconds":0,"auto_lock_minutes":0,
		"clipboard_osc52":"never","password_length":40,
		"pin_length":12,"passphrase_words":10,"passphrase_separator":"·",
		"default_preset":"passphrase","confirm_quit":false,"backup_on_save":false,
		"privacy_mode":false,"theme":"light","keybindings":{"quit":"z"},
		"colors":{"terminal":{"primary":"1"}}
	}`)
	if err != nil {
		t.Fatal(err)
	}
	effective := global.Apply(o)
	if effective.AutoLockDuration() != 0 || effective.ClipboardClearDuration() != 0 || effective.ConfirmOnQuit() || effective.BackupEnabled() {
		t.Fatal("explicit zero or false override was treated as inheritance")
	}
	if effective.PasswordLen() != 40 || effective.PINLen() != 12 || effective.PassphraseWordCount() != 10 || effective.PassphraseSep() != "·" || effective.Preset() != "Passphrase" || effective.OSC52Mode() != OSC52Never {
		t.Fatal("generator or clipboard overrides were lost")
	}
	if !effective.HidePasswords() || effective.Theme() != "terminal" || effective.Keys().Quit != "X" || !reflect.DeepEqual(effective.Colors, global.Colors) {
		t.Fatal("database metadata overrode a global-only policy")
	}
	if global.String() != before {
		t.Fatal("applying overrides mutated the global settings")
	}
	encoded := o.Encode()
	var fields map[string]json.RawMessage
	if err := json.Unmarshal([]byte(encoded), &fields); err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"privacy_mode", "theme", "keybindings", "colors"} {
		if _, found := fields[field]; found {
			t.Fatalf("database settings retained global-only %s", field)
		}
	}
}

func TestOverridesCharacterSetsCoverEveryCombination(t *testing.T) {
	global := Default()
	before := global.String()
	for mask := 0; mask < 16; mask++ {
		upper, lower, digits, symbols := mask&1 != 0, mask&2 != 0, mask&4 != 0, mask&8 != 0
		effective := global.Apply(Overrides{PasswordUppercase: &upper, PasswordLowercase: &lower, PasswordDigits: &digits, PasswordSymbols: &symbols})
		if mask == 0 {
			if effective.PasswordCharset() != global.PasswordCharset() {
				t.Fatal("an empty override disabled password generation")
			}
			continue
		}
		if effective.PasswordHasUppercase() != upper || effective.PasswordHasLowercase() != lower || effective.PasswordHasDigits() != digits || effective.PasswordHasSymbols() != symbols {
			t.Fatalf("character-set combination %04b did not apply", mask)
		}
		// The effective flags are owned by the result, not aliases into the
		// parsed database metadata or the reusable global configuration.
		*effective.PasswordUppercase = !upper
		if upper != (mask&1 != 0) {
			t.Fatal("the result aliases a database override")
		}
		if global.String() != before {
			t.Fatal("the result aliases the global configuration")
		}
	}
}

func FuzzDatabaseOverridesRoundTrip(f *testing.F) {
	for _, seed := range []string{"", "{}", "null", `{"auto_lock_minutes":0,"confirm_quit":false}`, `{"auto_lock_minutes":2,"password_length":4}`, `{"pin_length":16,"passphrase_words":12,"passphrase_separator":"·"}`, `{"default_preset":" strong "}`, `{"password_symbols":false}`, `{"auto_lock_minutes":"wrong"}`, `{"privacy_mode":false,"theme":"light","unknown_future_setting":42}`} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, data string) {
		if len(data) > 16384 {
			return
		}
		o, err := ParseOverrides(data)
		if err != nil {
			if !o.IsZero() {
				t.Fatal("invalid metadata left partially decoded overrides")
			}
			return
		}
		encoded := o.Encode()
		again, err := ParseOverrides(encoded)
		if err != nil || again.Encode() != encoded {
			t.Fatalf("normalized overrides are not stable: %q, %v", encoded, err)
		}
		base := Default()
		base.ThemeName = "terminal"
		before := base.String()
		applied := base.Apply(o)
		if base.String() != before || applied.Theme() != "terminal" || len(applied.PasswordCharset()) == 0 {
			t.Fatal("database metadata changed global policy or disabled generation")
		}
		if applied.PINLen() < MinPINLength || applied.PINLen() > MaxPINLength || applied.PassphraseWordCount() < MinPassphraseWords || applied.PassphraseWordCount() > MaxPassphraseWords || !ValidSeparator(applied.PassphraseSep()) {
			t.Fatal("normalized generator settings are out of bounds")
		}
	})
}
