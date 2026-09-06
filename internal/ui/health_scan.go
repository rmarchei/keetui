package ui

import (
	"context"

	tea "charm.land/bubbletea/v2"

	"github.com/rmarchei/keetui/internal/keepass"
)

type healthScanResultMsg struct {
	gen   int
	view  viewState
	stats keepass.DBStats
	audit keepass.AuditResult
}

func (m *AppModel) cancelHealthScan() {
	if m.healthScanCancel != nil {
		m.healthScanCancel()
		m.healthScanCancel = nil
	}
	m.healthScanGen++
}

func (m AppModel) startHealthScan(view viewState) (tea.Model, tea.Cmd) {
	if m.db == nil || m.saving {
		return m, nil
	}
	m.cancelHealthScan()
	ctx, cancel := context.WithCancel(context.Background())
	m.healthScanCancel, m.healthScanView = cancel, view
	gen := m.healthScanGen
	// Only the inexpensive copy runs in Update. The command never touches
	// the live DB, which may be edited, saved or locked while it runs.
	snapshot := m.db.HealthSnapshot()
	m.state = view
	if view == viewStats {
		m.stats = StatsModel{loading: true, width: m.width, height: m.viewHeight()}
	} else {
		m.audit = AuditModel{loading: true, width: m.width, height: m.viewHeight()}
	}
	return m, func() tea.Msg {
		defer cancel()
		result := healthScanResultMsg{gen: gen, view: view}
		var err error
		if view == viewStats {
			result.stats, err = snapshot.Stats(ctx)
		} else {
			result.audit, err = snapshot.Audit(ctx)
		}
		if err != nil {
			return nil // canceled; do not send partial findings or passwords
		}
		return result
	}
}
