package ui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/rmarchei/keetui/internal/keepass"
)

func TestStatsBack(t *testing.T) {
	m := NewStatsModel(keepass.DBStats{})
	m.width, m.height = 80, 24

	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if cmd == nil {
		t.Fatal("esc should produce a command")
	}
	msg := cmd()
	if _, ok := msg.(BackToBrowserMsg); !ok {
		t.Errorf("expected BackToBrowserMsg, got %T", msg)
	}
}

func TestStatsQuit(t *testing.T) {
	m := NewStatsModel(keepass.DBStats{})
	m.width, m.height = 80, 24

	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
	if cmd == nil {
		t.Fatal("q should produce a command")
	}
	msg := cmd()
	if _, ok := msg.(RequestQuitMsg); !ok {
		t.Errorf("expected RequestQuitMsg, got %T", msg)
	}
}

func TestStatsView(t *testing.T) {
	stats := keepass.DBStats{
		FilePath:     "test.kdbx",
		FileSize:     1024,
		TotalGroups:  3,
		TotalEntries: 10,
	}
	m := NewStatsModel(stats)
	m.width, m.height = 80, 24

	view := m.View()
	if !strings.Contains(view, "Database Info") {
		t.Error("View() should contain 'Database Info'")
	}
	if !strings.Contains(view, "test.kdbx") {
		t.Error("View() should contain file path")
	}
}

func TestFormatSize(t *testing.T) {
	tests := []struct {
		name  string
		bytes int64
		want  string
	}{
		{"bytes", 500, "500 B"},
		{"kilobytes", 1536, "1.5 KB"},
		{"megabytes", 1048576, "1.0 MB"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatSize(tt.bytes)
			if got != tt.want {
				t.Errorf("formatSize(%d) = %q, want %q", tt.bytes, got, tt.want)
			}
		})
	}
}
