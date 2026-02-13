// Package keepass provides operations for reading and writing KeePass .kdbx files.
package keepass

import (
	"fmt"
	"net/url"
	"os"
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
	UUID         gokeepasslib.UUID
	Title        string
	UserName     string
	Password     string
	URL          string
	Notes        string
	TOTP         string // TOTP secret (base32) or otpauth:// URI
	CustomFields []CustomField
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

	file, err := os.Create(path)
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
		entry.CustomFields = extractCustomFields(e)
		group.Entries = append(group.Entries, entry)
	}
	return group
}

// Save writes the database back to its file.
func (d *DB) Save() error {
	return d.SaveAs(d.Path)
}

// SaveAs writes the database to the given path.
func (d *DB) SaveAs(path string) error {
	d.Database.LockProtectedEntries()
	defer d.Database.UnlockProtectedEntries()

	file, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("create file: %w", err)
	}
	defer file.Close()

	if err := gokeepasslib.NewEncoder(file).Encode(d.Database); err != nil {
		return fmt.Errorf("encode database: %w", err)
	}
	return nil
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
	g.Entries = append(g.Entries, entry)
	return nil
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
	entry.Times.LastModificationTime = &w.TimeWrapper{Time: time.Now()}
	return nil
}

// DeleteEntry removes an entry identified by entryUUID from its parent group.
func (d *DB) DeleteEntry(entryUUID gokeepasslib.UUID) error {
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

	// Find and remove the entry from its current group
	entry := extractEntry(root, entryUUID)
	if entry == nil {
		return fmt.Errorf("entry not found")
	}

	// Find the target group and add the entry
	target := findGroup(root, targetGroupUUID)
	if target == nil {
		return fmt.Errorf("target group not found")
	}
	target.Entries = append(target.Entries, *entry)
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

// DeleteGroup removes a sub-group identified by groupUUID.
func (d *DB) DeleteGroup(groupUUID gokeepasslib.UUID) error {
	if len(d.Database.Content.Root.Groups) == 0 {
		return fmt.Errorf("database has no root group")
	}
	if !removeGroup(&d.Database.Content.Root.Groups[0], groupUUID) {
		return fmt.Errorf("group not found")
	}
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
	"Title":                true,
	"UserName":             true,
	"Password":             true,
	"URL":                  true,
	"Notes":                true,
	"otp":                  true,
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
