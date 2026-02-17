package keepass

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/tobischo/gokeepasslib/v3"
)

// Attachment represents a file attachment on an entry.
type Attachment struct {
	Name string
	Size int
}

// EntryAttachments returns the list of attachments for an entry.
func (d *DB) EntryAttachments(entryUUID gokeepasslib.UUID) []Attachment {
	if len(d.Database.Content.Root.Groups) == 0 {
		return nil
	}
	entry := findEntry(&d.Database.Content.Root.Groups[0], entryUUID)
	if entry == nil {
		return nil
	}

	var attachments []Attachment
	for _, ref := range entry.Binaries {
		size := 0
		bin := d.Database.FindBinary(ref.Value.ID)
		if bin != nil {
			if data, err := bin.GetContentBytes(); err == nil {
				size = len(data)
			}
		}
		attachments = append(attachments, Attachment{
			Name: ref.Name,
			Size: size,
		})
	}
	return attachments
}

// ExtractAttachment saves an attachment to disk.
// The destPath is validated to reject path traversal attempts and symlink writes.
func (d *DB) ExtractAttachment(entryUUID gokeepasslib.UUID, name, destPath string) error {
	// Reject paths containing ".." to prevent directory traversal
	destPath = filepath.Clean(destPath)
	for _, part := range strings.Split(destPath, string(filepath.Separator)) {
		if part == ".." {
			return fmt.Errorf("invalid destination path (directory traversal): %q", destPath)
		}
	}

	// Resolve symlinks in the parent directory so that legitimate system
	// symlinks (e.g. /tmp -> /private/tmp on macOS) are accepted.
	parentDir := filepath.Dir(destPath)
	if resolvedParent, err := filepath.EvalSymlinks(parentDir); err == nil {
		destPath = filepath.Join(resolvedParent, filepath.Base(destPath))
	}

	// Refuse to write through a symlink to prevent overwriting arbitrary files.
	if fi, err := os.Lstat(destPath); err == nil {
		if fi.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("destination path is a symlink, refusing to write: %q", destPath)
		}
	}

	if len(d.Database.Content.Root.Groups) == 0 {
		return fmt.Errorf("database has no root group")
	}
	entry := findEntry(&d.Database.Content.Root.Groups[0], entryUUID)
	if entry == nil {
		return fmt.Errorf("entry not found")
	}

	for _, ref := range entry.Binaries {
		if ref.Name == name {
			bin := d.Database.FindBinary(ref.Value.ID)
			if bin == nil {
				return fmt.Errorf("binary data not found for %q", name)
			}
			data, err := bin.GetContentBytes()
			if err != nil {
				return fmt.Errorf("decode binary %q: %w", name, err)
			}
			// O_NOFOLLOW prevents following a symlink that appears after the Lstat check (TOCTOU).
			f, err := os.OpenFile(destPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC|syscall.O_NOFOLLOW, 0600)
			if err != nil {
				return fmt.Errorf("create file %q: %w", destPath, err)
			}
			_, writeErr := f.Write(data)
			closeErr := f.Close()
			if writeErr != nil {
				return fmt.Errorf("write file %q: %w", destPath, writeErr)
			}
			return closeErr
		}
	}
	return fmt.Errorf("attachment %q not found", name)
}

// AddAttachment adds a file attachment to an entry.
func (d *DB) AddAttachment(entryUUID gokeepasslib.UUID, name string, data []byte) error {
	if len(d.Database.Content.Root.Groups) == 0 {
		return fmt.Errorf("database has no root group")
	}
	entry := findEntry(&d.Database.Content.Root.Groups[0], entryUUID)
	if entry == nil {
		return fmt.Errorf("entry not found")
	}

	bin := d.Database.AddBinary(data)
	entry.Binaries = append(entry.Binaries, bin.CreateReference(name))

	return nil
}

// AddAttachmentFile adds a file attachment from a file path.
func (d *DB) AddAttachmentFile(entryUUID gokeepasslib.UUID, filePath string) error {
	filePath = filepath.Clean(filePath)
	data, err := os.ReadFile(filePath)
	if err != nil {
		return fmt.Errorf("read attachment file: %w", err)
	}
	name := filepath.Base(filePath)
	return d.AddAttachment(entryUUID, name, data)
}

// DeleteAttachment removes an attachment from an entry and cleans up
// orphaned binary data from the database pool.
func (d *DB) DeleteAttachment(entryUUID gokeepasslib.UUID, name string) error {
	if len(d.Database.Content.Root.Groups) == 0 {
		return fmt.Errorf("database has no root group")
	}
	entry := findEntry(&d.Database.Content.Root.Groups[0], entryUUID)
	if entry == nil {
		return fmt.Errorf("entry not found")
	}

	for i, ref := range entry.Binaries {
		if ref.Name == name {
			binID := ref.Value.ID
			entry.Binaries = append(entry.Binaries[:i], entry.Binaries[i+1:]...)
			// Clean up the binary data if no other entry references it
			if !d.isBinaryReferenced(binID) {
				d.removeBinary(binID)
			}
			return nil
		}
	}
	return fmt.Errorf("attachment %q not found", name)
}

// isBinaryReferenced returns true if any entry in the database references the binary ID.
func (d *DB) isBinaryReferenced(binID int) bool {
	if len(d.Database.Content.Root.Groups) == 0 {
		return false
	}
	return groupReferencesBinary(&d.Database.Content.Root.Groups[0], binID)
}

func groupReferencesBinary(g *gokeepasslib.Group, binID int) bool {
	for _, e := range g.Entries {
		for _, ref := range e.Binaries {
			if ref.Value.ID == binID {
				return true
			}
		}
	}
	for i := range g.Groups {
		if groupReferencesBinary(&g.Groups[i], binID) {
			return true
		}
	}
	return false
}

// removeBinary removes a binary from the database's binary pools.
func (d *DB) removeBinary(binID int) {
	// Remove from Meta.Binaries (KDBX 3.1)
	if d.Database.Content.Meta != nil {
		var kept gokeepasslib.Binaries
		for _, b := range d.Database.Content.Meta.Binaries {
			if b.ID != binID {
				kept = append(kept, b)
			}
		}
		d.Database.Content.Meta.Binaries = kept
	}
	// Remove from InnerHeader.Binaries (KDBX 4)
	if d.Database.Content.InnerHeader != nil {
		var kept gokeepasslib.Binaries
		for _, b := range d.Database.Content.InnerHeader.Binaries {
			if b.ID != binID {
				kept = append(kept, b)
			}
		}
		d.Database.Content.InnerHeader.Binaries = kept
	}
}
