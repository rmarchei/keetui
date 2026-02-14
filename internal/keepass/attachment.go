package keepass

import (
	"fmt"
	"os"
	"path/filepath"

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
func (d *DB) ExtractAttachment(entryUUID gokeepasslib.UUID, name, destPath string) error {
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
			return os.WriteFile(destPath, data, 0600)
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
	data, err := os.ReadFile(filePath)
	if err != nil {
		return fmt.Errorf("read attachment file: %w", err)
	}
	name := filepath.Base(filePath)
	return d.AddAttachment(entryUUID, name, data)
}

// DeleteAttachment removes an attachment from an entry.
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
			entry.Binaries = append(entry.Binaries[:i], entry.Binaries[i+1:]...)
			return nil
		}
	}
	return fmt.Errorf("attachment %q not found", name)
}
