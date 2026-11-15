package keepass

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"github.com/tobischo/gokeepasslib/v3"
	w "github.com/tobischo/gokeepasslib/v3/wrappers"
)

// DiskState compares the database file with what keetui last read from or
// wrote to it.
type DiskState int

const (
	DiskUnchanged  DiskState = iota // the file holds what keetui last read or wrote
	DiskChanged                     // another program (or a sync) wrote the file since
	DiskMissing                     // the file is gone; its folder is there
	DiskNoFolder                    // the file's folder is gone too: a volume unmounted, a folder moved
	DiskUnreadable                  // the file is there but cannot be read to compare: permissions, an I/O error, a cloud placeholder
)

// DiskError is what a save returns instead of overwriting a database file
// that changed since keetui read it, or that it cannot read to tell, or
// recreating one that is gone. Overwrite saves anyway; MergeDisk takes the
// file's changes in first.
type DiskError struct {
	State DiskState
	Path  string
	Err   error // why the file cannot be read (DiskUnreadable)
}

func (e *DiskError) Error() string {
	switch e.State {
	case DiskChanged:
		return fmt.Sprintf("%s changed on disk since keetui read it", e.Path)
	case DiskMissing:
		return fmt.Sprintf("%s is no longer on disk", e.Path)
	case DiskUnreadable:
		return fmt.Sprintf("%s cannot be read to check it for changes made elsewhere: %v", e.Path, e.Err)
	}
	return fmt.Sprintf("the folder of %s is gone (volume unmounted?)", e.Path)
}

func (e *DiskError) Unwrap() error { return e.Err }

// diskStamp is what keetui last read from or wrote to the database file: a
// checksum to notice another writer, the credentials the file is encrypted
// with (a master password changed since applies only from the next save),
// and the entries, groups and custom data it held, so that MergeDisk can
// tell an entry deleted here from one added there.
type diskStamp struct {
	known      bool
	path       string // filepath.Clean of the path read or written
	sum        [sha256.Size]byte
	creds      *gokeepasslib.DBCredentials
	entries    map[gokeepasslib.UUID]time.Time // modification times
	groups     map[gokeepasslib.UUID]bool
	customData map[string]string // database-level custom data items
}

func newDiskStamp(path string, sum [sha256.Size]byte, creds *gokeepasslib.DBCredentials, db *gokeepasslib.Database) diskStamp {
	s := diskStamp{
		known:      true,
		path:       filepath.Clean(path),
		sum:        sum,
		creds:      creds,
		entries:    make(map[gokeepasslib.UUID]time.Time),
		groups:     make(map[gokeepasslib.UUID]bool),
		customData: make(map[string]string),
	}
	var walk func(g *gokeepasslib.Group)
	walk = func(g *gokeepasslib.Group) {
		s.groups[g.UUID] = true
		for i := range g.Entries {
			s.entries[g.Entries[i].UUID] = timeOf(g.Entries[i].Times.LastModificationTime)
		}
		for i := range g.Groups {
			walk(&g.Groups[i])
		}
	}
	if db.Content != nil && db.Content.Meta != nil {
		for _, item := range db.Content.Meta.CustomData {
			s.customData[item.Key] = item.Value
		}
	}
	if db.Content != nil && db.Content.Root != nil {
		for i := range db.Content.Root.Groups {
			walk(&db.Content.Root.Groups[i])
		}
	}
	return s
}

func timeOf(t *w.TimeWrapper) time.Time {
	if t == nil {
		return time.Time{}
	}
	return t.Time
}

// CheckDisk compares the database file with what keetui last read from or
// wrote to it. A file it cannot read may have changed: it counts as
// DiskUnreadable, never as unchanged.
func (d *DB) CheckDisk() DiskState {
	state, _ := d.checkDisk()
	return state
}

// checkDisk is CheckDisk, with the reason a file cannot be read.
func (d *DB) checkDisk() (DiskState, error) {
	if !d.disk.known || d.disk.path != filepath.Clean(d.Path) {
		return DiskUnchanged, nil
	}
	data, err := os.ReadFile(d.Path)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		if _, err := os.Stat(filepath.Dir(d.Path)); errors.Is(err, fs.ErrNotExist) {
			return DiskNoFolder, nil
		}
		return DiskMissing, nil
	case err != nil:
		var pathErr *fs.PathError
		if errors.As(err, &pathErr) {
			err = pathErr.Err // DiskError names the file
		}
		return DiskUnreadable, err
	case sha256.Sum256(data) != d.disk.sum:
		return DiskChanged, nil
	}
	return DiskUnchanged, nil
}

// ownsPath reports whether path is the database's own file.
func (d *DB) ownsPath(path string) bool {
	if filepath.Clean(path) == filepath.Clean(d.Path) {
		return true
	}
	a, errA := os.Stat(path)
	b, errB := os.Stat(d.Path)
	return errA == nil && errB == nil && os.SameFile(a, b)
}

// Overwrite saves the database to its file whatever the file holds now:
// changes another program made there are lost, and a missing file is
// written again.
func (d *DB) Overwrite() error {
	return d.write(d.Path)
}
