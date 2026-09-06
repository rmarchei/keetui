package ui

import (
	"strings"
	"testing"
)

func TestSanitizeDisplay(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"empty", "", ""},
		{"plain ascii", "hello world!", "hello world!"},
		{"unicode preserved", "pässwörd 🔑 ño", "pässwörd 🔑 ño"},
		{"newline replaced", "a\nb", "a�b"},
		{"carriage return replaced", "a\rb", "a�b"},
		{"tab replaced", "a\tb", "a�b"},
		{"invalid utf8 byte", "VId\x86J\xc3(", "VId�J�("},
		{"c1 controls as raw bytes", "x\x85y\x8dz\x9b", "x�y�z�"},
		{"c1 controls as runes", "x\u0085y\u008dz\u009b", "x\uFFFDy\uFFFDz\uFFFD"},
		{"escape byte", "a\x1b[31mred", "a�[31mred"},
		{"del", "a\x7fb", "a�b"},
		{"bidi override", "a\u202eb\u2066c", "a�b�c"},
		{"zero width", "a\u200bb\u200fc", "a�b�c"},
		{"genuine replacement char kept", "a�b", "a�b"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := sanitizeDisplay(tt.in); got != tt.want {
				t.Errorf("sanitizeDisplay(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestSanitizeDisplayNeverEmitsControlBytes(t *testing.T) {
	// Every byte value as a one-byte string must come out clean.
	for b := 0; b < 256; b++ {
		got := sanitizeDisplay(string([]byte{byte(b)}))
		for _, r := range got {
			if r != replacementChar && unsafeDisplayRune(r) {
				t.Fatalf("byte 0x%02x: output %q still contains unsafe rune %U", b, got, r)
			}
		}
		if strings.ContainsAny(got, "\x00\x1b\r\n") {
			t.Fatalf("byte 0x%02x: output %q contains raw control byte", b, got)
		}
	}
}

func TestSanitizeNoteLines(t *testing.T) {
	got := sanitizeNoteLines("line1\r\nline2\x9bx\n\nlast")
	want := []string{"line1", "line2�x", "", "last"}
	if len(got) != len(want) {
		t.Fatalf("expected %d lines, got %d (%q)", len(want), len(got), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("line %d: got %q, want %q", i, got[i], want[i])
		}
	}
}
