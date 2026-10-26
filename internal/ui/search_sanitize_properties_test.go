package ui

import (
	"strings"
	"testing"
	"unicode"
	"unicode/utf8"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

func FuzzDisplaySanitization(f *testing.F) {
	for _, seed := range []string{"", "ordinary text", "a\r\n\tb", "\x1b]52;c;c3ludGhldGlj\x07", "\x00\xff\xfe\x9b", "東京 café", "a\u202eb\u2066c", "👩‍💻"} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, input string) {
		if len(input) > 4096 {
			return
		}
		displayed := SanitizeDisplay(input)
		if !utf8.ValidString(displayed) || ansi.Strip(displayed) != displayed {
			t.Fatal("displayed data contains invalid text or terminal commands")
		}
		if strings.ContainsFunc(displayed, unicode.IsControl) {
			t.Fatal("displayed data contains control characters")
		}
		if SanitizeDisplay(displayed) != displayed {
			t.Fatal("display sanitization is not idempotent")
		}
	})
}

func FuzzSearchMatchingAndHighlighting(f *testing.F) {
	for _, seed := range [][2]string{{"", ""}, {"testing", "test"}, {"t-e-s-t", "test"}, {"CAFÉ 東京", "fé 東"}, {"a\u0301bc", "a"}, {"👩‍💻 test", "test"}, {"İx", "i"}, {"\xff\x1babc", "a"}} {
		f.Add(seed[0], seed[1], false)
		f.Add(seed[0], seed[1], true)
	}
	f.Fuzz(func(t *testing.T, rawText, rawQuery string, fuzzy bool) {
		if len(rawText) > 256 || len(rawQuery) > 128 {
			return
		}
		text, query := strings.ToValidUTF8(rawText, "�"), strings.ToValidUTF8(rawQuery, "�")
		positions := matchPositions(text, query, fuzzy)
		lowerText, lowerQuery := []rune(strings.ToLower(text)), []rune(strings.ToLower(query))
		if query != "" && !fuzzy && (len(positions) > 0) != strings.Contains(strings.ToLower(text), strings.ToLower(query)) {
			t.Fatal("literal search disagrees with substring matching")
		}
		if len(positions) > 0 {
			if len(positions) != len(lowerQuery) {
				t.Fatal("match covers only part of the query")
			}
			for i, position := range positions {
				if position < 0 || position >= len(lowerText) || i > 0 && position <= positions[i-1] || lowerText[position] != lowerQuery[i] {
					t.Fatal("match indices are invalid or point at unrelated characters")
				}
				if !fuzzy && i > 0 && position != positions[i-1]+1 {
					t.Fatal("literal search matched separated letters")
				}
			}
		}
		displayed := SanitizeDisplay(text)
		highlighted := highlightSearch(displayed, SanitizeDisplay(query), lipgloss.NewStyle(), fuzzy)
		if ansi.Strip(highlighted) != displayed {
			t.Fatal("highlighting changed the displayed value")
		}
	})
}
