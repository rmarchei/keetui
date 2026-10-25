package ui

import (
	"fmt"
	"slices"
	"strconv"
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"

	"github.com/rmarchei/keetui/internal/config"
)

// databaseSettingList are the settings a database can override, in the
// order its editor lists them. The theme, the strength display and
// keybindings belong to the user, not the vault, and stay global.
var databaseSettingList = []int{
	settingClipboardClear, settingClipboardOSC52, settingAutoLock,
	settingPasswordLength, settingPWUppercase, settingPWLowercase, settingPWDigits, settingPWSymbols, settingDefaultPreset,
	settingConfirmQuit, settingBackupOnSave,
}

// databaseSettings edits the open database's settings. Values are kept as
// the global editor keeps them (a number, "1" or "0", a choice); an empty
// value inherits the global setting.
type databaseSettings struct {
	open, readOnly, editing bool
	saved                   config.Overrides // as stored in the database
	values                  [settingCount]textinput.Model
	focus                   int // index into databaseSettingList
	offset                  int
	errors                  map[int]string
}

// setDatabase attaches the open database's settings to the editor.
func (m *SettingsModel) setDatabase(o config.Overrides, readOnly bool) {
	m.db = databaseSettings{open: true, readOnly: readOnly, saved: o}
	for _, i := range databaseSettingList {
		input := newTextInput()
		input.Prompt = ""
		input.CharLimit = 6
		input.SetValue(overrideValue(o, i))
		m.db.values[i] = input
	}
	m.db.values[databaseSettingList[0]].Focus()
	m.syncLayout()
}

// overrideValue is a database setting as the editor keeps it.
func overrideValue(o config.Overrides, i int) string {
	number := func(p *int) string {
		if p == nil {
			return ""
		}
		return strconv.Itoa(*p)
	}
	text := func(p *string) string {
		if p == nil {
			return ""
		}
		return *p
	}
	flag := func(p *bool) string {
		switch {
		case p == nil:
			return ""
		case *p:
			return "1"
		default:
			return "0"
		}
	}
	switch i {
	case settingClipboardClear:
		return number(o.ClipboardClearSeconds)
	case settingClipboardOSC52:
		return text(o.ClipboardOSC52)
	case settingAutoLock:
		return number(o.AutoLockMinutes)
	case settingPasswordLength:
		return number(o.PasswordLength)
	case settingPWUppercase:
		return flag(o.PasswordUppercase)
	case settingPWLowercase:
		return flag(o.PasswordLowercase)
	case settingPWDigits:
		return flag(o.PasswordDigits)
	case settingPWSymbols:
		return flag(o.PasswordSymbols)
	case settingDefaultPreset:
		return text(o.DefaultPreset)
	case settingConfirmQuit:
		return flag(o.ConfirmQuit)
	case settingBackupOnSave:
		return flag(o.BackupOnSave)
	}
	return ""
}

// globalValue is the global setting i would inherit, for display.
func globalValue(cfg config.Config, i int) string {
	onOff := func(on bool) string {
		if on {
			return "On"
		}
		return "Off"
	}
	switch i {
	case settingClipboardClear:
		return strconv.Itoa(cfg.ClipboardClearSeconds)
	case settingClipboardOSC52:
		return cfg.OSC52Mode()
	case settingAutoLock:
		return strconv.Itoa(cfg.AutoLockMinutes)
	case settingPasswordLength:
		return strconv.Itoa(cfg.PasswordLength)
	case settingPWUppercase:
		return onOff(cfg.PasswordHasUppercase())
	case settingPWLowercase:
		return onOff(cfg.PasswordHasLowercase())
	case settingPWDigits:
		return onOff(cfg.PasswordHasDigits())
	case settingPWSymbols:
		return onOff(cfg.PasswordHasSymbols())
	case settingDefaultPreset:
		return cfg.Preset()
	case settingConfirmQuit:
		return onOff(cfg.ConfirmOnQuit())
	case settingBackupOnSave:
		return onOff(cfg.BackupEnabled())
	}
	return ""
}

// displayValue shows a kept value: toggles as On or Off.
func displayValue(i int, v string) string {
	if isToggleSetting(i) {
		if v == "1" {
			return "On"
		}
		return "Off"
	}
	return v
}

// databaseChoices are the values a toggle or choice cycles through after
// "inherit".
func databaseChoices(i int) []string {
	switch {
	case isToggleSetting(i):
		return []string{"1", "0"}
	case i == settingClipboardOSC52:
		return config.OSC52Modes
	case i == settingDefaultPreset:
		return PresetNames
	}
	return nil
}

// summary is the "This database" row's value in the global editor.
func (d databaseSettings) summary() string {
	if !d.open {
		return "(no database open)"
	}
	set := 0
	for _, i := range databaseSettingList {
		if strings.TrimSpace(d.values[i].Value()) != "" {
			set++
		}
	}
	label := "[ Edit… ]"
	if d.readOnly {
		label = "[ View… ]"
	}
	if set == 0 {
		return label + "  none set"
	}
	return fmt.Sprintf("%s  %d set", label, set)
}

// override returns the database's value for global setting i, if it sets
// one, for the note under that setting in the global editor.
func (d databaseSettings) override(i int) (string, bool) {
	if !d.open || !slices.Contains(databaseSettingList, i) {
		return "", false
	}
	v := strings.TrimSpace(d.values[i].Value())
	if v == "" {
		return "", false
	}
	return displayValue(i, v), true
}

// draftOverrides validates the database's settings against the global ones
// being edited, without changing anything.
func (m SettingsModel) draftOverrides(global config.Config) (config.Overrides, map[int]string) {
	var o config.Overrides
	errs := make(map[int]string)
	if !m.db.open {
		return o, errs
	}
	value := func(i int) string { return strings.TrimSpace(m.db.values[i].Value()) }
	number := func(i, minimum int, allowZero bool) *int {
		v := value(i)
		if v == "" {
			return nil
		}
		n, err := strconv.Atoi(v)
		if err != nil || n < minimum && (!allowZero || n != 0) {
			errs[i] = fmt.Sprintf("Must be at least %d", minimum)
			if allowZero {
				errs[i] += ", 0 to disable,"
			}
			errs[i] += " or empty to inherit"
			return nil
		}
		return &n
	}
	flag := func(i int) *bool {
		switch value(i) {
		case "1":
			return boolPtr(true)
		case "0":
			return boolPtr(false)
		}
		return nil
	}
	choice := func(i int) *string {
		v := value(i)
		if v == "" {
			return nil
		}
		if !slices.Contains(databaseChoices(i), v) {
			errs[i] = "Choose from the list"
			return nil
		}
		return &v
	}
	o.ClipboardClearSeconds = number(settingClipboardClear, 15, true)
	o.ClipboardOSC52 = choice(settingClipboardOSC52)
	o.AutoLockMinutes = number(settingAutoLock, 5, true)
	o.PasswordLength = number(settingPasswordLength, 8, false)
	o.PasswordUppercase = flag(settingPWUppercase)
	o.PasswordLowercase = flag(settingPWLowercase)
	o.PasswordDigits = flag(settingPWDigits)
	o.PasswordSymbols = flag(settingPWSymbols)
	o.DefaultPreset = choice(settingDefaultPreset)
	o.ConfirmQuit = flag(settingConfirmQuit)
	o.BackupOnSave = flag(settingBackupOnSave)
	pick := func(override *bool, inherited bool) bool {
		if override != nil {
			return *override
		}
		return inherited
	}
	if !pick(o.PasswordUppercase, global.PasswordHasUppercase()) && !pick(o.PasswordLowercase, global.PasswordHasLowercase()) &&
		!pick(o.PasswordDigits, global.PasswordHasDigits()) && !pick(o.PasswordSymbols, global.PasswordHasSymbols()) {
		errs[settingPWSymbols] = "At least one character set must be enabled"
	}
	return o, errs
}

func boolPtr(v bool) *bool { return &v }

func (m SettingsModel) updateDatabase(msg tea.Msg) (tea.Model, tea.Cmd) {
	i := databaseSettingList[m.db.focus]
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		return m, nil
	case tea.MouseWheelMsg:
		switch msg.Button {
		case tea.MouseWheelUp:
			return m.moveDatabaseFocus(-1), nil
		case tea.MouseWheelDown:
			return m.moveDatabaseFocus(1), nil
		}
		return m, nil
	case tea.KeyPressMsg:
		m.status, m.isError = "", false
		switch msg.String() {
		case "ctrl+c":
			return m, func() tea.Msg { return RequestQuitMsg{} }
		case "esc":
			m.db.editing = false
			return m, nil
		case "ctrl+s":
			return m.save()
		case "tab", "down":
			return m.moveDatabaseFocus(1), textinput.Blink
		case "shift+tab", "up":
			return m.moveDatabaseFocus(-1), textinput.Blink
		}
		if msg.String() == "enter" && databaseChoices(i) == nil {
			return m.moveDatabaseFocus(1), textinput.Blink
		}
		if m.db.readOnly {
			m.status, m.isError = "This database is read-only: its settings can't be changed", true
			return m, nil
		}
		if choices := databaseChoices(i); choices != nil {
			options := append([]string{""}, choices...)
			current := max(0, slices.Index(options, m.db.values[i].Value()))
			switch msg.String() {
			case "space", "enter", "right":
				m.db.values[i].SetValue(options[(current+1)%len(options)])
			case "left":
				m.db.values[i].SetValue(options[(current+len(options)-1)%len(options)])
			case "backspace", "delete":
				m.db.values[i].SetValue("")
			}
			return m, nil
		}
	}
	if databaseChoices(i) != nil || m.db.readOnly {
		return m, nil
	}
	var cmd tea.Cmd
	m.db.values[i], cmd = m.db.values[i].Update(msg)
	return m, cmd
}

func (m SettingsModel) moveDatabaseFocus(delta int) SettingsModel {
	m.db.values[databaseSettingList[m.db.focus]].Blur()
	m.db.focus = (m.db.focus + delta + len(databaseSettingList)) % len(databaseSettingList)
	m.db.values[databaseSettingList[m.db.focus]].Focus()
	return m
}

// databaseBody lists the database's settings under the global editor's
// section headings, each set or inheriting the global value shown beside it.
func (m SettingsModel) databaseBody() (string, []fieldRows) {
	var b lineBuilder
	width, _ := viewSize(m.width, m.height)
	right := ""
	if m.db.readOnly {
		right = "read-only"
	}
	b.WriteString(header(width, right, "Settings", "This database") + "\n\n")
	b.WriteString(hiddenStyle.Render(wrapIndented("Stored in the database and encrypted with it, these apply over your global settings while it is open; an empty one inherits. Save the database to keep a change.", width, 2)) + "\n")
	global, _ := m.draftConfig()
	rows := make([]fieldRows, 0, len(databaseSettingList))
	for n, i := range databaseSettingList {
		if section, ok := settingSections[i]; ok {
			b.WriteString("\n" + sectionStyle.Render("  "+section) + "\n")
		} else if i == settingConfirmQuit {
			b.WriteString("\n" + sectionStyle.Render("  General") + "\n")
		}
		start := b.row
		focused := n == m.db.focus
		label := labelStyle.Width(0).Render(fmt.Sprintf("  %-24s", settingLabels[i]+":"))
		v := m.db.values[i].Value()
		inherited := hiddenStyle.Render("global: " + globalValue(global, i))
		var value string
		switch {
		case databaseChoices(i) == nil:
			input := m.db.values[i]
			input.Placeholder = "inherit"
			value = input.View()
			if strings.TrimSpace(v) == "" {
				value += "  " + inherited
			}
		case v == "":
			style := secondaryStyle
			if focused {
				style = selectedItemStyle
			}
			value = style.Render("[ inherit ]") + "  " + inherited
		default:
			style := valueStyle
			if focused {
				style = selectedItemStyle
			}
			value = style.Render("[ " + displayValue(i, v) + " ]")
		}
		b.WriteString(formRow(wrapContent(label+value, width), width, focused) + "\n")
		if problem := m.db.errors[i]; problem != "" {
			b.WriteString(errorMsgStyle.Padding(0).Render(wrapIndented(problem, width, 2)) + "\n")
		}
		rows = append(rows, fieldRows{start, b.row - 1})
	}
	return b.String(), rows
}

func (m SettingsModel) databaseFooter() string {
	width, _ := viewSize(m.width, m.height)
	var b strings.Builder
	if m.status != "" {
		style := successStyle
		if m.isError {
			style = errorMsgStyle
		}
		fmt.Fprintf(&b, "%s\n", wrapContent(style.Render(sanitizeDisplay(m.status)), width))
	}
	action := "enter: next"
	if databaseChoices(databaseSettingList[m.db.focus]) != nil {
		action = "space/enter: change"
	}
	b.WriteString(renderHints(width, "ctrl+s: save", "esc: back", "F1: help", action, "backspace: inherit", "tab/shift+tab: navigate"))
	return m.withSaveError(b.String())
}

func (m SettingsModel) databaseView() string {
	width, height := viewSize(m.width, m.height)
	body, _ := m.databaseBody()
	return screen(body, m.databaseFooter(), width, height, m.db.offset)
}

// syncDatabaseLayout revalidates the database's settings and keeps the
// focused one in view.
func (m *SettingsModel) syncDatabaseLayout() {
	if !m.db.open {
		return
	}
	_, height := viewSize(m.width, m.height)
	for _, i := range databaseSettingList {
		m.db.values[i].SetWidth(7) // values are short; the global one follows
	}
	global, _ := m.draftConfig()
	_, m.db.errors = m.draftOverrides(global)
	if m.db.editing {
		body, rows := m.databaseBody()
		available := bodyHeight(height, m.databaseFooter())
		m.db.offset = clampOffset(body, available, followField(m.db.offset, rows[m.db.focus], available))
	}
}
