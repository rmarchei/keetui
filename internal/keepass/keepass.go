// Package keepass provides operations for reading and writing KeePass .kdbx files.
package keepass

import (
	"fmt"
	"os"
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

// Entry is a simplified view of a KeePass entry.
type Entry struct {
	UUID     gokeepasslib.UUID
	Title    string
	UserName string
	Password string
	URL      string
	Notes    string
}

// Group is a simplified view of a KeePass group.
type Group struct {
	UUID    gokeepasslib.UUID
	Name    string
	Groups  []Group
	Entries []Entry
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
		group.Entries = append(group.Entries, Entry{
			UUID:     e.UUID,
			Title:    e.GetContent("Title"),
			UserName: e.GetContent("UserName"),
			Password: e.GetContent("Password"),
			URL:      e.GetContent("URL"),
			Notes:    e.GetContent("Notes"),
		})
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
