package keepass

import "github.com/tobischo/gokeepasslib/v3"

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
	var result AuditResult
	root := d.RootGroup()
	rbUUID := d.RecycleBinUUID()

	// Collect all passwords for reuse detection
	passwordEntries := make(map[string][]AuditEntry)
	auditGroup(&root, root.Name, rbUUID, d.RecycleBinEnabled(), &result, passwordEntries)

	// Process reused passwords
	for _, entries := range passwordEntries {
		if len(entries) > 1 {
			result.ReusedEntries = append(result.ReusedEntries, entries...)
		}
	}

	return result
}

func auditGroup(g *Group, path string, rbUUID gokeepasslib.UUID, rbEnabled bool, result *AuditResult, passwordEntries map[string][]AuditEntry) {
	for _, e := range g.Entries {
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
		auditGroup(sg, subPath, rbUUID, rbEnabled, result, passwordEntries)
	}
}
