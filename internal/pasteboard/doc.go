// Package pasteboard writes secrets to the macOS general pasteboard
// in-process, without cgo. Copies carry the nspasteboard.org Concealed (and,
// when a clear is scheduled, Transient) markers, which make the pasteboard
// server exclude them from clipboard history, and they are kept off other
// devices (Universal Clipboard). Ownership is tracked by changeCount, so a
// later clear never needs to read the pasteboard back.
//
// The package is empty on other platforms.
package pasteboard
