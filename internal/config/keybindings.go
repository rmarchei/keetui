package config

import (
	"fmt"
	"reflect"
	"slices"
	"sort"
	"strings"
)

// KeyBindings holds configurable key mappings for the TUI.
// Empty strings are filled from defaults via Merge.
type KeyBindings struct {
	// Shared navigation (text inputs keep their own editing keys).
	NavUp   string `json:"nav_up,omitempty"`
	NavDown string `json:"nav_down,omitempty"`
	Open    string `json:"open,omitempty"`
	Back    string `json:"back,omitempty"`
	Search  string `json:"search,omitempty"`

	// Browser actions
	NewEntry       string `json:"new_entry,omitempty"`
	NewGroup       string `json:"new_group,omitempty"`
	RenameGroup    string `json:"rename_group,omitempty"`
	Delete         string `json:"delete,omitempty"`
	Duplicate      string `json:"duplicate,omitempty"`
	Cut            string `json:"cut,omitempty"`
	Paste          string `json:"paste,omitempty"`
	Save           string `json:"save,omitempty"`
	ShowStats      string `json:"show_stats,omitempty"`
	ShowAudit      string `json:"show_audit,omitempty"`
	ShowSettings   string `json:"show_settings,omitempty"`
	ChangePassword string `json:"change_password,omitempty"`
	Favorite       string `json:"favorite,omitempty"`
	CycleSort      string `json:"cycle_sort,omitempty"`
	FilterTag      string `json:"filter_tag,omitempty"`
	Quit           string `json:"quit,omitempty"`

	// Detail view
	TogglePassword string `json:"toggle_password,omitempty"`
	Edit           string `json:"edit,omitempty"`
	OpenURL        string `json:"open_url,omitempty"`
	Attachments    string `json:"attachments,omitempty"`
	CopyUsername   string `json:"copy_username,omitempty"`
	CopyPassword   string `json:"copy_password,omitempty"`
	CopyURL        string `json:"copy_url,omitempty"`
	CopyTOTP       string `json:"copy_totp,omitempty"`
	HIBPCheck      string `json:"hibp_check,omitempty"`
	ShowQR         string `json:"show_qr,omitempty"`
	ShowHistory    string `json:"show_history,omitempty"`

	// Recycle bin
	Restore         string `json:"restore,omitempty"`
	EmptyRecycleBin string `json:"empty_recycle_bin,omitempty"`
}

// DefaultKeyBindings returns the default key mappings matching the original
// hardcoded values.
func DefaultKeyBindings() KeyBindings {
	return KeyBindings{
		NavUp:   "k",
		NavDown: "j",
		Open:    "l",
		Back:    "h",
		Search:  "/",

		NewEntry:       "n",
		NewGroup:       "g",
		RenameGroup:    "r",
		Delete:         "d",
		Duplicate:      "y",
		Cut:            "x",
		Paste:          "v",
		Save:           "s",
		ShowStats:      "i",
		ShowAudit:      "I",
		ShowSettings:   "c",
		ChangePassword: "P",
		Favorite:       "f",
		CycleSort:      "S",
		FilterTag:      "t",
		Quit:           "q",

		TogglePassword: "p",
		Edit:           "e",
		OpenURL:        "o",
		Attachments:    "a",
		CopyUsername:   "1",
		CopyPassword:   "2",
		CopyURL:        "3",
		CopyTOTP:       "4",
		HIBPCheck:      "b",
		ShowQR:         "Q",
		ShowHistory:    "H",

		Restore:         "R",
		EmptyRecycleBin: "E",
	}
}

// Merge fills empty fields in kb with values from defaults.
func (kb KeyBindings) Merge(defaults KeyBindings) KeyBindings {
	if kb.NavUp == "" {
		kb.NavUp = defaults.NavUp
	}
	if kb.NavDown == "" {
		kb.NavDown = defaults.NavDown
	}
	if kb.Open == "" {
		kb.Open = defaults.Open
	}
	if kb.Back == "" {
		kb.Back = defaults.Back
	}
	if kb.Search == "" {
		kb.Search = defaults.Search
	}
	if kb.NewEntry == "" {
		kb.NewEntry = defaults.NewEntry
	}
	if kb.NewGroup == "" {
		kb.NewGroup = defaults.NewGroup
	}
	if kb.RenameGroup == "" {
		kb.RenameGroup = defaults.RenameGroup
	}
	if kb.Delete == "" {
		kb.Delete = defaults.Delete
	}
	if kb.Duplicate == "" {
		kb.Duplicate = defaults.Duplicate
	}
	if kb.Cut == "" {
		kb.Cut = defaults.Cut
	}
	if kb.Paste == "" {
		kb.Paste = defaults.Paste
	}
	if kb.Save == "" {
		kb.Save = defaults.Save
	}
	if kb.ShowStats == "" {
		kb.ShowStats = defaults.ShowStats
	}
	if kb.ShowAudit == "" {
		kb.ShowAudit = defaults.ShowAudit
	}
	if kb.ShowSettings == "" {
		kb.ShowSettings = defaults.ShowSettings
	}
	if kb.ChangePassword == "" {
		kb.ChangePassword = defaults.ChangePassword
	}
	if kb.Favorite == "" {
		kb.Favorite = defaults.Favorite
	}
	if kb.CycleSort == "" {
		kb.CycleSort = defaults.CycleSort
	}
	if kb.FilterTag == "" {
		kb.FilterTag = defaults.FilterTag
	}
	if kb.Quit == "" {
		kb.Quit = defaults.Quit
	}
	if kb.TogglePassword == "" {
		kb.TogglePassword = defaults.TogglePassword
	}
	if kb.Edit == "" {
		kb.Edit = defaults.Edit
	}
	if kb.OpenURL == "" {
		kb.OpenURL = defaults.OpenURL
	}
	if kb.Attachments == "" {
		kb.Attachments = defaults.Attachments
	}
	if kb.CopyUsername == "" {
		kb.CopyUsername = defaults.CopyUsername
	}
	if kb.CopyPassword == "" {
		kb.CopyPassword = defaults.CopyPassword
	}
	if kb.CopyURL == "" {
		kb.CopyURL = defaults.CopyURL
	}
	if kb.CopyTOTP == "" {
		kb.CopyTOTP = defaults.CopyTOTP
	}
	if kb.HIBPCheck == "" {
		kb.HIBPCheck = defaults.HIBPCheck
	}
	if kb.ShowQR == "" {
		kb.ShowQR = defaults.ShowQR
	}
	if kb.ShowHistory == "" {
		kb.ShowHistory = defaults.ShowHistory
	}
	if kb.Restore == "" {
		kb.Restore = defaults.Restore
	}
	if kb.EmptyRecycleBin == "" {
		kb.EmptyRecycleBin = defaults.EmptyRecycleBin
	}
	return kb
}

// Validate checks for duplicate bindings within the same context.
// Returns an error listing duplicates, or nil if no conflicts found.
func (kb KeyBindings) Validate() error {
	conflicts := kb.conflicts()
	if len(conflicts) == 0 {
		return nil
	}
	var descriptions []string
	for _, conflict := range conflicts {
		descriptions = append(descriptions, fmt.Sprintf("%s: %q bound to %v", conflict.context, conflict.key, conflict.actions))
	}
	sort.Strings(descriptions)
	return fmt.Errorf("duplicate keybindings: %s", strings.Join(descriptions, "; "))
}

type bindingConflict struct {
	context, key string
	actions      []string
}

func (kb KeyBindings) conflicts() []bindingConflict {
	// Browser context keys
	browser := map[string][]string{}
	addKey := func(m map[string][]string, key, name string) {
		if key != "" {
			m[key] = append(m[key], name)
		}
	}

	addKey(browser, kb.NavUp, "nav_up")
	addKey(browser, kb.NavDown, "nav_down")
	addKey(browser, kb.Open, "open")
	addKey(browser, kb.Back, "back")
	addKey(browser, kb.Search, "search")
	addKey(browser, kb.NewEntry, "new_entry")
	addKey(browser, kb.NewGroup, "new_group")
	addKey(browser, kb.RenameGroup, "rename_group")
	addKey(browser, kb.Delete, "delete")
	addKey(browser, kb.Duplicate, "duplicate")
	addKey(browser, kb.Cut, "cut")
	addKey(browser, kb.Paste, "paste")
	addKey(browser, kb.Save, "save")
	addKey(browser, kb.ShowStats, "show_stats")
	addKey(browser, kb.ShowAudit, "show_audit")
	addKey(browser, kb.ShowSettings, "show_settings")
	addKey(browser, kb.Favorite, "favorite")
	addKey(browser, kb.CycleSort, "cycle_sort")
	addKey(browser, kb.FilterTag, "filter_tag")
	addKey(browser, kb.ChangePassword, "change_password")
	addKey(browser, kb.Restore, "restore")
	addKey(browser, kb.EmptyRecycleBin, "empty_recycle_bin")
	addKey(browser, kb.Quit, "quit")

	// Detail context keys
	detail := map[string][]string{}
	addKey(detail, kb.NavUp, "nav_up")
	addKey(detail, kb.NavDown, "nav_down")
	addKey(detail, kb.TogglePassword, "toggle_password")
	addKey(detail, kb.Edit, "edit")
	addKey(detail, kb.OpenURL, "open_url")
	addKey(detail, kb.Attachments, "attachments")
	addKey(detail, kb.CopyUsername, "copy_username")
	addKey(detail, kb.CopyPassword, "copy_password")
	addKey(detail, kb.CopyURL, "copy_url")
	addKey(detail, kb.CopyTOTP, "copy_totp")
	addKey(detail, kb.HIBPCheck, "hibp_check")
	addKey(detail, kb.ShowQR, "show_qr")
	addKey(detail, kb.ShowHistory, "show_history")
	addKey(detail, kb.Back, "back")
	addKey(detail, kb.Quit, "quit")

	// Hardcoded keys handled before configurable bindings in the key
	// switches; a user binding that collides with one is shadowed and
	// never fires. aliasOf names the action the built-in key duplicates —
	// binding that same action to the key is harmless and not flagged.
	addReserved := func(m map[string][]string, key, aliasOf string) {
		if names, ok := m[key]; ok && len(names) == 1 && names[0] == aliasOf {
			return
		}
		m[key] = append(m[key], "built-in "+key)
	}
	addReserved(browser, "up", "nav_up")
	addReserved(browser, "down", "nav_down")
	addReserved(browser, "enter", "open")
	addReserved(browser, "right", "open")
	addReserved(browser, "esc", "back")
	addReserved(browser, "left", "back")
	addReserved(browser, "?", "")
	addReserved(browser, "f1", "")
	addReserved(browser, "ctrl+c", "")
	addReserved(browser, "ctrl+z", "")
	addReserved(browser, "ctrl+s", "save")
	addReserved(browser, "ctrl+o", "show_settings")
	addReserved(detail, "enter", "")
	addReserved(detail, "esc", "back")
	addReserved(detail, "left", "back")
	addReserved(detail, "ctrl+c", "")
	addReserved(detail, "f1", "")
	addReserved(detail, "?", "")
	addReserved(detail, "up", "nav_up")
	addReserved(detail, "down", "nav_down")
	addReserved(detail, "ctrl+z", "")

	// These screens share navigation but have additional fixed controls.
	// History, QR and audit use subsets of the contexts checked above.
	attachments := map[string][]string{}
	stats := map[string][]string{}
	help := map[string][]string{}
	for _, context := range []map[string][]string{attachments, stats, help} {
		addKey(context, kb.NavUp, "nav_up")
		addKey(context, kb.NavDown, "nav_down")
		addKey(context, kb.Quit, "quit")
	}
	addKey(attachments, kb.Back, "back")
	addKey(attachments, kb.Save, "save")
	addKey(attachments, kb.Delete, "delete")
	addReserved(attachments, "+", "")
	addKey(stats, kb.Back, "back")
	for _, key := range []string{"pgup", "pgdown", "home", "end"} {
		addReserved(stats, key, "")
	}
	for _, key := range []string{"tab", "shift+tab", "left", "right", "pgup", "pgdown"} {
		addReserved(help, key, "")
	}

	var conflicts []bindingConflict
	for context, bindings := range map[string]map[string][]string{
		"browser": browser, "detail": detail, "attachments": attachments, "stats": stats, "help": help,
	} {
		for key, names := range bindings {
			if len(names) > 1 {
				conflicts = append(conflicts, bindingConflict{context, key, names})
			}
		}
	}

	return conflicts
}

// Overrides omits inherited defaults so saving settings does not pin them.
func (kb KeyBindings) Overrides() *KeyBindings {
	defaults := DefaultKeyBindings().Bindings()
	var overrides KeyBindings
	for i, binding := range kb.Bindings() {
		if binding.Key != defaults[i].Key {
			overrides, _ = overrides.WithBinding(binding.ID, binding.Key)
		}
	}
	if overrides == (KeyBindings{}) {
		return nil
	}
	return &overrides
}

// Previous defaults, newest first. Omitted fields did not change. Keep this
// history as data so conflict repair is independent of particular actions.
var previousKeyDefaults = []KeyBindings{
	{HIBPCheck: "5"},
}

// repairBindings retains unrelated overrides, including valid swaps. Reset
// only overrides involved in conflicts, iterating if a reset exposes another
// conflict with a default. It never rewrites the user's file during loading.
func repairBindings(overrides KeyBindings) (*KeyBindings, error) {
	return repairBindingsWithHistory(overrides, previousKeyDefaults)
}

func repairBindingsWithHistory(overrides KeyBindings, history []KeyBindings) (*KeyBindings, error) {
	defaults := DefaultKeyBindings()
	var changes []string
	// Only implicit defaults may migrate, and each action gets at most one
	// migration. Later resets can expose conflicts with a historical key;
	// do not oscillate between restoring it and resetting it again.
	inherited := make(map[string]bool)
	for _, binding := range overrides.Bindings() {
		if binding.Key == "" {
			inherited[binding.ID] = true
		}
	}
	for {
		conflicts := overrides.Merge(defaults).conflicts()
		if len(conflicts) == 0 {
			break
		}
		reset := make(map[string]bool)
		for _, conflict := range conflicts {
			for _, action := range conflict.actions {
				reset[action] = true
			}
		}
		// Try a free historical default before discarding an explicit user
		// shortcut. Validate its action in every context, allowing unrelated
		// conflicts to be repaired by later iterations.
		restored := false
		for _, previous := range history {
			for _, binding := range previous.Bindings() {
				if binding.Key == "" || !inherited[binding.ID] || !reset[binding.ID] {
					continue
				}
				candidate, _ := overrides.WithBinding(binding.ID, binding.Key)
				if slices.ContainsFunc(candidate.Merge(defaults).conflicts(), func(c bindingConflict) bool {
					return slices.Contains(c.actions, binding.ID)
				}) {
					continue
				}
				overrides = candidate
				delete(inherited, binding.ID)
				changes = append(changes, "retained legacy "+binding.ID+" shortcut "+binding.Key)
				restored = true
				break
			}
			if restored {
				break
			}
		}
		if restored {
			continue
		}
		for _, binding := range overrides.Bindings() {
			if binding.Key != "" && reset[binding.ID] {
				overrides, _ = overrides.WithBinding(binding.ID, "")
				changes = append(changes, "reset conflicting "+binding.ID)
			}
		}
	}
	var result *KeyBindings
	if overrides != (KeyBindings{}) {
		result = &overrides
	}
	if len(changes) > 0 {
		sort.Strings(changes)
		return result, fmt.Errorf("keybindings adjusted: %s", strings.Join(changes, "; "))
	}
	return result, nil
}

// Binding describes one configurable action. JSON field names are stable IDs
// shared by the config file and the key-recording settings editor.
type Binding struct {
	ID, Label, Key string
}

func (kb KeyBindings) Bindings() []Binding {
	value := reflect.ValueOf(kb)
	typ := value.Type()
	bindings := make([]Binding, 0, value.NumField())
	for i := 0; i < value.NumField(); i++ {
		id := strings.Split(typ.Field(i).Tag.Get("json"), ",")[0]
		bindings = append(bindings, Binding{ID: id, Label: strings.ReplaceAll(id, "_", " "), Key: value.Field(i).String()})
	}
	return bindings
}

func (kb KeyBindings) WithBinding(id, key string) (KeyBindings, error) {
	value := reflect.ValueOf(&kb).Elem()
	typ := value.Type()
	for i := 0; i < value.NumField(); i++ {
		if strings.Split(typ.Field(i).Tag.Get("json"), ",")[0] == id {
			value.Field(i).SetString(key)
			return kb, nil
		}
	}
	return kb, fmt.Errorf("unknown keybinding %q", id)
}

// String returns a human-readable table of all keybindings.
func (kb KeyBindings) String() string {
	var b strings.Builder
	section := func(title string, bindings []struct{ name, key string }) {
		b.WriteString(title + ":\n")
		for _, bind := range bindings {
			fmt.Fprintf(&b, "  %-20s %s\n", bind.name, bind.key)
		}
	}

	section("Browser", []struct{ name, key string }{
		{"nav_up", kb.NavUp},
		{"nav_down", kb.NavDown},
		{"open", kb.Open},
		{"back", kb.Back},
		{"search", kb.Search},
		{"new_entry", kb.NewEntry},
		{"new_group", kb.NewGroup},
		{"rename_group", kb.RenameGroup},
		{"delete", kb.Delete},
		{"duplicate", kb.Duplicate},
		{"cut", kb.Cut},
		{"paste", kb.Paste},
		{"save", kb.Save},
		{"show_stats", kb.ShowStats},
		{"show_audit", kb.ShowAudit},
		{"show_settings", kb.ShowSettings},
		{"favorite", kb.Favorite},
		{"cycle_sort", kb.CycleSort},
		{"filter_tag", kb.FilterTag},
		{"change_password", kb.ChangePassword},
		{"quit", kb.Quit},
	})

	section("Detail", []struct{ name, key string }{
		{"toggle_password", kb.TogglePassword},
		{"edit", kb.Edit},
		{"open_url", kb.OpenURL},
		{"attachments", kb.Attachments},
		{"copy_username", kb.CopyUsername},
		{"copy_password", kb.CopyPassword},
		{"copy_url", kb.CopyURL},
		{"copy_totp", kb.CopyTOTP},
		{"hibp_check", kb.HIBPCheck},
		{"show_qr", kb.ShowQR},
		{"show_history", kb.ShowHistory},
	})

	section("Recycle Bin", []struct{ name, key string }{
		{"restore", kb.Restore},
		{"empty_recycle_bin", kb.EmptyRecycleBin},
	})

	return b.String()
}
