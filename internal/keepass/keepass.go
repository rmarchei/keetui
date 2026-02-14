// Package keepass provides operations for reading and writing KeePass .kdbx files.
package keepass

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/tobischo/gokeepasslib/v3"
	w "github.com/tobischo/gokeepasslib/v3/wrappers"
)

// DB wraps a gokeepasslib.Database with file path and credentials info.
type DB struct {
	Path     string
	Database *gokeepasslib.Database
	unlocked bool
}

// CustomField represents an extra key-value pair stored on an entry.
type CustomField struct {
	Key       string
	Value     string
	Protected bool
}

// Entry is a simplified view of a KeePass entry.
type Entry struct {
	UUID             gokeepasslib.UUID
	Title            string
	UserName         string
	Password         string
	URL              string
	Notes            string
	TOTP             string // TOTP secret (base32) or otpauth:// URI
	CustomFields     []CustomField
	CreationTime     time.Time
	ModificationTime time.Time
	ExpiryTime       time.Time
	Expires          bool
	HistoryCount     int
}

// IsExpired returns true if the entry has an expiry date set and it is in the past.
func (e Entry) IsExpired() bool {
	return e.Expires && !e.ExpiryTime.IsZero() && time.Now().After(e.ExpiryTime)
}

// HistoryEntry represents a single password history record.
type HistoryEntry struct {
	Password         string
	ModificationTime time.Time
	Title            string
	UserName         string
}

// Group is a simplified view of a KeePass group.
type Group struct {
	UUID    gokeepasslib.UUID
	Name    string
	Groups  []Group
	Entries []Entry
}

// Create creates a new .kdbx database file with the given password.
func Create(path, password string) (*DB, error) {
	rootGroup := gokeepasslib.NewGroup()
	rootGroup.Name = "Root"

	db := gokeepasslib.NewDatabase()
	db.Credentials = gokeepasslib.NewPasswordCredentials(password)
	db.Content.Root = &gokeepasslib.RootData{
		Groups: []gokeepasslib.Group{rootGroup},
	}

	db.LockProtectedEntries()

	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return nil, fmt.Errorf("create file: %w", err)
	}
	defer file.Close()

	if err := gokeepasslib.NewEncoder(file).Encode(db); err != nil {
		return nil, fmt.Errorf("encode database: %w", err)
	}

	db.UnlockProtectedEntries()

	return &DB{
		Path:     path,
		Database: db,
		unlocked: true,
	}, nil
}

// Open reads and decodes a .kdbx file with the given password.
func Open(path, password string) (*DB, error) {
	if err := checkFileExtension(path); err != nil {
		return nil, err
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open file: %w", err)
	}
	defer file.Close()

	db := gokeepasslib.NewDatabase()
	db.Credentials = gokeepasslib.NewPasswordCredentials(password)

	if err := gokeepasslib.NewDecoder(file).Decode(db); err != nil {
		return nil, fmt.Errorf("decode database: %w", err)
	}

	db.UnlockProtectedEntries()

	return &DB{
		Path:     path,
		Database: db,
		unlocked: true,
	}, nil
}

// OpenWithKeyFile reads and decodes a .kdbx file with password and key file.
func OpenWithKeyFile(path, password, keyFilePath string) (*DB, error) {
	if err := checkFileExtension(path); err != nil {
		return nil, err
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open file: %w", err)
	}
	defer file.Close()

	creds, err := gokeepasslib.NewPasswordAndKeyCredentials(password, keyFilePath)
	if err != nil {
		return nil, fmt.Errorf("create credentials: %w", err)
	}

	db := gokeepasslib.NewDatabase()
	db.Credentials = creds

	if err := gokeepasslib.NewDecoder(file).Decode(db); err != nil {
		return nil, fmt.Errorf("decode database: %w", err)
	}

	db.UnlockProtectedEntries()

	return &DB{
		Path:     path,
		Database: db,
		unlocked: true,
	}, nil
}

// RootGroup returns the root group of the database as a simplified Group.
func (d *DB) RootGroup() Group {
	if len(d.Database.Content.Root.Groups) == 0 {
		return Group{Name: "Root"}
	}
	return convertGroup(d.Database.Content.Root.Groups[0])
}

func convertGroup(g gokeepasslib.Group) Group {
	group := Group{
		UUID: g.UUID,
		Name: g.Name,
	}
	for _, sg := range g.Groups {
		group.Groups = append(group.Groups, convertGroup(sg))
	}
	for _, e := range g.Entries {
		entry := Entry{
			UUID:     e.UUID,
			Title:    e.GetContent("Title"),
			UserName: e.GetContent("UserName"),
			Password: e.GetContent("Password"),
			URL:      e.GetContent("URL"),
			Notes:    e.GetContent("Notes"),
			TOTP:     extractTOTP(e),
		}
		if e.Times.CreationTime != nil {
			entry.CreationTime = e.Times.CreationTime.Time
		}
		if e.Times.LastModificationTime != nil {
			entry.ModificationTime = e.Times.LastModificationTime.Time
		}
		entry.Expires = e.Times.Expires.Bool
		if e.Times.ExpiryTime != nil {
			entry.ExpiryTime = e.Times.ExpiryTime.Time
		}
		for _, h := range e.Histories {
			entry.HistoryCount += len(h.Entries)
		}
		entry.CustomFields = extractCustomFields(e)
		group.Entries = append(group.Entries, entry)
	}
	return group
}

// Save writes the database back to its file.
func (d *DB) Save() error {
	return d.SaveAs(d.Path)
}

// SaveAs writes the database to the given path using atomic file replacement.
// It writes to a temporary file first, then renames it to the target path
// to avoid data loss if the process is interrupted during save.
func (d *DB) SaveAs(path string) error {
	d.Database.LockProtectedEntries()
	defer d.Database.UnlockProtectedEntries()

	dir := filepath.Dir(path)
	tmpFile, err := os.CreateTemp(dir, ".keetui-save-*.tmp")
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	tmpPath := tmpFile.Name()

	// Ensure temp file is cleaned up on failure
	success := false
	defer func() {
		if !success {
			os.Remove(tmpPath)
		}
	}()

	if err := os.Chmod(tmpPath, 0600); err != nil {
		tmpFile.Close()
		return fmt.Errorf("set file permissions: %w", err)
	}

	if err := gokeepasslib.NewEncoder(tmpFile).Encode(d.Database); err != nil {
		tmpFile.Close()
		return fmt.Errorf("encode database: %w", err)
	}

	if err := tmpFile.Sync(); err != nil {
		tmpFile.Close()
		return fmt.Errorf("sync file: %w", err)
	}

	if err := tmpFile.Close(); err != nil {
		return fmt.Errorf("close temp file: %w", err)
	}

	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("rename temp file: %w", err)
	}

	// Sync the parent directory to ensure the rename is durably persisted.
	if dirFD, err := os.Open(dir); err == nil {
		_ = dirFD.Sync()
		dirFD.Close()
	}

	success = true
	return nil
}

// ChangePassword updates the master password of the database.
func (d *DB) ChangePassword(newPassword string) {
	d.Database.Credentials = gokeepasslib.NewPasswordCredentials(newPassword)
}

// AddEntry adds a new entry to the group identified by groupUUID.
// It searches recursively from the root group.
func (d *DB) AddEntry(groupUUID gokeepasslib.UUID, e Entry) error {
	if len(d.Database.Content.Root.Groups) == 0 {
		return fmt.Errorf("database has no root group")
	}
	g := findGroup(&d.Database.Content.Root.Groups[0], groupUUID)
	if g == nil {
		return fmt.Errorf("group not found")
	}
	entry := gokeepasslib.NewEntry()
	entry.Values = []gokeepasslib.ValueData{
		mkValue("Title", e.Title),
		mkValue("UserName", e.UserName),
		mkProtectedValue("Password", e.Password),
		mkValue("URL", e.URL),
		mkValue("Notes", e.Notes),
	}
	if e.TOTP != "" {
		if strings.HasPrefix(e.TOTP, "otpauth://") {
			entry.Values = append(entry.Values, mkProtectedValue("otp", e.TOTP))
		} else {
			entry.Values = append(entry.Values, mkProtectedValue("TimeOtp-Secret-Base32", e.TOTP))
		}
	}
	for _, cf := range e.CustomFields {
		if cf.Protected {
			entry.Values = append(entry.Values, mkProtectedValue(cf.Key, cf.Value))
		} else {
			entry.Values = append(entry.Values, mkValue(cf.Key, cf.Value))
		}
	}
	if e.Expires {
		entry.Times.Expires = w.NewBoolWrapper(true)
		entry.Times.ExpiryTime = &w.TimeWrapper{Time: e.ExpiryTime}
	}
	g.Entries = append(g.Entries, entry)
	return nil
}

// EntryHistory returns the password history for an entry, most recent first.
func (d *DB) EntryHistory(entryUUID gokeepasslib.UUID) []HistoryEntry {
	if len(d.Database.Content.Root.Groups) == 0 {
		return nil
	}
	entry := findEntry(&d.Database.Content.Root.Groups[0], entryUUID)
	if entry == nil {
		return nil
	}
	var history []HistoryEntry
	for _, h := range entry.Histories {
		for _, he := range h.Entries {
			var modTime time.Time
			if he.Times.LastModificationTime != nil {
				modTime = he.Times.LastModificationTime.Time
			}
			history = append(history, HistoryEntry{
				Password:         he.GetContent("Password"),
				ModificationTime: modTime,
				Title:            he.GetContent("Title"),
				UserName:         he.GetContent("UserName"),
			})
		}
	}
	// Sort most recent first
	for i, j := 0, len(history)-1; i < j; i, j = i+1, j-1 {
		history[i], history[j] = history[j], history[i]
	}
	return history
}

// UpdateEntry updates an existing entry identified by entryUUID.
func (d *DB) UpdateEntry(entryUUID gokeepasslib.UUID, e Entry) error {
	if len(d.Database.Content.Root.Groups) == 0 {
		return fmt.Errorf("database has no root group")
	}
	entry := findEntry(&d.Database.Content.Root.Groups[0], entryUUID)
	if entry == nil {
		return fmt.Errorf("entry not found")
	}

	// Save current state to history before applying changes
	historyClone := entry.Clone()
	if len(entry.Histories) == 0 {
		entry.Histories = []gokeepasslib.History{{}}
	}
	// Clear the clone's history to avoid nesting
	historyClone.Histories = nil
	entry.Histories[0].Entries = append(entry.Histories[0].Entries, historyClone)

	// Trim history to max items
	maxItems := int(d.Database.Content.Meta.HistoryMaxItems)
	if maxItems <= 0 {
		maxItems = 10
	}
	if len(entry.Histories[0].Entries) > maxItems {
		entry.Histories[0].Entries = entry.Histories[0].Entries[len(entry.Histories[0].Entries)-maxItems:]
	}

	setEntryValue(entry, "Title", e.Title, false)
	setEntryValue(entry, "UserName", e.UserName, false)
	setEntryValue(entry, "Password", e.Password, true)
	setEntryValue(entry, "URL", e.URL, false)
	setEntryValue(entry, "Notes", e.Notes, false)
	if e.TOTP != "" {
		if strings.HasPrefix(e.TOTP, "otpauth://") {
			setEntryValue(entry, "otp", e.TOTP, true)
		} else {
			setEntryValue(entry, "TimeOtp-Secret-Base32", e.TOTP, true)
		}
	}
	// Sync custom fields: remove old custom fields, add new ones
	var cleaned []gokeepasslib.ValueData
	for _, v := range entry.Values {
		if standardFields[v.Key] {
			cleaned = append(cleaned, v)
		}
	}
	entry.Values = cleaned
	for _, cf := range e.CustomFields {
		if cf.Protected {
			entry.Values = append(entry.Values, mkProtectedValue(cf.Key, cf.Value))
		} else {
			entry.Values = append(entry.Values, mkValue(cf.Key, cf.Value))
		}
	}
	if e.Expires {
		entry.Times.Expires = w.NewBoolWrapper(true)
		entry.Times.ExpiryTime = &w.TimeWrapper{Time: e.ExpiryTime}
	} else {
		entry.Times.Expires = w.NewBoolWrapper(false)
	}
	entry.Times.LastModificationTime = &w.TimeWrapper{Time: time.Now()}
	return nil
}

// RecycleBinEnabled returns whether the recycle bin is enabled in database metadata.
func (d *DB) RecycleBinEnabled() bool {
	return d.Database.Content.Meta.RecycleBinEnabled.Bool
}

// RecycleBinUUID returns the UUID of the recycle bin group.
func (d *DB) RecycleBinUUID() gokeepasslib.UUID {
	return d.Database.Content.Meta.RecycleBinUUID
}

// IsRecycleBin returns true if the group with the given UUID is the recycle bin.
func (d *DB) IsRecycleBin(groupUUID gokeepasslib.UUID) bool {
	if !d.RecycleBinEnabled() {
		return false
	}
	return d.RecycleBinUUID().Compare(groupUUID)
}

// ensureRecycleBin creates a Recycle Bin group if one doesn't exist and returns its UUID.
func (d *DB) ensureRecycleBin() gokeepasslib.UUID {
	if len(d.Database.Content.Root.Groups) == 0 {
		return gokeepasslib.UUID{}
	}
	root := &d.Database.Content.Root.Groups[0]
	rbUUID := d.RecycleBinUUID()

	// Check if the recycle bin group already exists
	if g := findGroup(root, rbUUID); g != nil {
		return rbUUID
	}

	// Create the recycle bin group
	rbGroup := gokeepasslib.NewGroup()
	rbGroup.Name = "Recycle Bin"
	root.Groups = append(root.Groups, rbGroup)

	// Set metadata
	d.Database.Content.Meta.RecycleBinEnabled = w.NewBoolWrapper(true)
	d.Database.Content.Meta.RecycleBinUUID = rbGroup.UUID
	now := w.Now()
	d.Database.Content.Meta.RecycleBinChanged = &now

	return rbGroup.UUID
}

// DeleteEntry removes an entry. If recycle bin is enabled, moves to recycle bin instead.
func (d *DB) DeleteEntry(entryUUID gokeepasslib.UUID) error {
	if len(d.Database.Content.Root.Groups) == 0 {
		return fmt.Errorf("database has no root group")
	}
	if d.RecycleBinEnabled() {
		// Check if entry is already in recycle bin
		root := &d.Database.Content.Root.Groups[0]
		rbUUID := d.RecycleBinUUID()
		rbGroup := findGroup(root, rbUUID)
		if rbGroup != nil {
			for _, e := range rbGroup.Entries {
				if e.UUID.Compare(entryUUID) {
					// Already in recycle bin, permanently delete
					return d.PermanentlyDeleteEntry(entryUUID)
				}
			}
		}
		rbUUID = d.ensureRecycleBin()
		return d.MoveEntry(entryUUID, rbUUID)
	}
	if !removeEntry(&d.Database.Content.Root.Groups[0], entryUUID) {
		return fmt.Errorf("entry not found")
	}
	return nil
}

// PermanentlyDeleteEntry removes an entry without using the recycle bin.
func (d *DB) PermanentlyDeleteEntry(entryUUID gokeepasslib.UUID) error {
	if len(d.Database.Content.Root.Groups) == 0 {
		return fmt.Errorf("database has no root group")
	}
	if !removeEntry(&d.Database.Content.Root.Groups[0], entryUUID) {
		return fmt.Errorf("entry not found")
	}
	return nil
}

// AddGroup adds a new sub-group under the group identified by parentUUID.
func (d *DB) AddGroup(parentUUID gokeepasslib.UUID, name string) error {
	if len(d.Database.Content.Root.Groups) == 0 {
		return fmt.Errorf("database has no root group")
	}
	parent := findGroup(&d.Database.Content.Root.Groups[0], parentUUID)
	if parent == nil {
		return fmt.Errorf("parent group not found")
	}
	newGroup := gokeepasslib.NewGroup()
	newGroup.Name = name
	parent.Groups = append(parent.Groups, newGroup)
	return nil
}

// MoveEntry moves an entry from its current group to the group identified by targetGroupUUID.
func (d *DB) MoveEntry(entryUUID, targetGroupUUID gokeepasslib.UUID) error {
	if len(d.Database.Content.Root.Groups) == 0 {
		return fmt.Errorf("database has no root group")
	}
	root := &d.Database.Content.Root.Groups[0]

	// Validate target group exists before extracting (to avoid data loss)
	target := findGroup(root, targetGroupUUID)
	if target == nil {
		return fmt.Errorf("target group not found")
	}

	// Now safe to remove the entry from its current group
	entry := extractEntry(root, entryUUID)
	if entry == nil {
		return fmt.Errorf("entry not found")
	}

	target.Entries = append(target.Entries, *entry)
	return nil
}

// MoveGroup moves a group from its current parent to the group identified by targetGroupUUID.
// It refuses to move a group into itself or one of its descendants.
func (d *DB) MoveGroup(groupUUID, targetGroupUUID gokeepasslib.UUID) error {
	if len(d.Database.Content.Root.Groups) == 0 {
		return fmt.Errorf("database has no root group")
	}
	root := &d.Database.Content.Root.Groups[0]

	// Cannot move a group into itself
	if groupUUID.Compare(targetGroupUUID) {
		return fmt.Errorf("cannot move a group into itself")
	}

	// Check the target exists
	target := findGroup(root, targetGroupUUID)
	if target == nil {
		return fmt.Errorf("target group not found")
	}

	// Check the source exists
	src := findGroup(root, groupUUID)
	if src == nil {
		return fmt.Errorf("group not found")
	}

	// Cannot move into a descendant of itself
	if findGroup(src, targetGroupUUID) != nil {
		return fmt.Errorf("cannot move a group into one of its descendants")
	}

	// Extract the group from its current parent
	extracted := extractGroup(root, groupUUID)
	if extracted == nil {
		return fmt.Errorf("group not found")
	}

	// Re-find target after extraction (pointers may have shifted)
	target = findGroup(root, targetGroupUUID)
	if target == nil {
		// Should not happen, but protect against it
		root.Groups = append(root.Groups, *extracted)
		return fmt.Errorf("target group lost after extraction")
	}

	target.Groups = append(target.Groups, *extracted)
	return nil
}

// DuplicateEntry creates a copy of an entry in the same group.
func (d *DB) DuplicateEntry(entryUUID gokeepasslib.UUID) error {
	if len(d.Database.Content.Root.Groups) == 0 {
		return fmt.Errorf("database has no root group")
	}
	root := &d.Database.Content.Root.Groups[0]

	// Find the entry and its parent group
	parentGroup, entryIdx := findEntryParent(root, entryUUID)
	if parentGroup == nil {
		return fmt.Errorf("entry not found")
	}

	original := parentGroup.Entries[entryIdx]

	// Create a new entry with a new UUID but same values
	dup := gokeepasslib.NewEntry()
	for _, v := range original.Values {
		if v.Key == "Title" {
			dup.Values = append(dup.Values, gokeepasslib.ValueData{
				Key:   v.Key,
				Value: gokeepasslib.V{Content: v.Value.Content + " (copy)", Protected: v.Value.Protected},
			})
		} else {
			dup.Values = append(dup.Values, gokeepasslib.ValueData{
				Key:   v.Key,
				Value: gokeepasslib.V{Content: v.Value.Content, Protected: v.Value.Protected},
			})
		}
	}
	dup.Times.CreationTime = &w.TimeWrapper{Time: time.Now()}
	dup.Times.LastModificationTime = &w.TimeWrapper{Time: time.Now()}

	// Copy binary attachment references
	for _, binRef := range original.Binaries {
		dup.Binaries = append(dup.Binaries, binRef)
	}

	parentGroup.Entries = append(parentGroup.Entries, dup)
	return nil
}

// RenameGroup changes the name of a group identified by groupUUID.
func (d *DB) RenameGroup(groupUUID gokeepasslib.UUID, newName string) error {
	if len(d.Database.Content.Root.Groups) == 0 {
		return fmt.Errorf("database has no root group")
	}
	g := findGroup(&d.Database.Content.Root.Groups[0], groupUUID)
	if g == nil {
		return fmt.Errorf("group not found")
	}
	g.Name = newName
	return nil
}

// DeleteGroup removes a sub-group. If recycle bin is enabled, moves to recycle bin instead.
func (d *DB) DeleteGroup(groupUUID gokeepasslib.UUID) error {
	if len(d.Database.Content.Root.Groups) == 0 {
		return fmt.Errorf("database has no root group")
	}
	if d.RecycleBinEnabled() {
		// Don't move the recycle bin itself
		if d.IsRecycleBin(groupUUID) {
			return fmt.Errorf("cannot delete the recycle bin group")
		}
		// Check if group is already in recycle bin
		root := &d.Database.Content.Root.Groups[0]
		rbGroup := findGroup(root, d.RecycleBinUUID())
		if rbGroup != nil {
			for i := range rbGroup.Groups {
				if rbGroup.Groups[i].UUID.Compare(groupUUID) {
					// Already in recycle bin, permanently delete
					return d.PermanentlyDeleteGroup(groupUUID)
				}
			}
		}
		rbUUID := d.ensureRecycleBin()
		return d.MoveGroup(groupUUID, rbUUID)
	}
	if !removeGroup(&d.Database.Content.Root.Groups[0], groupUUID) {
		return fmt.Errorf("group not found")
	}
	return nil
}

// PermanentlyDeleteGroup removes a group without using the recycle bin.
func (d *DB) PermanentlyDeleteGroup(groupUUID gokeepasslib.UUID) error {
	if len(d.Database.Content.Root.Groups) == 0 {
		return fmt.Errorf("database has no root group")
	}
	if !removeGroup(&d.Database.Content.Root.Groups[0], groupUUID) {
		return fmt.Errorf("group not found")
	}
	return nil
}

// RestoreEntry moves an entry from the recycle bin to the given target group.
func (d *DB) RestoreEntry(entryUUID, targetGroupUUID gokeepasslib.UUID) error {
	return d.MoveEntry(entryUUID, targetGroupUUID)
}

// RestoreGroup moves a group from the recycle bin to the given target group.
func (d *DB) RestoreGroup(groupUUID, targetGroupUUID gokeepasslib.UUID) error {
	return d.MoveGroup(groupUUID, targetGroupUUID)
}

// EmptyRecycleBin removes all entries and sub-groups from the recycle bin.
func (d *DB) EmptyRecycleBin() error {
	if len(d.Database.Content.Root.Groups) == 0 {
		return fmt.Errorf("database has no root group")
	}
	root := &d.Database.Content.Root.Groups[0]
	rbGroup := findGroup(root, d.RecycleBinUUID())
	if rbGroup == nil {
		return fmt.Errorf("recycle bin not found")
	}
	rbGroup.Entries = nil
	rbGroup.Groups = nil
	return nil
}

func findGroup(g *gokeepasslib.Group, uuid gokeepasslib.UUID) *gokeepasslib.Group {
	if g.UUID.Compare(uuid) {
		return g
	}
	for i := range g.Groups {
		if found := findGroup(&g.Groups[i], uuid); found != nil {
			return found
		}
	}
	return nil
}

func findEntry(g *gokeepasslib.Group, uuid gokeepasslib.UUID) *gokeepasslib.Entry {
	for i := range g.Entries {
		if g.Entries[i].UUID.Compare(uuid) {
			return &g.Entries[i]
		}
	}
	for i := range g.Groups {
		if found := findEntry(&g.Groups[i], uuid); found != nil {
			return found
		}
	}
	return nil
}

func removeEntry(g *gokeepasslib.Group, uuid gokeepasslib.UUID) bool {
	for i := range g.Entries {
		if g.Entries[i].UUID.Compare(uuid) {
			g.Entries = append(g.Entries[:i], g.Entries[i+1:]...)
			return true
		}
	}
	for i := range g.Groups {
		if removeEntry(&g.Groups[i], uuid) {
			return true
		}
	}
	return false
}

// extractGroup removes and returns a sub-group from the tree, or nil if not found.
// It does not extract the root group itself.
func extractGroup(g *gokeepasslib.Group, uuid gokeepasslib.UUID) *gokeepasslib.Group {
	for i := range g.Groups {
		if g.Groups[i].UUID.Compare(uuid) {
			grp := g.Groups[i]
			g.Groups = append(g.Groups[:i], g.Groups[i+1:]...)
			return &grp
		}
	}
	for i := range g.Groups {
		if found := extractGroup(&g.Groups[i], uuid); found != nil {
			return found
		}
	}
	return nil
}

// extractEntry removes and returns an entry from the tree, or nil if not found.
func extractEntry(g *gokeepasslib.Group, uuid gokeepasslib.UUID) *gokeepasslib.Entry {
	for i := range g.Entries {
		if g.Entries[i].UUID.Compare(uuid) {
			entry := g.Entries[i]
			g.Entries = append(g.Entries[:i], g.Entries[i+1:]...)
			return &entry
		}
	}
	for i := range g.Groups {
		if found := extractEntry(&g.Groups[i], uuid); found != nil {
			return found
		}
	}
	return nil
}

// findEntryParent returns the parent group and index of the entry.
func findEntryParent(g *gokeepasslib.Group, uuid gokeepasslib.UUID) (*gokeepasslib.Group, int) {
	for i := range g.Entries {
		if g.Entries[i].UUID.Compare(uuid) {
			return g, i
		}
	}
	for i := range g.Groups {
		if parent, idx := findEntryParent(&g.Groups[i], uuid); parent != nil {
			return parent, idx
		}
	}
	return nil, -1
}

func removeGroup(g *gokeepasslib.Group, uuid gokeepasslib.UUID) bool {
	for i := range g.Groups {
		if g.Groups[i].UUID.Compare(uuid) {
			g.Groups = append(g.Groups[:i], g.Groups[i+1:]...)
			return true
		}
	}
	for i := range g.Groups {
		if removeGroup(&g.Groups[i], uuid) {
			return true
		}
	}
	return false
}

func setEntryValue(entry *gokeepasslib.Entry, key, value string, protected bool) {
	if v := entry.Get(key); v != nil {
		v.Value.Content = value
		return
	}
	if protected {
		entry.Values = append(entry.Values, mkProtectedValue(key, value))
	} else {
		entry.Values = append(entry.Values, mkValue(key, value))
	}
}

// extractTOTP reads the TOTP value from an entry.
// It supports two conventions:
//   - KeePassXC: "otp" field containing an otpauth:// URI
//   - KeePass2: "TimeOtp-Secret-Base32" field containing a raw base32 secret
func extractTOTP(e gokeepasslib.Entry) string {
	// KeePassXC style: otp field with otpauth:// URI
	if otp := e.GetContent("otp"); otp != "" {
		return otp
	}
	// KeePass2 style: TimeOtp-Secret-Base32
	if secret := e.GetContent("TimeOtp-Secret-Base32"); secret != "" {
		return secret
	}
	return ""
}

// TOTPSecret extracts the base32 secret from a TOTP value.
// The value may be a raw base32 string or an otpauth:// URI.
func TOTPSecret(totp string) string {
	if totp == "" {
		return ""
	}
	if strings.HasPrefix(totp, "otpauth://") {
		u, err := url.Parse(totp)
		if err != nil {
			return ""
		}
		return u.Query().Get("secret")
	}
	return totp
}

// TOTPPeriod extracts the period (in seconds) from a TOTP value.
// Returns 30 as default if not specified.
func TOTPPeriod(totp string) uint {
	if strings.HasPrefix(totp, "otpauth://") {
		u, err := url.Parse(totp)
		if err != nil {
			return 30
		}
		if p := u.Query().Get("period"); p != "" {
			var period uint
			if _, err := fmt.Sscanf(p, "%d", &period); err == nil && period > 0 {
				return period
			}
		}
	}
	return 30
}

// standardFields are the built-in KeePass fields that are handled separately.
var standardFields = map[string]bool{
	"Title":                 true,
	"UserName":              true,
	"Password":              true,
	"URL":                   true,
	"Notes":                 true,
	"otp":                   true,
	"TimeOtp-Secret-Base32": true,
}

func extractCustomFields(e gokeepasslib.Entry) []CustomField {
	var fields []CustomField
	for _, v := range e.Values {
		if standardFields[v.Key] {
			continue
		}
		fields = append(fields, CustomField{
			Key:       v.Key,
			Value:     v.Value.Content,
			Protected: v.Value.Protected.Bool,
		})
	}
	return fields
}

func checkFileExtension(path string) error {
	if strings.HasSuffix(strings.ToLower(path), ".kdb") {
		return fmt.Errorf("KeePass 1.x .kdb files are not supported; please convert to .kdbx format using KeePass 2 or KeePassXC")
	}
	return nil
}

func mkValue(key, value string) gokeepasslib.ValueData {
	return gokeepasslib.ValueData{
		Key:   key,
		Value: gokeepasslib.V{Content: value},
	}
}

func mkProtectedValue(key, value string) gokeepasslib.ValueData {
	return gokeepasslib.ValueData{
		Key:   key,
		Value: gokeepasslib.V{Content: value, Protected: w.NewBoolWrapper(true)},
	}
}
