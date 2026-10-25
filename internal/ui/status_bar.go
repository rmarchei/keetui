package ui

import (
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

func (m AppModel) statusBar() string {
	width, _ := viewSize(m.width, m.height)
	budget := max(0, width-2) // background style's horizontal padding
	security := ansi.Truncate(m.securityStatus(budget), budget, "…")
	summaryWidth := budget
	if security != "" {
		summaryWidth = max(0, budget-lipgloss.Width(security)-2)
	}
	summary := m.statusSummary(summaryWidth)
	content := summary
	if security != "" {
		content += strings.Repeat(" ", max(0, budget-lipgloss.Width(summary)-lipgloss.Width(security))) + security
	}
	return fitFrame(statusBarBgStyle.Width(width).Render(onBackground(content, statusBarBgStyle)), width, 1)
}

// Security indicators have reserved space. Fit whole context items in what
// remains, shortening metadata before sacrificing copy/error text and undo.
func (m AppModel) statusSummary(width int) string {
	if width <= 0 {
		return ""
	}
	var flags, compactFlags []string
	if m.readOnly {
		flags = append(flags, statusBarModifiedStyle.Render("[read-only]"))
		compactFlags = append(compactFlags, statusBarModifiedStyle.Render("RO"))
	}
	if m.db != nil && m.db.SuspectCorruption() {
		flags = append(flags, errorMsgStyle.Padding(0).Render("[corrupted?]"))
		compactFlags = append(compactFlags, errorMsgStyle.Padding(0).Render("corrupt?"))
	}
	if m.dirty {
		flags = append(flags, statusBarModifiedStyle.Render("[modified]"))
		compactFlags = append(compactFlags, statusBarModifiedStyle.Render("*"))
	}
	flagText, compactFlagText := strings.Join(flags, " "), strings.Join(compactFlags, " ")
	undo, compactUndo := "", ""
	if m.undoStack.length() > 0 {
		undo = helpStyle.Render(fmt.Sprintf("ctrl+z: undo (%d)", m.undoStack.length()))
		compactUndo = helpStyle.Render("ctrl+z: undo")
	}
	filename := sanitizeDisplay(filepath.Base(m.filePath))
	shortFilename := ansi.Truncate(filename, max(1, width/4), "…")
	count := fmt.Sprintf("%d entries", m.entryCount)
	join := func(parts ...string) string {
		var present []string
		for _, part := range parts {
			if part != "" {
				present = append(present, part)
			}
		}
		return strings.Join(present, "  ")
	}
	if m.statusText != "" {
		message := sanitizeDisplay(m.statusText)
		prefixes := []string{
			join(flagText, undo, shortFilename, count, "F1: details"),
			join(flagText, compactUndo, count, "F1"),
			join(compactFlagText, compactUndo, "F1"),
			join(compactFlagText, compactUndo),
		}
		prefix := ""
		// Preserve the most compact context even when an exceptionally narrow
		// terminal leaves room for only a tiny message excerpt.
		for _, candidate := range prefixes {
			if lipgloss.Width(candidate) <= width {
				prefix = candidate
			}
		}
		// A message that fits whole beside some context that keeps the F1
		// hint gets the widest such context: a remedy at its end must not be
		// cut off for the file name. A longer one keeps the context and at
		// least 16 cells.
		msgWidth := lipgloss.Width(message)
		if i := slices.IndexFunc(prefixes[:3], func(c string) bool { return lipgloss.Width(c)+2+msgWidth <= width }); i >= 0 {
			prefix = prefixes[i]
		} else {
			for _, candidate := range prefixes {
				if lipgloss.Width(candidate)+2+min(16, msgWidth) <= width {
					prefix = candidate
					break
				}
			}
		}
		remaining := max(0, width-lipgloss.Width(prefix)-2)
		style := successStyle.Padding(0)
		if m.statusError {
			style = errorMsgStyle.Padding(0)
		}
		return join(prefix, style.Render(ansi.Truncate(message, remaining, "…")))
	}
	candidates := []string{
		join(flagText, undo, filename, count),
		join(flagText, compactUndo, shortFilename, count),
		join(compactFlagText, compactUndo, shortFilename),
		join(compactFlagText, compactUndo),
	}
	for _, candidate := range candidates {
		if lipgloss.Width(candidate) <= width {
			return candidate
		}
	}
	return ansi.Truncate(join(compactFlagText, compactUndo), width, "…")
}
