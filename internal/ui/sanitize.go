package ui

import (
	"strings"
	"unicode"
)

// replacementChar substitutes characters that would corrupt the terminal or
// visually reorder text when rendered.
const replacementChar = '�'

// sanitizeDisplay returns s safe for terminal rendering: invalid UTF-8
// sequences, control characters (C0, DEL, C1) and Unicode bidi/zero-width
// formatting characters are replaced with U+FFFD.
//
// Database values can contain arbitrary bytes (mis-encoded imports, corrupted
// protected values, hostile data). Rendering them raw lets the terminal
// interpret control bytes as cursor movements, which desyncs Bubble Tea's
// line-based layout and corrupts the whole frame.
//
// Sanitization is display-only — clipboard copies and QR codes always use the
// raw value.
func sanitizeDisplay(s string) string {
	if s == "" {
		return s
	}
	s = strings.ToValidUTF8(s, string(replacementChar))
	return strings.Map(func(r rune) rune {
		if unsafeDisplayRune(r) {
			return replacementChar
		}
		return r
	}, s)
}

// sanitizeNoteLines splits multi-line text into lines and sanitizes each,
// preserving intentional line breaks while neutralizing everything else.
func sanitizeNoteLines(s string) []string {
	lines := strings.Split(s, "\n")
	for i, line := range lines {
		lines[i] = sanitizeDisplay(strings.TrimSuffix(line, "\r"))
	}
	return lines
}

// unsafeDisplayRune reports whether r must not reach the terminal raw.
func unsafeDisplayRune(r rune) bool {
	if unicode.IsControl(r) { // C0, DEL and C1 ranges
		return true
	}
	switch {
	case r >= 0x200B && r <= 0x200F: // zero-width space/joiners, LRM/RLM
		return true
	case r >= 0x202A && r <= 0x202E: // bidi embedding/override
		return true
	case r >= 0x2066 && r <= 0x2069: // bidi isolates
		return true
	}
	return false
}
