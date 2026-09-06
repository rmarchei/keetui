package ui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/rmarchei/keetui/internal/config"
	"github.com/rmarchei/keetui/internal/keepass"
	"github.com/tobischo/gokeepasslib/v3"
)

func TestStrengthLabelsEstimatesAndLimits(t *testing.T) {
	for _, sample := range []struct{ password, want string }{
		{"PASS-e1-v2", "Good ~28 guess bits"},
		{strings.Repeat("a", 101), "first 100 chars"},
		{string([]byte{0xff}), "Not rated: invalid text"},
	} {
		if got := ansi.Strip(renderStrength(keepass.EvaluatePasswordStrength(sample.password))); !strings.Contains(got, sample.want) {
			t.Errorf("label = %q, want %q", got, sample.want)
		}
	}
}

func TestEditorRefreshesStrengthOnlyOnPasswordChange(t *testing.T) {
	m := NewEditorModel(&keepass.Entry{Password: "PASS-e1-v2"}, gokeepasslib.UUID{}, config.Default())
	if m.strength != keepass.EvaluatePasswordStrength("PASS-e1-v2") {
		t.Fatal("initial strength not evaluated")
	}
	m.SetGeneratedPassword("Xk9#mQ2$vL7@")
	if m.strength != keepass.EvaluatePasswordStrength("Xk9#mQ2$vL7@") {
		t.Fatal("generated password did not refresh strength")
	}
	m.focused = fieldPassword
	m.updateFocus()
	updated, _ := m.Update(tea.PasteMsg{Content: "extended"})
	m = updated.(EditorModel)
	if m.strength != keepass.EvaluatePasswordStrength(m.inputs[fieldPassword].Value()) {
		t.Fatal("pasted password did not refresh strength")
	}
	// A sentinel detects accidental work on resize, cursor blink, or rendering.
	m.strength.Label = "cached"
	updated, _ = m.Update(tea.WindowSizeMsg{Width: 80, Height: 23})
	m = updated.(EditorModel)
	m.View()
	if m.strength.Label != "cached" {
		t.Fatal("resize unnecessarily recalculated strength")
	}
}
