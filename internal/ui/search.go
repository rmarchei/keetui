package ui

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// Keep the search row in place after committing a query, including when
// returning from a result. Scrolling and mouse hit testing use the same offset.
func (m BrowserModel) listStartRow() int {
	if m.searching || m.searchQuery != "" {
		return browserHeaderRows + 2
	}
	return browserHeaderRows
}

func (m BrowserModel) searchView() string {
	width, _ := viewSize(m.width, m.height)
	count := fmt.Sprintf("%d results", len(m.items))
	if len(m.items) == 1 {
		count = "1 result"
	}
	if width < 32 {
		count = fmt.Sprintf("%d found", len(m.items))
	}
	count = ansi.Truncate(count, max(0, width-8), "…")
	leftWidth := max(1, width-lipgloss.Width(count)-2)
	left := focusedStyle.Render("  / ") + valueStyle.Render(sanitizeDisplay(m.searchInput.Value()))
	if m.searching {
		input := m.searchInput
		sizeInput(&input, leftWidth)
		setInputTextStyle(&input, valueStyle)
		left = input.View()
	}
	left = ansi.Truncate(left, leftWidth, "…")
	gap := strings.Repeat(" ", max(0, width-lipgloss.Width(left)-lipgloss.Width(count)-1))
	bar := statusBarBgStyle.Padding(0)
	return fitFrame(bar.Width(width).Render(onBackground(left+gap+secondaryStyle.Render(count)+" ", bar)), width, 1)
}

// matchPositions prefers a contiguous match, then (when allowed) the same
// subsequence match used by search. Indices are runes, not UTF-8 byte offsets.
func matchPositions(text, query string, fuzzy bool) []int {
	if query == "" {
		return nil
	}
	lower, query := strings.ToLower(text), strings.ToLower(query)
	pattern := []rune(query)
	if at := strings.Index(lower, query); at >= 0 {
		start := utf8.RuneCountInString(lower[:at])
		positions := make([]int, len(pattern))
		for i := range positions {
			positions[i] = start + i
		}
		return positions
	}
	if !fuzzy {
		return nil
	}
	var positions []int
	for i, r := range []rune(lower) {
		if r == pattern[len(positions)] {
			positions = append(positions, i)
			if len(positions) == len(pattern) {
				return positions
			}
		}
	}
	return nil
}

// Highlight complete grapheme clusters so accents and wide characters retain
// their terminal width. Underlining remains visible on the selected row and
// without color; the surrounding text keeps its usual hierarchy.
func highlightSearch(text, query string, style lipgloss.Style, fuzzy bool) string {
	positions := matchPositions(text, query, fuzzy)
	if len(positions) == 0 {
		return style.Render(text)
	}
	var b strings.Builder
	pos, runeIndex := 0, 0
	for text != "" {
		cluster, _ := ansi.FirstGraphemeCluster(text, ansi.GraphemeWidth)
		end := runeIndex + utf8.RuneCountInString(cluster)
		highlight := false
		for pos < len(positions) && positions[pos] < end {
			highlight = true
			pos++
		}
		segmentStyle := style
		if highlight {
			segmentStyle = style.Bold(true).Underline(true)
		}
		b.WriteString(segmentStyle.Render(cluster))
		text, runeIndex = text[len(cluster):], end
	}
	return b.String()
}

// Explain the highest-ranked matching field, using the same exact-before-fuzzy
// ordering as applySearch. Never include notes, custom values or protected
// field names in the explanation.
func searchMatchField(item listItem, query string) string {
	if query == "" {
		return ""
	}
	query = strings.ToLower(query)
	type field struct{ label, value string }
	fields := []field{{"title", item.name()}}
	if !item.isGroup {
		e := item.entry
		fields = append(fields, field{"username", e.UserName},
			field{"URL", e.URL},
			field{"notes", e.Notes})
		for _, cf := range e.CustomFields {
			label := "custom field"
			if cf.Protected {
				label = "protected field"
			}
			fields = append(fields, field{label, cf.Key},
				field{label, cf.Value})
		}
	}
	for _, fuzzy := range []bool{false, true} {
		if fuzzy && len(query) <= 1 {
			break
		}
		for _, field := range fields {
			matches := strings.Contains(strings.ToLower(field.value), query)
			if fuzzy {
				matches = fuzzyMatch(field.value, query)
			}
			if matches {
				return field.label
			}
		}
	}
	return ""
}
