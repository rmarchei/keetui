package ui

import (
	"testing"

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
	if len(names) != 3 {
		t.Errorf("ThemeNames() returned %d names, want 3", len(names))
	}
	expected := map[string]bool{"dark": true, "light": true, "high-contrast": true}
	for _, n := range names {
		if !expected[n] {
			t.Errorf("unexpected theme name: %q", n)
		}
	}
}
