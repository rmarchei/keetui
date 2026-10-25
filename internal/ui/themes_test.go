package ui

import (
	"image/color"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

func TestGetThemeDefaults(t *testing.T) {
	dark := GetTheme("dark")
	if dark.Primary != "#7D56F4" {
		t.Errorf("dark Primary = %q, want %q", dark.Primary, "#7D56F4")
	}
	if dark.Text != "#FAFAFA" {
		t.Errorf("dark Text = %q, want %q", dark.Text, "#FAFAFA")
	}
}

func TestGetThemeFallback(t *testing.T) {
	unknown := GetTheme("nonexistent")
	dark := GetTheme("dark")
	if unknown.Primary != dark.Primary {
		t.Errorf("unknown theme should fall back to dark, got Primary = %q", unknown.Primary)
	}
}

func TestGetThemeLight(t *testing.T) {
	light := GetTheme("light")
	if light.Primary != "#5B21B6" {
		t.Errorf("light Primary = %q, want %q", light.Primary, "#5B21B6")
	}
}

func TestGetThemeHighContrast(t *testing.T) {
	hc := GetTheme("high-contrast")
	if hc.Primary != "#FFFF00" {
		t.Errorf("high-contrast Primary = %q, want %q", hc.Primary, "#FFFF00")
	}
}

func TestApplyTheme(t *testing.T) {
	// Apply light theme
	ApplyTheme(GetTheme("light"))
	if primaryColor != lipgloss.Color("#5B21B6") {
		t.Errorf("after light theme, primaryColor = %v, want #5B21B6", primaryColor)
	}

	// Apply dark theme to reset
	ApplyTheme(GetTheme("dark"))
	if primaryColor != lipgloss.Color("#7D56F4") {
		t.Errorf("after dark theme, primaryColor = %v, want #7D56F4", primaryColor)
	}
}

func TestThemeNames(t *testing.T) {
	names := ThemeNames()
	if len(names) != 4 {
		t.Errorf("ThemeNames() returned %d names, want 4", len(names))
	}
	expected := map[string]bool{"auto": true, "dark": true, "light": true, "high-contrast": true}
	for _, n := range names {
		if !expected[n] {
			t.Errorf("unexpected theme name: %q", n)
		}
	}
}

func TestAutoThemeFollowsTerminalBackground(t *testing.T) {
	t.Cleanup(func() {
		lightBackground = false
		ApplyTheme(GetTheme("dark"))
	})
	if got := GetTheme("auto"); got != GetTheme("dark") {
		t.Fatal("auto should be dark until the terminal answers")
	}

	m := createTestApp(t) // the default configuration: auto
	updated, _ := m.Update(tea.BackgroundColorMsg{Color: color.White})
	m = updated.(AppModel)
	if primaryColor != lipgloss.Color(GetTheme("light").Primary) {
		t.Errorf("light background: primaryColor = %v, want the light theme's", primaryColor)
	}
	updated, _ = m.Update(tea.BackgroundColorMsg{Color: color.Black})
	m = updated.(AppModel)
	if primaryColor != lipgloss.Color(GetTheme("dark").Primary) {
		t.Errorf("dark background: primaryColor = %v, want the dark theme's", primaryColor)
	}

	// A theme set by name ignores the terminal.
	m.cfg.ThemeName = "dark"
	m.Update(tea.BackgroundColorMsg{Color: color.White})
	if primaryColor != lipgloss.Color(GetTheme("dark").Primary) {
		t.Errorf("explicit dark on a light background: primaryColor = %v, want dark", primaryColor)
	}
}
