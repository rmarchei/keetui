package keepass

import (
	"encoding/base64"
	"encoding/csv"
	"encoding/xml"
	"fmt"
	"io"
	"os"
	"strings"
)

// ExportCSV writes all entries in the database to a CSV file.
// Columns: Group, Title, Username, Password, URL, Notes, TOTP, Attachments
func (d *DB) ExportCSV(w io.Writer) error {
	writer := csv.NewWriter(w)
	defer writer.Flush()

	// Write header
	if err := writer.Write([]string{"Group", "Title", "Username", "Password", "URL", "Notes", "TOTP", "Attachments"}); err != nil {
		return fmt.Errorf("write CSV header: %w", err)
	}

	root := d.RootGroup()
	return d.writeGroupCSV(writer, &root, root.Name)
}

func (d *DB) writeGroupCSV(w *csv.Writer, g *Group, path string) error {
	for _, e := range g.Entries {
		attachments := d.EntryAttachments(e.UUID)
		var names []string
		for _, a := range attachments {
			names = append(names, a.Name)
		}
		attachStr := strings.Join(names, ";")
		record := []string{path, e.Title, e.UserName, e.Password, e.URL, e.Notes, e.TOTP, attachStr}
		if err := w.Write(record); err != nil {
			return fmt.Errorf("write CSV row: %w", err)
		}
	}
	for i := range g.Groups {
		subPath := path + "/" + g.Groups[i].Name
		if err := d.writeGroupCSV(w, &g.Groups[i], subPath); err != nil {
			return err
		}
	}
	return nil
}

// ExportCSVFile is a convenience wrapper that writes CSV to a file.
func (d *DB) ExportCSVFile(path string) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		return fmt.Errorf("create CSV file: %w", err)
	}
	defer f.Close()
	return d.ExportCSV(f)
}

// XMLDatabase represents the XML export structure.
type XMLDatabase struct {
	XMLName xml.Name `xml:"KeePassFile"`
	Root    XMLRoot  `xml:"Root"`
}

// XMLRoot wraps the root group.
type XMLRoot struct {
	Group XMLGroup `xml:"Group"`
}

// XMLGroup represents a group in XML export.
type XMLGroup struct {
	Name    string     `xml:"Name"`
	Groups  []XMLGroup `xml:"Group,omitempty"`
	Entries []XMLEntry `xml:"Entry,omitempty"`
}

// XMLEntry represents an entry in XML export.
type XMLEntry struct {
	Strings  []XMLString `xml:"String"`
	Binaries []XMLBinary `xml:"Binary,omitempty"`
}

// XMLString represents a key-value pair in XML export.
type XMLString struct {
	Key   string `xml:"Key"`
	Value string `xml:"Value"`
}

// XMLBinary represents a binary attachment in XML export.
type XMLBinary struct {
	Name string `xml:"Key"`
	Data string `xml:"Value"` // base64-encoded content
}

// ExportXML writes all entries in the database to XML format.
func (d *DB) ExportXML(w io.Writer) error {
	root := d.RootGroup()
	xmlDB := XMLDatabase{
		Root: XMLRoot{
			Group: d.convertGroupToXML(&root),
		},
	}

	if _, err := io.WriteString(w, xml.Header); err != nil {
		return fmt.Errorf("write XML header: %w", err)
	}

	enc := xml.NewEncoder(w)
	enc.Indent("", "  ")
	if err := enc.Encode(xmlDB); err != nil {
		return fmt.Errorf("encode XML: %w", err)
	}
	return nil
}

func (d *DB) convertGroupToXML(g *Group) XMLGroup {
	xg := XMLGroup{Name: g.Name}
	for _, e := range g.Entries {
		xe := XMLEntry{
			Strings: []XMLString{
				{Key: "Title", Value: e.Title},
				{Key: "UserName", Value: e.UserName},
				{Key: "Password", Value: e.Password},
				{Key: "URL", Value: e.URL},
				{Key: "Notes", Value: e.Notes},
			},
		}
		for _, cf := range e.CustomFields {
			xe.Strings = append(xe.Strings, XMLString{Key: cf.Key, Value: cf.Value})
		}
		// Export binary attachments as base64
		if d != nil {
			attachments := d.EntryAttachments(e.UUID)
			for _, a := range attachments {
				bin := d.findBinaryByName(e.UUID, a.Name)
				if bin != nil {
					xe.Binaries = append(xe.Binaries, XMLBinary{
						Name: a.Name,
						Data: base64.StdEncoding.EncodeToString(bin),
					})
				}
			}
		}
		xg.Entries = append(xg.Entries, xe)
	}
	for i := range g.Groups {
		xg.Groups = append(xg.Groups, d.convertGroupToXML(&g.Groups[i]))
	}
	return xg
}

// findBinaryByName returns the raw bytes of an attachment by name.
func (d *DB) findBinaryByName(entryUUID [16]byte, name string) []byte {
	if len(d.Database.Content.Root.Groups) == 0 {
		return nil
	}
	entry := findEntry(&d.Database.Content.Root.Groups[0], entryUUID)
	if entry == nil {
		return nil
	}
	for _, ref := range entry.Binaries {
		if ref.Name == name {
			bin := d.Database.FindBinary(ref.Value.ID)
			if bin == nil {
				return nil
			}
			data, err := bin.GetContentBytes()
			if err != nil {
				return nil
			}
			return data
		}
	}
	return nil
}

// ExportXMLFile is a convenience wrapper that writes XML to a file.
func (d *DB) ExportXMLFile(path string) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		return fmt.Errorf("create XML file: %w", err)
	}
	defer f.Close()
	return d.ExportXML(f)
}

// ImportCSV reads entries from a CSV file and adds them to the database root group.
// Expected columns: Group, Title, Username, Password, URL, Notes, TOTP
// The Group column is used to create sub-groups as needed.
func (d *DB) ImportCSV(r io.Reader) (int, error) {
	reader := csv.NewReader(r)
	records, err := reader.ReadAll()
	if err != nil {
		return 0, fmt.Errorf("read CSV: %w", err)
	}

	if len(records) < 2 {
		return 0, nil // Empty CSV or header only
	}

	// Detect column indices from header
	header := records[0]
	colIdx := map[string]int{}
	for i, h := range header {
		colIdx[strings.ToLower(strings.TrimSpace(h))] = i
	}

	imported := 0
	root := d.RootGroup()

	for _, record := range records[1:] {
		entry := Entry{}
		groupPath := ""

		if idx, ok := colIdx["group"]; ok && idx < len(record) {
			groupPath = record[idx]
		}
		if idx, ok := colIdx["title"]; ok && idx < len(record) {
			entry.Title = record[idx]
		}
		if idx, ok := colIdx["username"]; ok && idx < len(record) {
			entry.UserName = record[idx]
		}
		if idx, ok := colIdx["password"]; ok && idx < len(record) {
			entry.Password = record[idx]
		}
		if idx, ok := colIdx["url"]; ok && idx < len(record) {
			entry.URL = record[idx]
		}
		if idx, ok := colIdx["notes"]; ok && idx < len(record) {
			entry.Notes = record[idx]
		}
		if idx, ok := colIdx["totp"]; ok && idx < len(record) {
			entry.TOTP = record[idx]
		}

		targetUUID := root.UUID
		if groupPath != "" {
			targetUUID = d.ensureGroupPath(groupPath)
		}

		if _, err := d.AddEntry(targetUUID, entry); err != nil {
			return imported, fmt.Errorf("add entry %q: %w", entry.Title, err)
		}
		imported++
	}

	return imported, nil
}

// ImportCSVFile is a convenience wrapper that imports from a CSV file.
func (d *DB) ImportCSVFile(path string) (int, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, fmt.Errorf("open CSV file: %w", err)
	}
	defer f.Close()
	return d.ImportCSV(f)
}

// ImportXML reads entries from an XML file and adds them to the database.
func (d *DB) ImportXML(r io.Reader) (int, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return 0, fmt.Errorf("read XML: %w", err)
	}

	var xmlDB XMLDatabase
	if err := xml.Unmarshal(data, &xmlDB); err != nil {
		return 0, fmt.Errorf("parse XML: %w", err)
	}

	root := d.RootGroup()
	return d.importXMLGroup(&xmlDB.Root.Group, root.UUID)
}

func (d *DB) importXMLGroup(xg *XMLGroup, parentUUID [16]byte) (int, error) {
	imported := 0

	// Ensure the group exists
	groupUUID := parentUUID
	if xg.Name != "" && xg.Name != "Root" {
		groupUUID = d.ensureGroupPath(xg.Name)
	}

	for _, xe := range xg.Entries {
		entry := Entry{}
		for _, xs := range xe.Strings {
			switch xs.Key {
			case "Title":
				entry.Title = xs.Value
			case "UserName":
				entry.UserName = xs.Value
			case "Password":
				entry.Password = xs.Value
			case "URL":
				entry.URL = xs.Value
			case "Notes":
				entry.Notes = xs.Value
			default:
				entry.CustomFields = append(entry.CustomFields, CustomField{
					Key:   xs.Key,
					Value: xs.Value,
				})
			}
		}
		if _, err := d.AddEntry(groupUUID, entry); err != nil {
			return imported, fmt.Errorf("add entry %q: %w", entry.Title, err)
		}
		// Restore binary attachments
		if len(xe.Binaries) > 0 {
			// Find the entry we just added (last entry in the group)
			g := findGroup(&d.Database.Content.Root.Groups[0], groupUUID)
			if g != nil && len(g.Entries) > 0 {
				newEntry := &g.Entries[len(g.Entries)-1]
				for _, xb := range xe.Binaries {
					data, err := base64.StdEncoding.DecodeString(xb.Data)
					if err != nil {
						continue // skip malformed binary data
					}
					bin := d.Database.AddBinary(data)
					newEntry.Binaries = append(newEntry.Binaries, bin.CreateReference(xb.Name))
				}
			}
		}
		imported++
	}

	for i := range xg.Groups {
		n, err := d.importXMLGroup(&xg.Groups[i], groupUUID)
		imported += n
		if err != nil {
			return imported, err
		}
	}

	return imported, nil
}

// ImportXMLFile is a convenience wrapper that imports from an XML file.
func (d *DB) ImportXMLFile(path string) (int, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, fmt.Errorf("open XML file: %w", err)
	}
	defer f.Close()
	return d.ImportXML(f)
}

// ensureGroupPath creates groups along the path if they don't exist,
// returning the UUID of the leaf group.
func (d *DB) ensureGroupPath(path string) [16]byte {
	parts := strings.Split(path, "/")
	root := d.RootGroup()
	currentUUID := root.UUID

	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" || part == "Root" {
			continue
		}
		// Look for existing group
		found := false
		current := d.findGroupByUUID(currentUUID)
		if current != nil {
			for _, g := range current.Groups {
				if g.Name == part {
					currentUUID = g.UUID
					found = true
					break
				}
			}
		}
		if !found {
			_, _ = d.AddGroup(currentUUID, part)
			// Re-read to get the new group's UUID
			current = d.findGroupByUUID(currentUUID)
			if current != nil {
				for _, g := range current.Groups {
					if g.Name == part {
						currentUUID = g.UUID
						break
					}
				}
			}
		}
	}
	return currentUUID
}

func (d *DB) findGroupByUUID(uuid [16]byte) *Group {
	root := d.RootGroup()
	return findGroupSimple(&root, uuid)
}

func findGroupSimple(g *Group, uuid [16]byte) *Group {
	if g.UUID == uuid {
		return g
	}
	for i := range g.Groups {
		if found := findGroupSimple(&g.Groups[i], uuid); found != nil {
			return found
		}
	}
	return nil
}
