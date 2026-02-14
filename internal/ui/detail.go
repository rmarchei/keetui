package ui

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/pquerna/otp/totp"

	"github.com/rmarchei/keetui/internal/keepass"
)

// totpTickMsg is sent every second to refresh the TOTP code.
type totpTickMsg time.Time

// hibpResultMsg carries the result of a HIBP breach check.
type hibpResultMsg struct {
	Breached bool
	Count    int
	Err      error
}

// DetailModel shows a read-only view of a single entry.
type DetailModel struct {
	entry        keepass.Entry
	showPassword bool
	totpCode     string
	totpRemain   int // seconds remaining in current TOTP period
	width        int
	height       int
	copied       string
	strength     keepass.StrengthResult
	hibpChecking bool
	hibpResult   *hibpResultMsg
	attachments  []keepass.Attachment
}

// BackToBrowserMsg signals returning to the browser.
type BackToBrowserMsg struct{}

// CopyToClipboardMsg requests copying a value to clipboard.
type CopyToClipboardMsg struct {
	Field string
	Value string
}

func NewDetailModel(entry keepass.Entry) DetailModel {
	m := DetailModel{
		entry:    entry,
		strength: keepass.EvaluatePasswordStrength(entry.Password),
	}
	m.refreshTOTP()
	return m
}

// SetAttachments sets the attachment list for display.
func (m *DetailModel) SetAttachments(attachments []keepass.Attachment) {
	m.attachments = attachments
}

func (m DetailModel) Init() tea.Cmd {
	if m.entry.TOTP != "" {
		return tickTOTP()
	}
	return nil
}

func tickTOTP() tea.Cmd {
	return tea.Tick(time.Second, func(t time.Time) tea.Msg {
		return totpTickMsg(t)
	})
}

func (m *DetailModel) refreshTOTP() {
	secret := keepass.TOTPSecret(m.entry.TOTP)
	if secret == "" {
		return
	}
	period := keepass.TOTPPeriod(m.entry.TOTP)
	now := time.Now()
	code, err := totp.GenerateCodeCustom(secret, now, totp.ValidateOpts{
		Period: period,
		Digits: 6,
	})
	if err != nil {
		m.totpCode = ""
		m.totpRemain = 0
		return
	}
	m.totpCode = code
	m.totpRemain = int(period) - int(now.Unix()%int64(period))
}

func (m DetailModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case totpTickMsg:
		m.refreshTOTP()
		return m, tickTOTP()

	case tea.KeyMsg:
		switch msg.String() {
		case "q":
			return m, func() tea.Msg { return RequestQuitMsg{} }
		case "ctrl+c":
			return m, func() tea.Msg { return RequestQuitMsg{} }

		case "esc", "h", "left":
			return m, func() tea.Msg { return BackToBrowserMsg{} }

		case "p":
			m.showPassword = !m.showPassword

		case "e":
			return m, func() tea.Msg {
				return EditEntryMsg{Entry: m.entry}
			}

		case "1":
			return m, func() tea.Msg {
				return CopyToClipboardMsg{Field: "username", Value: m.entry.UserName}
			}

		case "2":
			return m, func() tea.Msg {
				return CopyToClipboardMsg{Field: "password", Value: m.entry.Password}
			}

		case "3":
			return m, func() tea.Msg {
				return CopyToClipboardMsg{Field: "url", Value: m.entry.URL}
			}

		case "4":
			if m.totpCode != "" {
				return m, func() tea.Msg {
					return CopyToClipboardMsg{Field: "TOTP code", Value: m.totpCode}
				}
			}

		case "5":
			// HIBP breach check
			if !m.hibpChecking && m.entry.Password != "" {
				m.hibpChecking = true
				m.hibpResult = nil
				pw := m.entry.Password
				return m, func() tea.Msg {
					result := keepass.CheckHIBP(pw)
					return hibpResultMsg{
						Breached: result.Breached,
						Count:    result.Count,
						Err:      result.Err,
					}
				}
			}

		case "o":
			if m.entry.URL != "" {
				return m, func() tea.Msg {
					return OpenURLMsg{URL: m.entry.URL}
				}
			}
		}
	}

	return m, nil
}

func (m DetailModel) View() string {
	var b strings.Builder

	b.WriteString("\n")
	title := m.entry.Title
	if title == "" {
		title = "(untitled)"
	}
	b.WriteString(titleStyle.Render(fmt.Sprintf(" %s ", title)))
	b.WriteString("\n\n")

	b.WriteString(fmt.Sprintf("  %s %s\n",
		labelStyle.Render("Username:"),
		valueStyle.Render(m.entry.UserName)))

	pw := strings.Repeat("*", len(m.entry.Password))
	if m.showPassword {
		pw = m.entry.Password
	}
	strengthStr := renderStrength(m.strength)
	b.WriteString(fmt.Sprintf("  %s %s  %s\n",
		labelStyle.Render("Password:"),
		hiddenStyle.Render(pw),
		strengthStr))

	b.WriteString(fmt.Sprintf("  %s %s\n",
		labelStyle.Render("URL:"),
		valueStyle.Render(m.entry.URL)))

	if m.totpCode != "" {
		bar := totpCountdownBar(m.totpRemain, int(keepass.TOTPPeriod(m.entry.TOTP)))
		b.WriteString(fmt.Sprintf("  %s %s  %s\n",
			labelStyle.Render("TOTP:"),
			valueStyle.Render(m.totpCode),
			helpStyle.Render(fmt.Sprintf("%s %ds", bar, m.totpRemain))))
	}

	if m.entry.Notes != "" {
		b.WriteString(fmt.Sprintf("  %s\n",
			labelStyle.Render("Notes:")))
		for _, line := range strings.Split(m.entry.Notes, "\n") {
			b.WriteString(fmt.Sprintf("  %s%s\n",
				strings.Repeat(" ", 13),
				valueStyle.Render(line)))
		}
	}

	if !m.entry.CreationTime.IsZero() {
		b.WriteString(fmt.Sprintf("  %s %s\n",
			labelStyle.Render("Created:"),
			hiddenStyle.Render(m.entry.CreationTime.Local().Format("2006-01-02 15:04:05"))))
	}
	if !m.entry.ModificationTime.IsZero() {
		b.WriteString(fmt.Sprintf("  %s %s\n",
			labelStyle.Render("Modified:"),
			hiddenStyle.Render(m.entry.ModificationTime.Local().Format("2006-01-02 15:04:05"))))
	}

	for _, cf := range m.entry.CustomFields {
		val := cf.Value
		if cf.Protected && !m.showPassword {
			val = strings.Repeat("*", len(cf.Value))
		}
		b.WriteString(fmt.Sprintf("  %s %s\n",
			labelStyle.Render(cf.Key+":"),
			valueStyle.Render(val)))
	}

	// Attachments
	if len(m.attachments) > 0 {
		b.WriteString(fmt.Sprintf("\n  %s\n", helpStyle.Render("Attachments:")))
		for _, a := range m.attachments {
			b.WriteString(fmt.Sprintf("    %s (%s)\n",
				valueStyle.Render(a.Name),
				hiddenStyle.Render(formatAttachmentSize(a.Size))))
		}
	}

	// HIBP result
	if m.hibpChecking {
		b.WriteString("\n")
		b.WriteString(helpStyle.Render("  Checking HIBP..."))
		b.WriteString("\n")
	} else if m.hibpResult != nil {
		b.WriteString("\n")
		if m.hibpResult.Err != nil {
			b.WriteString(errorMsgStyle.Render(fmt.Sprintf("  HIBP check failed: %s", m.hibpResult.Err)))
		} else if m.hibpResult.Breached {
			b.WriteString(errorMsgStyle.Render(fmt.Sprintf("  WARNING: Password found in %d data breaches!", m.hibpResult.Count)))
		} else {
			b.WriteString(successStyle.Render("  Password not found in any known breaches"))
		}
		b.WriteString("\n")
	}

	if m.copied != "" {
		b.WriteString("\n")
		b.WriteString(successStyle.Render(fmt.Sprintf("  Copied %s to clipboard!", m.copied)))
		b.WriteString("\n")
	}

	b.WriteString("\n")
	help := "  p: toggle password  e: edit  o: open url  1: copy user  2: copy pass  3: copy url"
	if m.totpCode != "" {
		help += "  4: copy totp"
	}
	help += "  5: HIBP check  esc: back  q: quit"
	b.WriteString(helpStyle.Render(help))
	b.WriteString("\n")

	return b.String()
}

// renderStrength returns a styled strength label.
func renderStrength(s keepass.StrengthResult) string {
	label := fmt.Sprintf("[%s %.0f bits]", s.Label, s.Entropy)
	switch s.Score {
	case keepass.StrengthWeak:
		return errorMsgStyle.Render(label)
	case keepass.StrengthFair:
		return warnStyle.Render(label)
	case keepass.StrengthGood:
		return valueStyle.Render(label)
	case keepass.StrengthStrong, keepass.StrengthExcellent:
		return successStyle.Render(label)
	default:
		return hiddenStyle.Render(label)
	}
}

func formatAttachmentSize(bytes int) string {
	if bytes < 1024 {
		return fmt.Sprintf("%d B", bytes)
	}
	return fmt.Sprintf("%.1f KB", float64(bytes)/1024)
}

// totpCountdownBar renders a visual progress bar for the TOTP countdown.
func totpCountdownBar(remaining, period int) string {
	const width = 10
	filled := remaining * width / period
	if filled > width {
		filled = width
	}
	return "[" + strings.Repeat("#", filled) + strings.Repeat("-", width-filled) + "]"
}
