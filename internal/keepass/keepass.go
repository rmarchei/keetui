// Package keepass provides operations for reading and writing KeePass .kdbx files.
package keepass

import (
	"crypto/rand"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unicode/utf8"

	"github.com/tobischo/gokeepasslib/v3"
	w "github.com/tobischo/gokeepasslib/v3/wrappers"
)

// DB wraps a gokeepasslib.Database with file path and credentials info.
type DB struct {
	Path         string
	Database     *gokeepasslib.Database
	BackupOnSave bool   // when true, create a .bak copy before saving
	keyFilePath  string // key file used to open the database, if any
	unlocked     bool
	// suspectCorruption is set when decoded protected values look like
	// binary garbage, i.e. the inner stream cipher was misaligned while
	// decoding (e.g. a format feature the KeePass library does not handle).
	suspectCorruption bool
}

// SuspectCorruption reports whether the database's protected values looked
// corrupted after unlocking. Callers should treat the database as read-only
// in that case: displayed/copied secrets are likely wrong, and saving would
// persist the garbage.
func (d *DB) SuspectCorruption() bool {
	return d.suspectCorruption
}

// corruptionThreshold is the number of non-empty protected values with
// invalid UTF-8 required before a database is flagged as suspect. KeePass
// tools treat protected values as text, so several binary-looking values
// indicate a misaligned stream cipher rather than genuine data.
const corruptionThreshold = 2

// detectCorruptedProtectedValues scans all protected values (including
// history entries) of an unlocked database and reports whether at least
// corruptionThreshold non-empty values are not valid UTF-8.
func detectCorruptedProtectedValues(db *gokeepasslib.Database) bool {
	if db.Content == nil || db.Content.Root == nil {
		return false
	}

	count := 0

	var scanEntries func(entries []gokeepasslib.Entry) bool
	scanEntries = func(entries []gokeepasslib.Entry) bool {
		for i := range entries {
			e := &entries[i]
			for j := range e.Values {
				v := &e.Values[j]
				if v.Value.Protected.Bool && v.Value.Content != "" && !utf8.ValidString(v.Value.Content) {
					count++
					if count >= corruptionThreshold {
						return true
					}
				}
			}
			for k := range e.Histories {
				if scanEntries(e.Histories[k].Entries) {
					return true
				}
			}
		}
		return false
	}

	var scanGroups func(groups []gokeepasslib.Group) bool
	scanGroups = func(groups []gokeepasslib.Group) bool {
		for i := range groups {
			if scanEntries(groups[i].Entries) {
				return true
			}
			if scanGroups(groups[i].Groups) {
				return true
			}
		}
		return false
	}

	return scanGroups(db.Content.Root.Groups)
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
	Tags             string
}

// IsExpired returns true if the entry has an expiry date set and it is in the past.
func (e Entry) IsExpired() bool {
	return e.Expires && !e.ExpiryTime.IsZero() && time.Now().After(e.ExpiryTime)
}

// IsFavorite returns true if the entry has the "favorite" tag.
func (e Entry) IsFavorite() bool {
	for tag := range strings.SplitSeq(e.Tags, ";") {
		if strings.TrimSpace(tag) == "favorite" {
			return true
		}
	}
	return false
}

// customField returns the value of the named custom field, or "".
func (e Entry) customField(key string) string {
	for _, cf := range e.CustomFields {
		if cf.Key == key {
			return cf.Value
		}
	}
	return ""
}

// totpURIParam returns a query parameter from the entry's otpauth:// URI,
// or "" when the TOTP value is not a valid URI.
func (e Entry) totpURIParam(name string) string {
	if !strings.HasPrefix(e.TOTP, "otpauth://") {
		return ""
	}
	u, err := url.Parse(e.TOTP)
	if err != nil {
		return ""
	}
	return u.Query().Get(name)
}

// TOTPDigits returns the number of digits for the entry's TOTP codes,
// honoring the otpauth URI digits= parameter and the KeePass2
// TimeOtp-Length field. Defaults to 6 when unspecified or out of range.
func (e Entry) TOTPDigits() int {
	v := e.totpURIParam("digits")
	if v == "" {
		v = e.customField("TimeOtp-Length")
	}
	if n, err := strconv.Atoi(strings.TrimSpace(v)); err == nil && n >= 4 && n <= 10 {
		return n
	}
	return 6
}

// TOTPAlgorithm returns the normalized TOTP hash algorithm ("SHA1",
// "SHA256", or "SHA512"), honoring the otpauth URI algorithm= parameter
// and the KeePass2 TimeOtp-Algorithm field (e.g. "HMAC-SHA-256").
// Defaults to SHA1.
func (e Entry) TOTPAlgorithm() string {
	raw := e.totpURIParam("algorithm")
	if raw == "" {
		raw = e.customField("TimeOtp-Algorithm")
	}
	s := strings.ToUpper(strings.TrimSpace(raw))
	s = strings.TrimPrefix(s, "HMAC-")
	s = strings.ReplaceAll(s, "-", "")
	switch s {
	case "SHA256", "SHA512":
		return s
	default:
		return "SHA1"
	}
}

// TOTPPeriod returns the code period in seconds for the entry's TOTP,
// honoring the otpauth URI period= parameter and the KeePass2
// TimeOtp-Period field. Defaults to 30.
func (e Entry) TOTPPeriod() uint {
	if strings.HasPrefix(e.TOTP, "otpauth://") {
		return TOTPPeriod(e.TOTP)
	}
	if n, err := strconv.Atoi(strings.TrimSpace(e.customField("TimeOtp-Period"))); err == nil && n > 0 {
		return uint(n)
	}
	return 30
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

// buildCredentials assembles database credentials from a master password
// and an optional key file. An empty password combined with a key file
// yields key-only credentials (keyfile-only databases, as supported by
// KeePass2/KeePassXC).
func buildCredentials(password, keyFilePath string) (*gokeepasslib.DBCredentials, error) {
	switch {
	case keyFilePath == "":
		return gokeepasslib.NewPasswordCredentials(password), nil
	case password == "":
		return gokeepasslib.NewKeyCredentials(keyFilePath)
	default:
		return gokeepasslib.NewPasswordAndKeyCredentials(password, keyFilePath)
	}
}

// Create creates a new .kdbx database file with the given password.
func Create(path, password string) (*DB, error) {
	return CreateWithKeyFile(path, password, "")
}

// CreateWithKeyFile creates a new .kdbx database file protected by the
// given password and/or key file.
func CreateWithKeyFile(path, password, keyFilePath string) (*DB, error) {
	creds, err := buildCredentials(password, keyFilePath)
	if err != nil {
		return nil, fmt.Errorf("create credentials: %w", err)
	}

	rootGroup := gokeepasslib.NewGroup()
	rootGroup.Name = "Root"

	db := gokeepasslib.NewDatabase()
	db.Credentials = creds
	db.Content.Root = &gokeepasslib.RootData{
		Groups: []gokeepasslib.Group{rootGroup},
	}

	_ = db.LockProtectedEntries()

	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return nil, fmt.Errorf("create file: %w", err)
	}
	defer func() { _ = file.Close() }()

	if err := gokeepasslib.NewEncoder(file).Encode(db); err != nil {
		return nil, fmt.Errorf("encode database: %w", err)
	}

	if err := db.UnlockProtectedEntries(); err != nil {
		return nil, fmt.Errorf("unlock protected values: %w", err)
	}

	return &DB{
		Path:              path,
		Database:          db,
		keyFilePath:       keyFilePath,
		unlocked:          true,
		suspectCorruption: detectCorruptedProtectedValues(db),
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
	defer func() { _ = file.Close() }()

	db := gokeepasslib.NewDatabase()
	db.Credentials = gokeepasslib.NewPasswordCredentials(password)

	if err := gokeepasslib.NewDecoder(file).Decode(db); err != nil {
		return nil, fmt.Errorf("decode database: %w", err)
	}

	// Propagate unlock failures (e.g. unsupported inner stream ciphers):
	// otherwise every protected value would display as ciphertext garbage.
	if err := db.UnlockProtectedEntries(); err != nil {
		return nil, fmt.Errorf("unlock protected values: %w", err)
	}

	return &DB{
		Path:              path,
		Database:          db,
		unlocked:          true,
		suspectCorruption: detectCorruptedProtectedValues(db),
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
	defer func() { _ = file.Close() }()

	creds, err := buildCredentials(password, keyFilePath)
	if err != nil {
		return nil, fmt.Errorf("create credentials: %w", err)
	}

	db := gokeepasslib.NewDatabase()
	db.Credentials = creds

	if err := gokeepasslib.NewDecoder(file).Decode(db); err != nil {
		return nil, fmt.Errorf("decode database: %w", err)
	}

	if err := db.UnlockProtectedEntries(); err != nil {
		return nil, fmt.Errorf("unlock protected values: %w", err)
	}

	return &DB{
		Path:              path,
		Database:          db,
		keyFilePath:       keyFilePath,
		unlocked:          true,
		suspectCorruption: detectCorruptedProtectedValues(db),
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
		entry.Tags = e.Tags
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
	// Follow a symlinked database path so the final rename lands on the
	// link's target instead of replacing the link with a regular file
	// (which would silently fork the database from its real location).
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		path = resolved
	}

	// For KDBX 4, binaries live in the InnerHeader (raw bytes).
	// Some tools also write them into the XML <Meta><Binaries> section.
	// gokeepasslib's cleanupBinaries only cleans the "active" location,
	// so stale Meta.Binaries would be re-serialised as base64 in the XML,
	// duplicating every attachment and inflating the file.
	if d.Database.Header.IsKdbx4() {
		d.Database.Content.Meta.Binaries = nil
	}

	// Refresh before locking: the inner stream key must not change while
	// protected values are masked with it.
	if err := refreshRandomHeaderFields(d.Database); err != nil {
		return fmt.Errorf("refresh header randomness: %w", err)
	}

	// Lock before encoding: the encoder's internal Unlock→Lock cycle assumes
	// entries are locked. Since we keep them unlocked in memory, we must lock
	// first so the encoder's Unlock correctly decrypts them.
	_ = d.Database.LockProtectedEntries()
	defer func() { _ = d.Database.UnlockProtectedEntries() }()

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
			_ = os.Remove(tmpPath)
		}
	}()

	// os.CreateTemp already creates with mode 0600 (O_EXCL ensures no race).

	if err := gokeepasslib.NewEncoder(tmpFile).Encode(d.Database); err != nil {
		_ = tmpFile.Close()
		return fmt.Errorf("encode database: %w", err)
	}

	if err := tmpFile.Sync(); err != nil {
		_ = tmpFile.Close()
		return fmt.Errorf("sync file: %w", err)
	}

	if err := tmpFile.Close(); err != nil {
		return fmt.Errorf("close temp file: %w", err)
	}

	if d.BackupOnSave {
		if err := createBackup(path); err != nil {
			return fmt.Errorf("create backup: %w", err)
		}
	}

	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("rename temp file: %w", err)
	}

	// Sync the parent directory to ensure the rename is durably persisted.
	if dirFD, err := os.Open(dir); err == nil {
		_ = dirFD.Sync()
		_ = dirFD.Close()
	}

	success = true
	return nil
}

// refreshRandomHeaderFields draws new values for the random fields of the
// database headers, as KeePass does on every save: master seed, encryption IV,
// KDF salt (KDBX 4) or transform seed (KDBX 3.1), inner random stream key and,
// for KDBX 3.1, stream start bytes. gokeepasslib only generates them along with
// a new header, so every save would otherwise reuse the header's values, such
// as those read from the file: two versions of the file (e.g. the database and
// its .bak) would then share a ChaCha20 keystream or a CBC-encrypted prefix.
// Each value keeps its length, and the KDF cost parameters stay unchanged.
//
// Locked protected values are masked with the inner stream key, so this may
// only be called while they are unlocked.
func refreshRandomHeaderFields(db *gokeepasslib.Database) error {
	fh := db.Header.FileHeaders
	fields := [][]byte{fh.MasterSeed, fh.EncryptionIV}
	if db.Header.IsKdbx4() {
		if fh.KdfParameters == nil || db.Content.InnerHeader == nil {
			return fmt.Errorf("KDBX 4 database without KDF parameters or inner header")
		}
		fields = append(fields, fh.KdfParameters.Salt[:], db.Content.InnerHeader.InnerRandomStreamKey)
	} else {
		fields = append(fields, fh.TransformSeed, fh.ProtectedStreamKey, fh.StreamStartBytes)
	}
	for _, field := range fields {
		// Refill in place to keep the length. crypto/rand.Read cannot fail:
		// it aborts the program instead of returning an error.
		_, _ = rand.Read(field)
	}
	return nil
}

// createBackup copies the original file to path + ".bak" before overwriting.
// If the file doesn't exist (new database), it does nothing.
func createBackup(path string) error {
	src, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("open source: %w", err)
	}
	defer func() { _ = src.Close() }()

	bakPath := path + ".bak"

	// Refuse to write through a symlink to prevent an attacker from
	// tricking us into overwriting an arbitrary file with DB content.
	if fi, err := os.Lstat(bakPath); err == nil {
		if fi.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("backup path is a symlink, refusing to overwrite: %s", bakPath)
		}
	}

	// O_NOFOLLOW prevents following a symlink that appears between Lstat and Open (TOCTOU).
	dst, err := os.OpenFile(bakPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC|syscall.O_NOFOLLOW, 0600)
	if err != nil {
		return fmt.Errorf("create backup file: %w", err)
	}
	defer func() { _ = dst.Close() }()

	if _, err := io.Copy(dst, src); err != nil {
		return fmt.Errorf("copy to backup: %w", err)
	}
	if err := dst.Sync(); err != nil {
		return fmt.Errorf("sync backup: %w", err)
	}
	return nil
}

// IsKdbx4 returns true if the database is in KDBX 4 format.
func (d *DB) IsKdbx4() bool {
	return d.Database.Header.IsKdbx4()
}

// FormatVersion returns a human-readable string describing the database format.
func (d *DB) FormatVersion() string {
	if d.Database.Header.IsKdbx4() {
		return "KDBX 4 (Argon2)"
	}
	return "KDBX 3.1 (AES)"
}

// ConvertToKDBX4 upgrades a KDBX 3.1 database to KDBX 4 format in memory.
// The caller must save the database afterward to persist the change.
func (d *DB) ConvertToKDBX4() error {
	if d.Database.Header.IsKdbx4() {
		return fmt.Errorf("database is already KDBX 4 format")
	}

	// Extract raw binary contents before changing format, keeping the old
	// pool IDs so entry references can be remapped afterward.
	// KDBX 3.1 stores binaries as base64+gzip in Meta.Binaries;
	// KDBX 4 stores them as raw bytes in InnerHeader.Binaries.
	type oldBinary struct {
		id      int
		content []byte
	}
	var oldBinaries []oldBinary
	for _, b := range d.Database.Content.Meta.Binaries {
		content, err := b.GetContentBytes()
		if err != nil {
			return fmt.Errorf("read binary %d: %w", b.ID, err)
		}
		oldBinaries = append(oldBinaries, oldBinary{id: b.ID, content: content})
	}

	// Switch to KDBX 4: replaces header (Argon2, ChaCha20) and
	// creates InnerHeader with ChaCha20 random stream key.
	gokeepasslib.WithDatabaseKDBXVersion40()(d.Database)

	// Re-add binaries — AddBinary now routes to InnerHeader for KDBX 4 and
	// assigns fresh sequential IDs (deduplicating identical contents).
	idMap := make(map[int]int, len(oldBinaries))
	for _, ob := range oldBinaries {
		nb := d.Database.AddBinary(ob.content)
		idMap[ob.id] = nb.ID
	}

	// Rewrite entry attachment references (including history snapshots) from
	// old pool IDs to the freshly assigned ones.
	if root := d.Database.Content.Root; root != nil && len(root.Groups) > 0 {
		remapGroupBinaryIDs(&root.Groups[0], idMap)
	}

	// Clear stale Meta.Binaries to avoid duplication on save.
	d.Database.Content.Meta.Binaries = nil

	return nil
}

// remapGroupBinaryIDs rewrites every BinaryReference ID in the group subtree
// through idMap. IDs not present in idMap (already-dangling references) are
// left unchanged.
func remapGroupBinaryIDs(g *gokeepasslib.Group, idMap map[int]int) {
	for i := range g.Entries {
		remapEntryBinaryIDs(&g.Entries[i], idMap)
	}
	for i := range g.Groups {
		remapGroupBinaryIDs(&g.Groups[i], idMap)
	}
}

func remapEntryBinaryIDs(e *gokeepasslib.Entry, idMap map[int]int) {
	for i := range e.Binaries {
		if newID, ok := idMap[e.Binaries[i].Value.ID]; ok {
			e.Binaries[i].Value.ID = newID
		}
	}
	for hi := range e.Histories {
		for ei := range e.Histories[hi].Entries {
			remapEntryBinaryIDs(&e.Histories[hi].Entries[ei], idMap)
		}
	}
}

// ChangePassword updates the master password of the database, preserving
// the key file factor when the database was opened with one.
func (d *DB) ChangePassword(newPassword string) error {
	creds, err := buildCredentials(newPassword, d.keyFilePath)
	if err != nil {
		return fmt.Errorf("create credentials: %w", err)
	}
	d.Database.Credentials = creds
	return nil
}

// AddEntry adds a new entry to the group identified by groupUUID.
// It searches recursively from the root group. Returns the UUID of the new entry.
func (d *DB) AddEntry(groupUUID gokeepasslib.UUID, e Entry) (gokeepasslib.UUID, error) {
	if len(d.Database.Content.Root.Groups) == 0 {
		return gokeepasslib.UUID{}, fmt.Errorf("database has no root group")
	}
	g := findGroup(&d.Database.Content.Root.Groups[0], groupUUID)
	if g == nil {
		return gokeepasslib.UUID{}, fmt.Errorf("group not found")
	}
	entry := gokeepasslib.NewEntry()
	entry.Values = []gokeepasslib.ValueData{
		mkValue("Title", e.Title),
		mkValue("UserName", e.UserName),
		mkProtectedValue("Password", e.Password),
		mkValue("URL", e.URL),
		mkValue("Notes", e.Notes),
	}
	entry.Tags = e.Tags
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
	return entry.UUID, nil
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

	// Save current state to history before applying changes.
	// Meta.HistoryMaxItems follows KeePass semantics: negative = unlimited,
	// 0 = keep no history. cloneEntry (not Clone) keeps the entry's UUID,
	// as the KDBX format requires for history records.
	maxItems := int(d.Database.Content.Meta.HistoryMaxItems)
	if maxItems != 0 {
		historyClone := cloneEntry(*entry)
		if len(entry.Histories) == 0 {
			entry.Histories = []gokeepasslib.History{{}}
		}
		// Clear the clone's history to avoid nesting
		historyClone.Histories = nil
		entry.Histories[0].Entries = append(entry.Histories[0].Entries, historyClone)

		if maxItems > 0 && len(entry.Histories[0].Entries) > maxItems {
			entry.Histories[0].Entries = entry.Histories[0].Entries[len(entry.Histories[0].Entries)-maxItems:]
		}
	}

	setEntryValue(entry, "Title", e.Title, false)
	setEntryValue(entry, "UserName", e.UserName, false)
	setEntryValue(entry, "Password", e.Password, true)
	setEntryValue(entry, "URL", e.URL, false)
	setEntryValue(entry, "Notes", e.Notes, false)
	// TOTP lives in one of two fields ("otp" for otpauth:// URIs,
	// "TimeOtp-Secret-Base32" for raw secrets). Remove whichever
	// representation is not being written so a stale value can never
	// shadow the new one, and remove both when the TOTP is cleared.
	switch {
	case e.TOTP == "":
		entry.Values = removeValues(entry.Values, "otp", "TimeOtp-Secret-Base32")
	case strings.HasPrefix(e.TOTP, "otpauth://"):
		entry.Values = removeValues(entry.Values, "TimeOtp-Secret-Base32")
		setEntryValue(entry, "otp", e.TOTP, true)
	default:
		entry.Values = removeValues(entry.Values, "otp")
		setEntryValue(entry, "TimeOtp-Secret-Base32", e.TOTP, true)
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
	entry.Tags = e.Tags
	entry.Times.LastModificationTime = &w.TimeWrapper{Time: time.Now()}
	return nil
}

// ToggleFavorite adds or removes the "favorite" tag on an entry.
func (d *DB) ToggleFavorite(entryUUID gokeepasslib.UUID) error {
	if len(d.Database.Content.Root.Groups) == 0 {
		return fmt.Errorf("database has no root group")
	}
	entry := findEntry(&d.Database.Content.Root.Groups[0], entryUUID)
	if entry == nil {
		return fmt.Errorf("entry not found")
	}
	var tags []string
	for t := range strings.SplitSeq(entry.Tags, ";") {
		t = strings.TrimSpace(t)
		if t != "" && t != "favorite" {
			tags = append(tags, t)
		}
	}
	// Check if it was a favorite before
	isFav := false
	for t := range strings.SplitSeq(entry.Tags, ";") {
		if strings.TrimSpace(t) == "favorite" {
			isFav = true
			break
		}
	}
	if !isFav {
		tags = append(tags, "favorite")
	}
	entry.Tags = strings.Join(tags, ";")
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
// Returns the UUID of the new group.
func (d *DB) AddGroup(parentUUID gokeepasslib.UUID, name string) (gokeepasslib.UUID, error) {
	if len(d.Database.Content.Root.Groups) == 0 {
		return gokeepasslib.UUID{}, fmt.Errorf("database has no root group")
	}
	parent := findGroup(&d.Database.Content.Root.Groups[0], parentUUID)
	if parent == nil {
		return gokeepasslib.UUID{}, fmt.Errorf("parent group not found")
	}
	newGroup := gokeepasslib.NewGroup()
	newGroup.Name = name
	parent.Groups = append(parent.Groups, newGroup)
	return newGroup.UUID, nil
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
// Returns the UUID of the new entry.
func (d *DB) DuplicateEntry(entryUUID gokeepasslib.UUID) (gokeepasslib.UUID, error) {
	if len(d.Database.Content.Root.Groups) == 0 {
		return gokeepasslib.UUID{}, fmt.Errorf("database has no root group")
	}
	root := &d.Database.Content.Root.Groups[0]

	// Find the entry and its parent group
	parentGroup, entryIdx := findEntryParent(root, entryUUID)
	if parentGroup == nil {
		return gokeepasslib.UUID{}, fmt.Errorf("entry not found")
	}

	original := parentGroup.Entries[entryIdx]

	// Deep-copy the whole entry (values, tags, expiry, icon, colors,
	// auto-type, custom data, attachment references), then give the copy
	// its own identity: fresh UUID, no history, retitled, new timestamps.
	dup := cloneEntry(original)
	dup.UUID = gokeepasslib.NewUUID()
	dup.Histories = nil
	setEntryValue(&dup, "Title", original.GetTitle()+" (copy)", false)
	now := time.Now()
	dup.Times.CreationTime = &w.TimeWrapper{Time: now}
	dup.Times.LastModificationTime = &w.TimeWrapper{Time: now}

	parentGroup.Entries = append(parentGroup.Entries, dup)
	return dup.UUID, nil
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

// removeValues returns values with all entries matching the given keys removed.
func removeValues(values []gokeepasslib.ValueData, keys ...string) []gokeepasslib.ValueData {
	remove := make(map[string]bool, len(keys))
	for _, k := range keys {
		remove[k] = true
	}
	kept := values[:0]
	for _, v := range values {
		if !remove[v.Key] {
			kept = append(kept, v)
		}
	}
	return kept
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
// Spaces and hyphens are stripped since base32 secrets are often
// displayed with separators for readability.
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
	s := strings.ReplaceAll(totp, " ", "")
	s = strings.ReplaceAll(s, "-", "")
	return s
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

// FindEntryParentUUID returns the UUID of the group containing the given entry.
func (d *DB) FindEntryParentUUID(entryUUID gokeepasslib.UUID) (gokeepasslib.UUID, error) {
	if len(d.Database.Content.Root.Groups) == 0 {
		return gokeepasslib.UUID{}, fmt.Errorf("database has no root group")
	}
	parent, _ := findEntryParent(&d.Database.Content.Root.Groups[0], entryUUID)
	if parent == nil {
		return gokeepasslib.UUID{}, fmt.Errorf("entry not found")
	}
	return parent.UUID, nil
}

// FindGroupParentUUID returns the UUID of the parent of the given group.
func (d *DB) FindGroupParentUUID(groupUUID gokeepasslib.UUID) (gokeepasslib.UUID, error) {
	if len(d.Database.Content.Root.Groups) == 0 {
		return gokeepasslib.UUID{}, fmt.Errorf("database has no root group")
	}
	parent := findGroupParent(&d.Database.Content.Root.Groups[0], groupUUID)
	if parent == nil {
		return gokeepasslib.UUID{}, fmt.Errorf("group not found or is root")
	}
	return parent.UUID, nil
}

// GetRawEntry returns a deep copy of the raw gokeepasslib entry, preserving its UUID.
func (d *DB) GetRawEntry(entryUUID gokeepasslib.UUID) (*gokeepasslib.Entry, error) {
	if len(d.Database.Content.Root.Groups) == 0 {
		return nil, fmt.Errorf("database has no root group")
	}
	entry := findEntry(&d.Database.Content.Root.Groups[0], entryUUID)
	if entry == nil {
		return nil, fmt.Errorf("entry not found")
	}
	clone := cloneEntry(*entry)
	return &clone, nil
}

// GetRawGroup returns a deep copy of the raw gokeepasslib group.
func (d *DB) GetRawGroup(groupUUID gokeepasslib.UUID) (*gokeepasslib.Group, error) {
	if len(d.Database.Content.Root.Groups) == 0 {
		return nil, fmt.Errorf("database has no root group")
	}
	g := findGroup(&d.Database.Content.Root.Groups[0], groupUUID)
	if g == nil {
		return nil, fmt.Errorf("group not found")
	}
	clone := cloneGroup(*g)
	return &clone, nil
}

// InsertRawEntry inserts a raw entry (preserving its UUID) into the given group.
func (d *DB) InsertRawEntry(groupUUID gokeepasslib.UUID, entry gokeepasslib.Entry) error {
	if len(d.Database.Content.Root.Groups) == 0 {
		return fmt.Errorf("database has no root group")
	}
	g := findGroup(&d.Database.Content.Root.Groups[0], groupUUID)
	if g == nil {
		return fmt.Errorf("group not found")
	}
	g.Entries = append(g.Entries, entry)
	return nil
}

// InsertRawGroup inserts a raw group (preserving its UUID) into the given parent group.
func (d *DB) InsertRawGroup(parentUUID gokeepasslib.UUID, group gokeepasslib.Group) error {
	if len(d.Database.Content.Root.Groups) == 0 {
		return fmt.Errorf("database has no root group")
	}
	parent := findGroup(&d.Database.Content.Root.Groups[0], parentUUID)
	if parent == nil {
		return fmt.Errorf("parent group not found")
	}
	parent.Groups = append(parent.Groups, group)
	return nil
}

// GetEntry returns a simplified Entry for the given UUID.
func (d *DB) GetEntry(entryUUID gokeepasslib.UUID) (Entry, error) {
	if len(d.Database.Content.Root.Groups) == 0 {
		return Entry{}, fmt.Errorf("database has no root group")
	}
	raw := findEntry(&d.Database.Content.Root.Groups[0], entryUUID)
	if raw == nil {
		return Entry{}, fmt.Errorf("entry not found")
	}
	entry := Entry{
		UUID:     raw.UUID,
		Title:    raw.GetContent("Title"),
		UserName: raw.GetContent("UserName"),
		Password: raw.GetContent("Password"),
		URL:      raw.GetContent("URL"),
		Notes:    raw.GetContent("Notes"),
		TOTP:     extractTOTP(*raw),
		Tags:     raw.Tags,
		Expires:  raw.Times.Expires.Bool,
	}
	if raw.Times.CreationTime != nil {
		entry.CreationTime = raw.Times.CreationTime.Time
	}
	if raw.Times.LastModificationTime != nil {
		entry.ModificationTime = raw.Times.LastModificationTime.Time
	}
	if raw.Times.ExpiryTime != nil {
		entry.ExpiryTime = raw.Times.ExpiryTime.Time
	}
	entry.CustomFields = extractCustomFields(*raw)
	return entry, nil
}

func findGroupParent(g *gokeepasslib.Group, uuid gokeepasslib.UUID) *gokeepasslib.Group {
	for i := range g.Groups {
		if g.Groups[i].UUID.Compare(uuid) {
			return g
		}
		if found := findGroupParent(&g.Groups[i], uuid); found != nil {
			return found
		}
	}
	return nil
}

// cloneEntry creates a deep copy of an entry preserving its UUID.
func cloneEntry(e gokeepasslib.Entry) gokeepasslib.Entry {
	c := e
	c.Values = make([]gokeepasslib.ValueData, len(e.Values))
	copy(c.Values, e.Values)
	c.Histories = make([]gokeepasslib.History, len(e.Histories))
	for i, h := range e.Histories {
		ch := h
		ch.Entries = make([]gokeepasslib.Entry, len(h.Entries))
		for j, he := range h.Entries {
			ch.Entries[j] = cloneEntry(he)
		}
		c.Histories[i] = ch
	}
	c.Binaries = make([]gokeepasslib.BinaryReference, len(e.Binaries))
	copy(c.Binaries, e.Binaries)
	return c
}

func cloneGroup(g gokeepasslib.Group) gokeepasslib.Group {
	c := g
	c.Entries = make([]gokeepasslib.Entry, len(g.Entries))
	for i, e := range g.Entries {
		c.Entries[i] = cloneEntry(e)
	}
	c.Groups = make([]gokeepasslib.Group, len(g.Groups))
	for i, sg := range g.Groups {
		c.Groups[i] = cloneGroup(sg)
	}
	return c
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
