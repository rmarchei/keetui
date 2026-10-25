//go:build !darwin && !linux

package ui

// Elsewhere keetui does not read other processes: detection relies on
// tmux and on keetui's own environment.
func newProcessTable() processTable { return nil }
