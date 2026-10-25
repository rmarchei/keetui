package ui

import (
	"image/color"
	"path/filepath"
	"strings"

	"charm.land/bubbles/v2/textinput"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// Unsized standalone models use a useful default until the first resize.
func viewSize(width, height int) (int, int) {
	if width <= 0 {
		width = 80
	}
	if height <= 0 {
		height = 24
	}
	return width, height
}

// fitFrame is the final safety boundary, not a substitute for scrolling.
// It measures terminal cells, preserves ANSI styles, and never emits a final
// newline (which would consume a row reserved for the footer/status bar).
func fitFrame(content string, width, height int) string {
	if width <= 0 || height <= 0 {
		return ""
	}
	lines := strings.Split(strings.TrimRight(content, "\n"), "\n")
	lines = lines[:min(len(lines), height)]
	for i, line := range lines {
		lines[i] = ansi.Truncate(line, width, "…")
	}
	for len(lines) < height {
		lines = append(lines, "")
	}
	return strings.Join(lines, "\n")
}

// renderHints fits whole "key: action" pairs on one row, drawn as the key in
// the accent color and the action dimmed. Callers put essentials (including
// the help shortcut, which lists the rest) first; pairs that do not fit are
// left out, never cut in half.
func renderHints(width int, hints ...string) string {
	width, _ = viewSize(width, 1)
	budget := width - 1 // leading space, aligned with the status bar text
	var parts []string
	used := 0
	seen := make(map[string]bool)
	for _, hint := range hints {
		hint = sanitizeDisplay(strings.TrimSpace(hint))
		if hint == "" || seen[hint] {
			continue
		}
		seen[hint] = true
		key, action, pair := strings.Cut(hint, ": ")
		rendered := helpStyle.Render(hint)
		if pair {
			rendered = hintKeyStyle.Render(key) + " " + helpStyle.Render(action)
		}
		need := lipgloss.Width(rendered)
		if used > 0 {
			need += 2
		}
		if used+need > budget {
			continue
		}
		used += need
		parts = append(parts, rendered)
	}
	return " " + strings.Join(parts, "  ")
}

// onBackground re-applies style's colors after every reset in s. Styled
// segments end with a full reset, which would otherwise leave them, and the
// text after them, on the terminal's own background.
func onBackground(s string, style lipgloss.Style) string {
	seq := ansi.Style{}
	if fg := style.GetForeground(); !isNoColor(fg) {
		seq = seq.ForegroundColor(fg)
	}
	if bg := style.GetBackground(); !isNoColor(bg) {
		seq = seq.BackgroundColor(bg)
	}
	return strings.ReplaceAll(s, ansi.ResetStyle, ansi.ResetStyle+seq.String())
}

func isNoColor(c color.Color) bool {
	_, none := c.(lipgloss.NoColor)
	return c == nil || none
}

// selectedRow draws a list row as the selection: a bar across the width
// behind the row, which keeps its layout, so the selection never shifts it.
func selectedRow(row string, width int) string {
	width, _ = viewSize(width, 1)
	lines := strings.Split(row, "\n")
	for i, line := range lines {
		line = ansi.Truncate(line, width, "…")
		lines[i] = selectedRowStyle.Width(width).Render(onBackground(line, selectedRowStyle))
	}
	return strings.Join(lines, "\n")
}

func wrapContent(content string, width int) string {
	return ansi.Wrap(content, max(width, 1), "")
}

// wrapIndented wraps plain text to width with every line, continuations
// included, indented by indent cells.
func wrapIndented(text string, width, indent int) string {
	pad := strings.Repeat(" ", indent)
	lines := strings.Split(ansi.Wrap(text, max(width-indent, 1), ""), "\n")
	return pad + strings.Join(lines, "\n"+pad)
}

func bodyHeight(height int, footer string) int {
	return max(0, height-lipgloss.Height(footer))
}

func screen(body, footer string, width, height, offset int) string {
	width, height = viewSize(width, height)
	footerHeight := min(height, lipgloss.Height(footer))
	available := height - footerHeight
	if available == 0 {
		return fitFrame(footer, width, footerHeight)
	}
	return contentWindow(body, width, available, offset) + "\n" + fitFrame(footer, width, footerHeight)
}

func contentWindow(body string, width, height, offset int) string {
	lines := strings.Split(strings.TrimRight(body, "\n"), "\n")
	offset = clampOffset(body, height, offset)
	return fitFrame(strings.Join(lines[offset:], "\n"), width, height)
}

// Match the rendered window's treatment of trailing newlines everywhere.
func clampOffset(body string, height, offset int) int {
	return min(max(offset, 0), scrollLimit(body, height))
}

func scrollLimit(body string, height int) int {
	lines := strings.Count(strings.TrimRight(body, "\n"), "\n") + 1
	return max(0, lines-max(1, height))
}

func followList(cursor, offset, count, height int) (int, int) {
	cursor = min(max(cursor, 0), max(0, count-1))
	offset = followField(offset, fieldRows{cursor, cursor}, height)
	return cursor, min(offset, max(0, count-max(1, height)))
}

type fieldRows struct{ start, end int }

// lineBuilder tracks rows incrementally, avoiding rescans of a growing form.
type lineBuilder struct {
	builder strings.Builder
	row     int
}

func (b *lineBuilder) String() string {
	return b.builder.String()
}

func (b *lineBuilder) WriteString(s string) {
	b.row += strings.Count(s, "\n")
	b.builder.WriteString(s)
}

func followField(offset int, field fieldRows, height int) int {
	height = max(1, height)
	if field.start < offset {
		offset = field.start
	} else if field.end >= offset+height {
		offset = min(field.start, field.end-height+1)
	}
	return max(0, offset)
}

func sizeInput(input *textinput.Model, width int) {
	// Include the rendered prompt, padding, and virtual cursor in the budget.
	promptWidth := lipgloss.Width(input.Styles().Focused.Prompt.Render(input.Prompt))
	input.SetWidth(max(1, width-promptWidth-1))
	input.SetCursor(input.Position()) // recompute horizontal scrolling on resize
}

// pathLabel retains the basename, shortening the directory first. Only the
// displayed path changes; file operations and editable inputs keep raw values.
func pathLabel(path string, width int) string {
	if width <= 0 {
		return ""
	}
	base := sanitizeDisplay(filepath.Base(path))
	dir := sanitizeDisplay(strings.TrimSuffix(path, filepath.Base(path)))
	if lipgloss.Width(base) >= width {
		return valueStyle.Bold(true).Render(ansi.Truncate(base, width, "…"))
	}
	remaining := width - lipgloss.Width(base)
	if lipgloss.Width(dir) > remaining {
		dir = "…" + ansi.Cut(dir, lipgloss.Width(dir)-remaining+1, lipgloss.Width(dir))
	}
	return hiddenStyle.Render(dir) + valueStyle.Bold(true).Render(base)
}
