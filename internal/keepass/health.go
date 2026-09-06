package keepass

import "github.com/tobischo/gokeepasslib/v3"

// EntryCount counts live entries without reading or scoring passwords.
func (d *DB) EntryCount() int {
	if len(d.Database.Content.Root.Groups) == 0 {
		return 0
	}
	rbUUID, rbEnabled := d.RecycleBinUUID(), d.RecycleBinEnabled()
	var count func(*gokeepasslib.Group) int
	count = func(g *gokeepasslib.Group) int {
		if rbEnabled && g.UUID == rbUUID {
			return 0
		}
		n := len(g.Entries)
		for i := range g.Groups {
			n += count(&g.Groups[i])
		}
		return n
	}
	return count(&d.Database.Content.Root.Groups[0])
}

// HealthSnapshot owns an independent tree for background password analysis.
// Create it on the DB-owning goroutine, before starting the worker. Neither
// analysis method reads the live database, its credentials or attachments.
type HealthSnapshot struct {
	root              Group
	recycleBinUUID    gokeepasslib.UUID
	recycleBinEnabled bool
	path, format      string
}

func (d *DB) HealthSnapshot() HealthSnapshot {
	return HealthSnapshot{
		root:              d.RootGroup(),
		recycleBinUUID:    d.RecycleBinUUID(),
		recycleBinEnabled: d.RecycleBinEnabled(),
		path:              d.Path,
		format:            d.FormatVersion(),
	}
}
