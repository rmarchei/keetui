//go:build !darwin && !linux

package ui

// Other platforms have no local clipboard backend; copies use OSC 52.
func newNativeClipboard() nativeClipboard { return nil }
