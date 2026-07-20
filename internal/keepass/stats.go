package keepass

import (
	"os"

	"github.com/tobischo/gokeepasslib/v3"
)

// DBStats holds statistics about a KeePass database.
type DBStats struct {
	FilePath        string
	FileSize        int64
	FormatVersion   string
	TotalGroups     int
	TotalEntries    int
	WeakPasswords   int
	ReusedPasswords int
	EmptyPasswords  int
	ExpiredEntries  int
}

// Stats computes statistics about the database.
func (d *DB) Stats() DBStats {
	stats := DBStats{
		FilePath:      d.Path,
		FormatVersion: d.FormatVersion(),
	}

	if info, err := os.Stat(d.Path); err == nil {
		stats.FileSize = info.Size()
	}

	// Skip the recycle-bin subtree, matching Audit's behavior: deleted
	// entries should not count against password hygiene. The zero UUID
	// (bin disabled) matches no real group.
	var rbUUID gokeepasslib.UUID
	if d.RecycleBinEnabled() {
		rbUUID = d.RecycleBinUUID()
	}
	root := d.RootGroup()
	countGroupStats(&root, rbUUID, &stats)

	// Detect reused passwords
	passwordCounts := make(map[string]int)
	collectPasswords(&root, rbUUID, passwordCounts)
	for pw, count := range passwordCounts {
		if pw == "" {
			continue
		}
		if count > 1 {
			stats.ReusedPasswords += count
		}
	}

	return stats
}

func countGroupStats(g *Group, rbUUID gokeepasslib.UUID, stats *DBStats) {
	if g.UUID == rbUUID {
		return
	}
	stats.TotalGroups++
	for _, e := range g.Entries {
		stats.TotalEntries++
		if e.Password == "" {
			stats.EmptyPasswords++
		} else {
			result := EvaluatePasswordStrength(e.Password)
			if result.Score <= StrengthWeak {
				stats.WeakPasswords++
			}
		}
		if e.IsExpired() {
			stats.ExpiredEntries++
		}
	}
	for i := range g.Groups {
		countGroupStats(&g.Groups[i], rbUUID, stats)
	}
}

func collectPasswords(g *Group, rbUUID gokeepasslib.UUID, counts map[string]int) {
	if g.UUID == rbUUID {
		return
	}
	for _, e := range g.Entries {
		if e.Password != "" {
			counts[e.Password]++
		}
	}
	for i := range g.Groups {
		collectPasswords(&g.Groups[i], rbUUID, counts)
	}
}
