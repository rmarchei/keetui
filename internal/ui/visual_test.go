package ui

import (
	"regexp"
	"strconv"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/rmarchei/keetui/internal/config"
	"github.com/rmarchei/keetui/internal/keepass"
)

// assertOnBackground checks that every visible cell of s, including the text
// after a styled segment's reset, is drawn on style's background.
func assertOnBackground(t *testing.T, s string, style lipgloss.Style) {
	t.Helper()
	bg := strings.TrimSuffix(strings.TrimPrefix(
		ansi.Style{}.BackgroundColor(style.GetBackground()).String(), "\x1b["), "m")
	leading := regexp.MustCompile(`^(\x1b\[[0-9;:]*m)*`)
	for _, piece := range strings.Split(s, ansi.ResetStyle) {
		if ansi.Strip(piece) == "" {
			continue
		}
		if lead := leading.FindString(piece); !strings.Contains(lead, bg) {
			t.Errorf("%q is drawn without the background (%s)", ansi.Strip(piece), bg)
		}
	}
}

// column returns the cell at which text starts on the first line holding it.
func column(t *testing.T, view, text string) int {
	t.Helper()
	for _, line := range strings.Split(ansi.Strip(view), "\n") {
		if i := strings.Index(line, text); i >= 0 {
			return lipgloss.Width(line[:i])
		}
	}
	t.Fatalf("%q not shown in:\n%s", text, ansi.Strip(view))
	return -1
}

// rowWith returns the rendered line holding text.
func rowWith(t *testing.T, view, text string) string {
	t.Helper()
	for _, line := range strings.Split(view, "\n") {
		if strings.Contains(ansi.Strip(line), text) {
			return line
		}
	}
	t.Fatalf("%q not shown", text)
	return ""
}

func TestBrowserSelectionKeepsRowsAligned(t *testing.T) {
	db := createTestUIDB(t)
	if _, err := db.AddEntry(db.RootGroup().UUID, keepass.Entry{Title: "A much longer title", UserName: "someone"}); err != nil {
		t.Fatal(err)
	}
	m := NewBrowserModel(db)
	m.width, m.height = 100, 30
	for cursor, selected := range m.items {
		m.cursor = cursor
		view := m.View().Content
		want := column(t, view, m.items[0].title())
		for _, item := range m.items {
			if got := column(t, view, item.title()); got != want {
				t.Errorf("with %q selected, %q starts at %d, want %d", selected.title(), item.title(), got, want)
			}
		}
		// The usernames' column lines up too, and the selection bar spans the row.
		if got, want := column(t, view, "rootuser"), column(t, view, "someone"); got != want {
			t.Errorf("usernames at %d and %d", got, want)
		}
		bar := rowWith(t, view, selected.title())
		if lipgloss.Width(bar) != m.width {
			t.Errorf("selection bar is %d cells, want %d", lipgloss.Width(bar), m.width)
		}
		assertOnBackground(t, bar, selectedRowStyle)
	}
}

func TestDetailSelectionKeepsValuesAligned(t *testing.T) {
	m := NewDetailModel(testEntry(), config.Default())
	m.SetSize(100, 30)
	for field := range 3 {
		m.selectedField = field
		content := m.buildContent()
		if user, url := column(t, content, "testuser"), column(t, content, "https://example.com"); user != url {
			t.Errorf("field %d selected: username value at %d, URL value at %d", field, user, url)
		}
	}
}

func TestHintsFitOneRowWithStyledKeys(t *testing.T) {
	got := renderHints(80, "F1: help", "esc: back", "[username]")
	if lipgloss.Height(got) != 1 {
		t.Fatalf("hints take %d rows", lipgloss.Height(got))
	}
	for _, want := range []string{hintKeyStyle.Render("F1"), helpStyle.Render("help"), helpStyle.Render("[username]")} {
		if !strings.Contains(got, want) {
			t.Errorf("hints %q lack %q", got, want)
		}
	}
	if plain := ansi.Strip(got); plain != " F1 help  esc back  [username]" {
		t.Errorf("hints = %q", plain)
	}
}

func TestSettingsDescriptionsWrapIndented(t *testing.T) {
	m := NewSettingsModel(config.Default())
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 60, Height: 40})
	m = updated.(SettingsModel)
	body, _ := m.body()
	lines := strings.Split(ansi.Strip(body), "\n")
	start := -1
	for i, line := range lines {
		if strings.Contains(line, "Copy via OSC 52") {
			start = i
			break
		}
	}
	if start < 0 || lines[start+1] == "" {
		t.Fatalf("the terminal clipboard description does not wrap at 60 columns:\n%s", ansi.Strip(body))
	}
	for _, line := range lines[start:] {
		if line == "" {
			break
		}
		if !strings.HasPrefix(line, "  ") || strings.HasPrefix(line, "   ") {
			t.Errorf("description line %q is not indented by 2", line)
		}
	}
	// Toggles and choices share the text inputs' value column.
	if got, want := column(t, body, "[ auto ▾ ]"), column(t, body, "15"); got != want {
		t.Errorf("choice value at %d, input value at %d", got, want)
	}
}

func TestStrengthMeterFillsByScore(t *testing.T) {
	for score := keepass.StrengthWeak; score <= keepass.StrengthExcellent; score++ {
		got := ansi.Strip(renderStrength(keepass.StrengthResult{Score: score, Label: "Rated", GuessBits: 40, Length: 12, AnalyzedLength: 12}))
		if filled := int(score) + 1; strings.Count(got, "▰") != filled || strings.Count(got, "▱") != 5-filled {
			t.Errorf("score %d meter = %q, want %d of 5 filled", score, got, filled)
		}
		if !strings.HasSuffix(got, " Rated ~40 guess bits") {
			t.Errorf("score %d label = %q", score, got)
		}
	}
}

func TestTOTPCountdownDrainsAndWarns(t *testing.T) {
	for _, tc := range []struct {
		remaining, filled int
		style             lipgloss.Style
	}{
		{30, 12, successStyle.Padding(0)},
		{15, 6, successStyle.Padding(0)},
		{10, 4, warnStyle.Padding(0)},
		{3, 1, errorMsgStyle.Padding(0)},
		{0, 0, errorMsgStyle.Padding(0)},
	} {
		got := totpCountdown(tc.remaining, 30)
		plain := ansi.Strip(got)
		if strings.Count(plain, "━") != tc.filled || strings.Count(plain, "─") != 12-tc.filled {
			t.Errorf("%ds: bar %q, want %d of 12 filled", tc.remaining, plain, tc.filled)
		}
		if seconds := tc.style.Render(strconv.Itoa(tc.remaining) + "s"); !strings.HasSuffix(got, seconds) {
			t.Errorf("%ds: countdown %q, want it to end with %q", tc.remaining, got, seconds)
		}
	}
}

func TestGroupDigits(t *testing.T) {
	for code, want := range map[string]string{"026242": "026 242", "12345678": "1234 5678", "12345": "12345", "": ""} {
		if got := groupDigits(code); got != want {
			t.Errorf("groupDigits(%q) = %q, want %q", code, got, want)
		}
	}
}

func TestURLHost(t *testing.T) {
	for raw, want := range map[string]string{
		"https://www.github.com/login": "github.com",
		"https://gitea.example.com":    "gitea.example.com",
		"ssh://git@host:22":            "host",
		"not a url":                    "not a url",
		"":                             "",
	} {
		if got := urlHost(raw); got != want {
			t.Errorf("urlHost(%q) = %q, want %q", raw, got, want)
		}
	}
}
