package keepass

import (
	"context"
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
	stats, _ := d.HealthSnapshot().Stats(context.Background())
	return stats
}

// Stats scores an immutable snapshot; cancellation is checked between entries.
func (s HealthSnapshot) Stats(ctx context.Context) (DBStats, error) {
	if err := ctx.Err(); err != nil {
		return DBStats{}, err
	}
	stats := DBStats{
		FilePath:      s.path,
		FormatVersion: s.format,
	}

	if info, err := os.Stat(s.path); err == nil {
		stats.FileSize = info.Size()
	}

	// Skip the recycle-bin subtree, matching Audit's behavior: deleted
	// entries should not count against password hygiene. The zero UUID
	// (bin disabled) matches no real group.
	var rbUUID gokeepasslib.UUID
	if s.recycleBinEnabled {
		rbUUID = s.recycleBinUUID
	}
	if err := countGroupStats(ctx, &s.root, rbUUID, &stats); err != nil {
		return DBStats{}, err
	}

	// Detect reused passwords
	passwordCounts := make(map[string]int)
	collectPasswords(&s.root, rbUUID, passwordCounts)
	for pw, count := range passwordCounts {
		if pw == "" {
			continue
		}
		if count > 1 {
			stats.ReusedPasswords += count
		}
	}

	if err := ctx.Err(); err != nil {
		return DBStats{}, err
	}
	return stats, nil
}

func countGroupStats(ctx context.Context, g *Group, rbUUID gokeepasslib.UUID, stats *DBStats) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if g.UUID == rbUUID {
		return nil
	}
	stats.TotalGroups++
	for _, e := range g.Entries {
		if err := ctx.Err(); err != nil {
			return err
		}
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
		if err := countGroupStats(ctx, &g.Groups[i], rbUUID, stats); err != nil {
			return err
		}
	}
	return nil
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
