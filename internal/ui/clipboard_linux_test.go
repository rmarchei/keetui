//go:build linux

package ui

import (
	"slices"
	"testing"
)

func TestLinuxToolSelection(t *testing.T) {
	for _, tc := range []struct {
		name  string
		env   map[string]string
		tools []string
		want  string
	}{
		{"wayland", map[string]string{"WAYLAND_DISPLAY": "wayland-0", "DISPLAY": ":0"}, []string{"wl-copy", "wl-paste", "xclip"}, "wl-copy"},
		{"wayland without wl-clipboard", map[string]string{"WAYLAND_DISPLAY": "wayland-0", "DISPLAY": ":0"}, []string{"xclip"}, "xclip"},
		{"x11 xclip", map[string]string{"DISPLAY": ":0"}, []string{"xclip", "xsel", "wl-copy", "wl-paste"}, "xclip"},
		{"x11 xsel", map[string]string{"DISPLAY": ":0"}, []string{"xsel"}, "xsel"},
		{"no display", nil, []string{"xclip", "wl-copy", "wl-paste"}, ""},
		{"no tools", map[string]string{"DISPLAY": ":0"}, nil, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			getenv := func(k string) string { return tc.env[k] }
			has := func(name string) bool { return slices.Contains(tc.tools, name) }
			if got := linuxTool(getenv, has); got != tc.want {
				t.Fatalf("linuxTool = %q, want %q", got, tc.want)
			}
		})
	}
}
