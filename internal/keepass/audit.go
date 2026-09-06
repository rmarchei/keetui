package keepass

import (
	"context"

	"github.com/tobischo/gokeepasslib/v3"
)

// AuditEntry represents an entry flagged during audit.
type AuditEntry struct {
	Entry     Entry
	GroupPath string
	Category  string
}

// AuditResult holds categorized audit findings.
type AuditResult struct {
	WeakEntries    []AuditEntry
	ReusedEntries  []AuditEntry
	EmptyEntries   []AuditEntry
	ExpiredEntries []AuditEntry
}

// TotalIssues returns the total number of audit findings.
func (r AuditResult) TotalIssues() int {
	return len(r.WeakEntries) + len(r.ReusedEntries) + len(r.EmptyEntries) + len(r.ExpiredEntries)
}

// Audit performs a comprehensive password audit on the database.
// It skips entries in the recycle bin.
func (d *DB) Audit() AuditResult {
	result, _ := d.HealthSnapshot().Audit(context.Background())
	return result
}

// Audit scores an immutable snapshot; cancellation is checked between entries.
func (s HealthSnapshot) Audit(ctx context.Context) (AuditResult, error) {
	var result AuditResult

	// Collect all passwords for reuse detection. passwordOrder records
	// first-seen tree order so the report is deterministic across runs
	// (map iteration order is randomized).
	passwordEntries := make(map[string][]AuditEntry)
	var passwordOrder []string
	if err := auditGroup(ctx, &s.root, s.root.Name, s.recycleBinUUID, s.recycleBinEnabled, &result, passwordEntries, &passwordOrder); err != nil {
		return AuditResult{}, err
	}

	// Process reused passwords
	for _, pw := range passwordOrder {
		if entries := passwordEntries[pw]; len(entries) > 1 {
			result.ReusedEntries = append(result.ReusedEntries, entries...)
		}
	}

	if err := ctx.Err(); err != nil {
		return AuditResult{}, err
	}
	return result, nil
}

func auditGroup(ctx context.Context, g *Group, path string, rbUUID gokeepasslib.UUID, rbEnabled bool, result *AuditResult, passwordEntries map[string][]AuditEntry, passwordOrder *[]string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if rbEnabled && g.UUID == rbUUID {
		return nil
	}
	for _, e := range g.Entries {
		if err := ctx.Err(); err != nil {
			return err
		}
		ae := AuditEntry{Entry: e, GroupPath: path}

		if e.Password == "" {
			ae.Category = "Empty"
			result.EmptyEntries = append(result.EmptyEntries, ae)
		} else {
			strength := EvaluatePasswordStrength(e.Password)
			if strength.Score <= StrengthWeak {
				ae.Category = "Weak"
				result.WeakEntries = append(result.WeakEntries, ae)
			}
			if len(passwordEntries[e.Password]) == 0 {
				*passwordOrder = append(*passwordOrder, e.Password)
			}
			passwordEntries[e.Password] = append(passwordEntries[e.Password], AuditEntry{
				Entry:     e,
				GroupPath: path,
				Category:  "Reused",
			})
		}

		if e.IsExpired() {
			ae.Category = "Expired"
			result.ExpiredEntries = append(result.ExpiredEntries, ae)
		}
	}

	for i := range g.Groups {
		sg := &g.Groups[i]
		// Skip recycle bin
		if rbEnabled && sg.UUID.Compare(rbUUID) {
			continue
		}
		subPath := path + " > " + sg.Name
		if err := auditGroup(ctx, sg, subPath, rbUUID, rbEnabled, result, passwordEntries, passwordOrder); err != nil {
			return err
		}
	}
	return nil
}
