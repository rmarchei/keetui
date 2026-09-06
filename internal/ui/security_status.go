package ui

import (
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
)

// The display timer only repaints the countdowns. The existing security timers
// perform clearing/locking, independently of screen, input, and repaint timing.
type securityTickMsg struct {
	gen int
	at  time.Time
}

func (m *AppModel) recordActivity() {
	now := time.Now()
	m.lastActivity, m.securityNow = now, now
}

func (m AppModel) needsSecurityTick() bool {
	return m.state != viewUnlock && m.db != nil &&
		(m.cfg.AutoLockDuration() > 0 || !m.clipboardClearAt.IsZero())
}

func (m *AppModel) ensureSecurityTick() tea.Cmd {
	if !m.needsSecurityTick() {
		m.stopSecurityTick()
		return nil
	}
	if m.securityTickPending {
		return nil
	}
	m.securityTickGen++
	m.securityTickPending = true
	gen := m.securityTickGen
	return tea.Tick(time.Second, func(t time.Time) tea.Msg {
		return securityTickMsg{gen: gen, at: t}
	})
}

func (m *AppModel) stopSecurityTick() {
	if m.securityTickPending {
		m.securityTickGen++
		m.securityTickPending = false
	}
}

func (m *AppModel) forgetCopiedValue() {
	m.clipboardGen++
	m.clipboardClearAt = time.Time{}
	m.lastCopied = ""
}

func (m AppModel) autoLockDeadline() time.Time {
	deadline := m.lastActivity.Add(m.cfg.AutoLockDuration())
	// Activity extends the idle deadline. A deferred check (save in progress
	// or failed save before locking) can delay it further, never shorten it.
	if m.autoLockCheckAt.After(deadline) {
		deadline = m.autoLockCheckAt
	}
	return deadline
}

func countdown(deadline, now time.Time) string {
	d := deadline.Sub(now)
	seconds := max(0, int64(d/time.Second))
	if d > 0 && d%time.Second != 0 {
		seconds++ // never display zero before the deadline
	}
	if seconds >= 60 {
		return fmt.Sprintf("%d:%02d", seconds/60, seconds%60)
	}
	return fmt.Sprintf("%ds", seconds)
}

func (m AppModel) securityStatus(width int) string {
	if m.state == viewUnlock || m.db == nil {
		return ""
	}
	now := m.securityNow
	if now.IsZero() {
		now = m.lastActivity
	}
	clipLabel, lockLabel := "clip:", "lock:"
	if width < 38 {
		clipLabel, lockLabel = "C:", "L:"
	}
	var parts []string
	if !m.clipboardClearAt.IsZero() {
		parts = append(parts, valueStyle.Render(clipLabel+countdown(m.clipboardClearAt, now)))
	} else if m.lastCopied != "" {
		parts = append(parts, warnStyle.Padding(0).Render(clipLabel+"off"))
	}
	if m.cfg.AutoLockDuration() == 0 {
		parts = append(parts, helpStyle.Render(lockLabel+"off"))
	} else {
		deadline := m.autoLockDeadline()
		style := valueStyle
		if deadline.Sub(now) <= 30*time.Second {
			style = warnStyle.Padding(0).Bold(true)
		}
		parts = append(parts, style.Render(lockLabel+countdown(deadline, now)))
	}
	return strings.Join(parts, " ")
}

// Keep clipboard I/O injectable so timer regressions never access the user's
// real clipboard. A nil backend retains the existing platform/OSC 52 behavior.
type clipboardAccess interface {
	write(string) error
	read() (string, error)
	clear()
}

type systemClipboard struct{}

func (systemClipboard) write(value string) error { return copyToClipboard(value) }
func (systemClipboard) read() (string, error)    { return readClipboard() }
func (systemClipboard) clear()                   { clearClipboard() }

func (m AppModel) clipboardBackend() clipboardAccess {
	if m.clipboard != nil {
		return m.clipboard
	}
	return systemClipboard{}
}
