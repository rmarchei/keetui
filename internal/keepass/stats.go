package keepass

import (
	"os"
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

	root := d.RootGroup()
	countGroupStats(&root, &stats)

	// Detect reused passwords
	passwordCounts := make(map[string]int)
	collectPasswords(&root, passwordCounts)
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

func countGroupStats(g *Group, stats *DBStats) {
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
		countGroupStats(&g.Groups[i], stats)
	}
}

func collectPasswords(g *Group, counts map[string]int) {
	for _, e := range g.Entries {
		if e.Password != "" {
			counts[e.Password]++
		}
	}
	for i := range g.Groups {
		collectPasswords(&g.Groups[i], counts)
	}
}
