package ui

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/rmarchei/keetui/internal/config"
)

// Probe outputs captured from real tmux servers (display-message with
// tmuxProbeFormat, list-windows -a, list-clients, then show-messages -T),
// trimmed to the probe line, the window and client rows, the terminal
// headers and the lines around Ms. Each server had one client
// (xterm-256color) unless noted, and allow-passthrough on from 3.3 except
// in "default". "two clients" adds a screen-256color client; "hidden"
// shows another window, "zoomed" zooms another pane of keetui's window,
// "copy mode" puts keetui's pane in copy mode, and "suspended" suspends the
// client with prefix C-z, no tmux command running in between. "grouped"
// adds a grouped session whose client shows another window, and a third
// session that links keetui's window while its client shows its own;
// allow-passthrough is all there from 3.4. Unit tests never run tmux.
var tmuxFixtures = map[string]string{
	"3.0a default": `keetui-probe|3.0a|external||/dev/pts/1|xterm-256color|1|1||0|1||@0|Unknown
keetui-window|$0|1|@0|1
keetui-client|/dev/pts/1|$0|
Terminal 0: xterm-256color [references=1, flags=0x1]:
 179: kUP7: (string) \033[1;7A
 180: Ms: (string) \033]52;%p1%s;%p2%s\a
 181: op: (string) \033[39;49m
`,
	"3.0a two clients": `keetui-probe|3.0a|external||/dev/pts/3|xterm-256color|2|1||0|1||@0|Unknown
keetui-window|$0|2|@0|1
keetui-client|/dev/pts/2|$0|
keetui-client|/dev/pts/3|$0|
Terminal 0: xterm-256color [references=1, flags=0x1]:
 179: kUP7: (string) \033[1;7A
 180: Ms: (string) \033]52;%p1%s;%p2%s\a
 181: op: (string) \033[39;49m
Terminal 1: screen-256color [references=1, flags=0x1]:
 179: kUP7: [missing]
 180: Ms: [missing]
 181: op: (string) \033[39;49m
`,
	// show-messages failed: no current client (exit 1).
	"3.0a detached": `keetui-probe|3.0a|external||||0|1||0|1||@0|
keetui-window|$0|0|@0|1
`,
	"3.0a hidden": `keetui-probe|3.0a|external||/dev/pts/4|xterm-256color|1|0||0|1||@0|Unknown
keetui-window|$0|1|@0|0
keetui-window|$0|1|@1|1
keetui-client|/dev/pts/4|$0|
Terminal 0: xterm-256color [references=1, flags=0x1]:
 179: kUP7: (string) \033[1;7A
 180: Ms: (string) \033]52;%p1%s;%p2%s\a
 181: op: (string) \033[39;49m
`,
	"3.0a zoomed": `keetui-probe|3.0a|external||/dev/pts/5|xterm-256color|1|1||1|0||@0|Unknown
keetui-window|$0|1|@0|1
keetui-client|/dev/pts/5|$0|
Terminal 0: xterm-256color [references=1, flags=0x1]:
 179: kUP7: (string) \033[1;7A
 180: Ms: (string) \033]52;%p1%s;%p2%s\a
 181: op: (string) \033[39;49m
`,
	"3.0a copy mode": `keetui-probe|3.0a|external||/dev/pts/6|xterm-256color|1|1||0|1|copy-mode|@0|Unknown
keetui-window|$0|1|@0|1
keetui-client|/dev/pts/6|$0|
Terminal 0: xterm-256color [references=1, flags=0x1]:
 179: kUP7: (string) \033[1;7A
 180: Ms: (string) \033]52;%p1%s;%p2%s\a
 181: op: (string) \033[39;49m
`,
	"3.0a suspended": `keetui-probe|3.0a|external||/dev/pts/7|xterm-256color|1|1||0|1||@0|Unknown
keetui-window|$0|1|@0|1
keetui-client|/dev/pts/7|$0|
Terminal 0: xterm-256color [references=1, flags=0x1]:
 179: kUP7: (string) \033[1;7A
 180: Ms: (string) \033]52;%p1%s;%p2%s\a
 181: op: (string) \033[39;49m
`,
	"3.0a grouped": `keetui-probe|3.0a|external||/dev/pts/12|xterm-256color|1|0||0|1||@0|Unknown
keetui-window|$0|1|@0|1
keetui-window|$0|1|@2|0
keetui-window|$1|1|@0|0
keetui-window|$1|1|@2|1
keetui-window|$2|1|@3|1
keetui-window|$2|1|@0|0
keetui-client|/dev/pts/8|$0|
keetui-client|/dev/pts/10|$1|
keetui-client|/dev/pts/12|$2|
Terminal 0: xterm-256color [references=3, flags=0x1]:
 179: kUP7: (string) \033[1;7A
 180: Ms: (string) \033]52;%p1%s;%p2%s\a
 181: op: (string) \033[39;49m
`,
	"3.2a default": `keetui-probe|3.2a|external||/dev/pts/1|xterm-256color|1|1|1|0|1||@0|
keetui-window|$0|1|@0|1
keetui-client|/dev/pts/1|$0|attached,focused,UTF-8
Terminal 0: xterm-256color for /dev/pts/1, flags=0x20:
 191: kUP7: (string) \033[1;7A
 192: Ms: (string) \033]52;%p1%s;%p2%s\a
 193: ol: [missing]
`,
	"3.2a two clients": `keetui-probe|3.2a|external||/dev/pts/3|xterm-256color|2|1|2|0|1||@0|
keetui-window|$0|2|@0|1
keetui-client|/dev/pts/2|$0|attached,focused,UTF-8
keetui-client|/dev/pts/3|$0|attached,focused,UTF-8
Terminal 0: xterm-256color for /dev/pts/3, flags=0x20:
 191: kUP7: (string) \033[1;7A
 192: Ms: (string) \033]52;%p1%s;%p2%s\a
 193: ol: [missing]
Terminal 1: screen-256color for /dev/pts/2, flags=0x20:
 191: kUP7: [missing]
 192: Ms: [missing]
 193: ol: [missing]
`,
	// show-messages failed: no current client (exit 1).
	"3.2a detached": `keetui-probe|3.2a|external||||0|1|0|0|1||@0|
keetui-window|$0|0|@0|1
`,
	"3.2a hidden": `keetui-probe|3.2a|external||/dev/pts/4|xterm-256color|1|0|0|0|1||@0|
keetui-window|$0|1|@0|0
keetui-window|$0|1|@1|1
keetui-client|/dev/pts/4|$0|attached,focused,UTF-8
Terminal 0: xterm-256color for /dev/pts/4, flags=0x20:
 191: kUP7: (string) \033[1;7A
 192: Ms: (string) \033]52;%p1%s;%p2%s\a
 193: ol: [missing]
`,
	"3.2a zoomed": `keetui-probe|3.2a|external||/dev/pts/5|xterm-256color|1|1|1|1|0||@0|
keetui-window|$0|1|@0|1
keetui-client|/dev/pts/5|$0|attached,focused,UTF-8
Terminal 0: xterm-256color for /dev/pts/5, flags=0x20:
 191: kUP7: (string) \033[1;7A
 192: Ms: (string) \033]52;%p1%s;%p2%s\a
 193: ol: [missing]
`,
	"3.2a copy mode": `keetui-probe|3.2a|external||/dev/pts/6|xterm-256color|1|1|1|0|1|copy-mode|@0|
keetui-window|$0|1|@0|1
keetui-client|/dev/pts/6|$0|attached,focused,UTF-8
Terminal 0: xterm-256color for /dev/pts/6, flags=0x20:
 191: kUP7: (string) \033[1;7A
 192: Ms: (string) \033]52;%p1%s;%p2%s\a
 193: ol: [missing]
`,
	"3.2a suspended": `keetui-probe|3.2a|external||/dev/pts/7|xterm-256color|1|1|1|0|1||@0|
keetui-window|$0|1|@0|1
keetui-client|/dev/pts/7|$0|attached,focused,suspended,UTF-8
Terminal 0: xterm-256color for /dev/pts/7, flags=0x20:
 191: kUP7: (string) \033[1;7A
 192: Ms: (string) \033]52;%p1%s;%p2%s\a
 193: ol: [missing]
`,
	"3.2a grouped": `keetui-probe|3.2a|external||/dev/pts/12|xterm-256color|1|0|1|0|1||@0|
keetui-window|$0|1|@0|1
keetui-window|$0|1|@2|0
keetui-window|$1|1|@0|0
keetui-window|$1|1|@2|1
keetui-window|$2|1|@3|1
keetui-window|$2|1|@0|0
keetui-client|/dev/pts/8|$0|attached,focused,UTF-8
keetui-client|/dev/pts/10|$1|attached,focused,UTF-8
keetui-client|/dev/pts/12|$2|attached,focused,UTF-8
Terminal 0: xterm-256color for /dev/pts/12, flags=0x20:
 191: kUP7: (string) \033[1;7A
 192: Ms: (string) \033]52;%p1%s;%p2%s\a
 193: ol: [missing]
Terminal 1: xterm-256color for /dev/pts/10, flags=0x20:
 191: kUP7: (string) \033[1;7A
 192: Ms: (string) \033]52;%p1%s;%p2%s\a
 193: ol: [missing]
Terminal 2: xterm-256color for /dev/pts/8, flags=0x20:
 191: kUP7: (string) \033[1;7A
 192: Ms: (string) \033]52;%p1%s;%p2%s\a
 193: ol: [missing]
`,
	"3.3a default": `keetui-probe|3.3a|external|0|/dev/pts/1|xterm-256color|1|1|1|0|1||@0|
keetui-window|$0|1|@0|1
keetui-client|/dev/pts/1|$0|attached,focused,UTF-8
Terminal 0: xterm-256color for /dev/pts/1, flags=0x20:
 191: kUP7: (string) \033[1;7A
 192: Ms: (string) \033]52;%p1%s;%p2%s\a
 193: ol: [missing]
`,
	"3.3a two clients": `keetui-probe|3.3a|external|1|/dev/pts/3|xterm-256color|2|1|2|0|1||@0|
keetui-window|$0|2|@0|1
keetui-client|/dev/pts/2|$0|attached,focused,UTF-8
keetui-client|/dev/pts/3|$0|attached,focused,UTF-8
Terminal 0: xterm-256color for /dev/pts/3, flags=0x20:
 191: kUP7: (string) \033[1;7A
 192: Ms: (string) \033]52;%p1%s;%p2%s\a
 193: ol: [missing]
Terminal 1: screen-256color for /dev/pts/2, flags=0x20:
 191: kUP7: [missing]
 192: Ms: [missing]
 193: ol: [missing]
`,
	// show-messages failed: no current client (exit 1).
	"3.3a detached": `keetui-probe|3.3a|external|1|||0|1|0|0|1||@0|
keetui-window|$0|0|@0|1
`,
	"3.3a hidden": `keetui-probe|3.3a|external|1|/dev/pts/4|xterm-256color|1|0|0|0|1||@0|
keetui-window|$0|1|@0|0
keetui-window|$0|1|@1|1
keetui-client|/dev/pts/4|$0|attached,focused,UTF-8
Terminal 0: xterm-256color for /dev/pts/4, flags=0x20:
 191: kUP7: (string) \033[1;7A
 192: Ms: (string) \033]52;%p1%s;%p2%s\a
 193: ol: [missing]
`,
	"3.3a zoomed": `keetui-probe|3.3a|external|1|/dev/pts/5|xterm-256color|1|1|1|1|0||@0|
keetui-window|$0|1|@0|1
keetui-client|/dev/pts/5|$0|attached,focused,UTF-8
Terminal 0: xterm-256color for /dev/pts/5, flags=0x20:
 191: kUP7: (string) \033[1;7A
 192: Ms: (string) \033]52;%p1%s;%p2%s\a
 193: ol: [missing]
`,
	"3.3a copy mode": `keetui-probe|3.3a|external|1|/dev/pts/6|xterm-256color|1|1|1|0|1|copy-mode|@0|
keetui-window|$0|1|@0|1
keetui-client|/dev/pts/6|$0|attached,focused,UTF-8
Terminal 0: xterm-256color for /dev/pts/6, flags=0x20:
 191: kUP7: (string) \033[1;7A
 192: Ms: (string) \033]52;%p1%s;%p2%s\a
 193: ol: [missing]
`,
	"3.3a suspended": `keetui-probe|3.3a|external|1|/dev/pts/7|xterm-256color|1|1|1|0|1||@0|
keetui-window|$0|1|@0|1
keetui-client|/dev/pts/7|$0|attached,focused,suspended,UTF-8
Terminal 0: xterm-256color for /dev/pts/7, flags=0x20:
 191: kUP7: (string) \033[1;7A
 192: Ms: (string) \033]52;%p1%s;%p2%s\a
 193: ol: [missing]
`,
	"3.3a grouped": `keetui-probe|3.3a|external|1|/dev/pts/12|xterm-256color|1|0|1|0|1||@0|
keetui-window|$0|1|@0|1
keetui-window|$0|1|@2|0
keetui-window|$1|1|@0|0
keetui-window|$1|1|@2|1
keetui-window|$2|1|@3|1
keetui-window|$2|1|@0|0
keetui-client|/dev/pts/8|$0|attached,focused,UTF-8
keetui-client|/dev/pts/10|$1|attached,focused,UTF-8
keetui-client|/dev/pts/12|$2|attached,focused,UTF-8
Terminal 0: xterm-256color for /dev/pts/12, flags=0x20:
 191: kUP7: (string) \033[1;7A
 192: Ms: (string) \033]52;%p1%s;%p2%s\a
 193: ol: [missing]
Terminal 1: xterm-256color for /dev/pts/10, flags=0x20:
 191: kUP7: (string) \033[1;7A
 192: Ms: (string) \033]52;%p1%s;%p2%s\a
 193: ol: [missing]
Terminal 2: xterm-256color for /dev/pts/8, flags=0x20:
 191: kUP7: (string) \033[1;7A
 192: Ms: (string) \033]52;%p1%s;%p2%s\a
 193: ol: [missing]
`,
	"3.5a default": `keetui-probe|3.5a|external|off|/dev/pts/1|xterm-256color|1|1|1|0|1||@0|
keetui-window|$0|1|@0|1
keetui-client|/dev/pts/1|$0|attached,focused,UTF-8
Terminal 0: xterm-256color for /dev/pts/1, flags=0x20:
 192: kUP7: (string) \033[1;7A
 193: Ms: (string) \033]52;%p1%s;%p2%s\a
 194: Nobr: [missing]
`,
	"3.5a two clients": `keetui-probe|3.5a|external|on|/dev/pts/3|xterm-256color|2|1|2|0|1||@0|
keetui-window|$0|2|@0|1
keetui-client|/dev/pts/2|$0|attached,focused,UTF-8
keetui-client|/dev/pts/3|$0|attached,focused,UTF-8
Terminal 0: xterm-256color for /dev/pts/3, flags=0x20:
 192: kUP7: (string) \033[1;7A
 193: Ms: (string) \033]52;%p1%s;%p2%s\a
 194: Nobr: [missing]
Terminal 1: screen-256color for /dev/pts/2, flags=0x20:
 192: kUP7: [missing]
 193: Ms: [missing]
 194: Nobr: [missing]
`,
	// show-messages failed: no current client (exit 1).
	"3.5a detached": `keetui-probe|3.5a|external|on|||0|1|0|0|1||@0|
keetui-window|$0|0|@0|1
`,
	"3.5a hidden": `keetui-probe|3.5a|external|on|/dev/pts/4|xterm-256color|1|0|0|0|1||@0|
keetui-window|$0|1|@0|0
keetui-window|$0|1|@1|1
keetui-client|/dev/pts/4|$0|attached,focused,UTF-8
Terminal 0: xterm-256color for /dev/pts/4, flags=0x20:
 192: kUP7: (string) \033[1;7A
 193: Ms: (string) \033]52;%p1%s;%p2%s\a
 194: Nobr: [missing]
`,
	"3.5a zoomed": `keetui-probe|3.5a|external|on|/dev/pts/5|xterm-256color|1|1|1|1|0||@0|
keetui-window|$0|1|@0|1
keetui-client|/dev/pts/5|$0|attached,focused,UTF-8
Terminal 0: xterm-256color for /dev/pts/5, flags=0x20:
 192: kUP7: (string) \033[1;7A
 193: Ms: (string) \033]52;%p1%s;%p2%s\a
 194: Nobr: [missing]
`,
	"3.5a copy mode": `keetui-probe|3.5a|external|on|/dev/pts/6|xterm-256color|1|1|1|0|1|copy-mode|@0|
keetui-window|$0|1|@0|1
keetui-client|/dev/pts/6|$0|attached,focused,UTF-8
Terminal 0: xterm-256color for /dev/pts/6, flags=0x20:
 192: kUP7: (string) \033[1;7A
 193: Ms: (string) \033]52;%p1%s;%p2%s\a
 194: Nobr: [missing]
`,
	"3.5a suspended": `keetui-probe|3.5a|external|on|/dev/pts/7|xterm-256color|1|1|1|0|1||@0|
keetui-window|$0|1|@0|1
keetui-client|/dev/pts/7|$0|attached,focused,suspended,UTF-8
Terminal 0: xterm-256color for /dev/pts/7, flags=0x20:
 192: kUP7: (string) \033[1;7A
 193: Ms: (string) \033]52;%p1%s;%p2%s\a
 194: Nobr: [missing]
`,
	"3.5a grouped": `keetui-probe|3.5a|external|all|/dev/pts/12|xterm-256color|1|0|1|0|1||@0|
keetui-window|$0|1|@0|1
keetui-window|$0|1|@2|0
keetui-window|$1|1|@0|0
keetui-window|$1|1|@2|1
keetui-window|$2|1|@3|1
keetui-window|$2|1|@0|0
keetui-client|/dev/pts/8|$0|attached,focused,UTF-8
keetui-client|/dev/pts/10|$1|attached,focused,UTF-8
keetui-client|/dev/pts/12|$2|attached,focused,UTF-8
Terminal 0: xterm-256color for /dev/pts/12, flags=0x20:
 192: kUP7: (string) \033[1;7A
 193: Ms: (string) \033]52;%p1%s;%p2%s\a
 194: Nobr: [missing]
Terminal 1: xterm-256color for /dev/pts/10, flags=0x20:
 192: kUP7: (string) \033[1;7A
 193: Ms: (string) \033]52;%p1%s;%p2%s\a
 194: Nobr: [missing]
Terminal 2: xterm-256color for /dev/pts/8, flags=0x20:
 192: kUP7: (string) \033[1;7A
 193: Ms: (string) \033]52;%p1%s;%p2%s\a
 194: Nobr: [missing]
`,
	"3.7c default": `keetui-probe|3.7c|external|off|/dev/pts/1|xterm-256color|1|1|1|0|1||@0|
keetui-window|$0|1|@0|1
keetui-client|/dev/pts/1|$0|attached,focused,UTF-8
Terminal 0: xterm-256color for /dev/pts/1, flags=0x20:
 192: kUP7: (string) \033[1;7A
 193: Ms: (string) \033]52;%p1%s;%p2%s\a
 194: Nobr: [missing]
`,
	"3.7c two clients": `keetui-probe|3.7c|external|on|/dev/pts/3|xterm-256color|2|1|2|0|1||@0|
keetui-window|$0|2|@0|1
keetui-client|/dev/pts/2|$0|attached,focused,UTF-8
keetui-client|/dev/pts/3|$0|attached,focused,UTF-8
Terminal 0: xterm-256color for /dev/pts/3, flags=0x20:
 192: kUP7: (string) \033[1;7A
 193: Ms: (string) \033]52;%p1%s;%p2%s\a
 194: Nobr: [missing]
Terminal 1: screen-256color for /dev/pts/2, flags=0x20:
 192: kUP7: [missing]
 193: Ms: [missing]
 194: Nobr: [missing]
`,
	"3.7c detached": `keetui-probe|3.7c|external|on|||0|1|0|0|1||@0|
keetui-window|$0|0|@0|1
`,
	"3.7c hidden": `keetui-probe|3.7c|external|on|/dev/pts/4|xterm-256color|1|0|0|0|1||@0|
keetui-window|$0|1|@0|0
keetui-window|$0|1|@1|1
keetui-client|/dev/pts/4|$0|attached,focused,UTF-8
Terminal 0: xterm-256color for /dev/pts/4, flags=0x20:
 192: kUP7: (string) \033[1;7A
 193: Ms: (string) \033]52;%p1%s;%p2%s\a
 194: Nobr: [missing]
`,
	"3.7c zoomed": `keetui-probe|3.7c|external|on|/dev/pts/5|xterm-256color|1|1|1|1|0||@0|
keetui-window|$0|1|@0|1
keetui-client|/dev/pts/5|$0|attached,focused,UTF-8
Terminal 0: xterm-256color for /dev/pts/5, flags=0x20:
 192: kUP7: (string) \033[1;7A
 193: Ms: (string) \033]52;%p1%s;%p2%s\a
 194: Nobr: [missing]
`,
	"3.7c copy mode": `keetui-probe|3.7c|external|on|/dev/pts/6|xterm-256color|1|1|1|0|1|copy-mode|@0|
keetui-window|$0|1|@0|1
keetui-client|/dev/pts/6|$0|attached,focused,UTF-8
Terminal 0: xterm-256color for /dev/pts/6, flags=0x20:
 192: kUP7: (string) \033[1;7A
 193: Ms: (string) \033]52;%p1%s;%p2%s\a
 194: Nobr: [missing]
`,
	"3.7c suspended": `keetui-probe|3.7c|external|on|/dev/pts/7|xterm-256color|1|1|1|0|1||@0|
keetui-window|$0|1|@0|1
Terminal 0: xterm-256color for /dev/pts/7, flags=0x20:
 192: kUP7: (string) \033[1;7A
 193: Ms: (string) \033]52;%p1%s;%p2%s\a
 194: Nobr: [missing]
`,
	"3.7c grouped": `keetui-probe|3.7c|external|all|/dev/pts/12|xterm-256color|1|0|1|0|1||@0|
keetui-window|$0|1|@0|1
keetui-window|$0|1|@2|0
keetui-window|$1|1|@0|0
keetui-window|$1|1|@2|1
keetui-window|$2|1|@3|1
keetui-window|$2|1|@0|0
keetui-client|/dev/pts/8|$0|attached,focused,UTF-8
keetui-client|/dev/pts/10|$1|attached,focused,UTF-8
keetui-client|/dev/pts/12|$2|attached,focused,UTF-8
Terminal 0: xterm-256color for /dev/pts/12, flags=0x20:
 192: kUP7: (string) \033[1;7A
 193: Ms: (string) \033]52;%p1%s;%p2%s\a
 194: Nobr: [missing]
Terminal 1: xterm-256color for /dev/pts/10, flags=0x20:
 192: kUP7: (string) \033[1;7A
 193: Ms: (string) \033]52;%p1%s;%p2%s\a
 194: Nobr: [missing]
Terminal 2: xterm-256color for /dev/pts/8, flags=0x20:
 192: kUP7: (string) \033[1;7A
 193: Ms: (string) \033]52;%p1%s;%p2%s\a
 194: Nobr: [missing]
`,
	"3.8 default": `keetui-probe|3.8|external|off|/dev/pts/1|xterm-256color|1|1|1|0|1||@0|
keetui-window|$0|1|@0|1
keetui-client|/dev/pts/1|$0|attached,focused,UTF-8
Terminal 0: xterm-256color for /dev/pts/1, flags=0x20:
 195: kUP7: (string) \033[1;7A
 196: Ms: (string) \033]52;%p1%s;%p2%s\a
 197: Nobr: [missing]
`,
	"3.8 two clients": `keetui-probe|3.8|external|on|/dev/pts/3|xterm-256color|2|1|2|0|1||@0|
keetui-window|$0|2|@0|1
keetui-client|/dev/pts/2|$0|attached,focused,UTF-8
keetui-client|/dev/pts/3|$0|attached,focused,UTF-8
Terminal 0: xterm-256color for /dev/pts/3, flags=0x20:
 195: kUP7: (string) \033[1;7A
 196: Ms: (string) \033]52;%p1%s;%p2%s\a
 197: Nobr: [missing]
Terminal 1: screen-256color for /dev/pts/2, flags=0x20:
 195: kUP7: [missing]
 196: Ms: [missing]
 197: Nobr: [missing]
`,
	"3.8 detached": `keetui-probe|3.8|external|on|||0|1|0|0|1||@0|
keetui-window|$0|0|@0|1
`,
	"3.8 hidden": `keetui-probe|3.8|external|on|/dev/pts/4|xterm-256color|1|0|0|0|1||@0|
keetui-window|$0|1|@0|0
keetui-window|$0|1|@1|1
keetui-client|/dev/pts/4|$0|attached,focused,UTF-8
Terminal 0: xterm-256color for /dev/pts/4, flags=0x20:
 195: kUP7: (string) \033[1;7A
 196: Ms: (string) \033]52;%p1%s;%p2%s\a
 197: Nobr: [missing]
`,
	"3.8 zoomed": `keetui-probe|3.8|external|on|/dev/pts/5|xterm-256color|1|1|1|1|0||@0|
keetui-window|$0|1|@0|1
keetui-client|/dev/pts/5|$0|attached,focused,UTF-8
Terminal 0: xterm-256color for /dev/pts/5, flags=0x20:
 195: kUP7: (string) \033[1;7A
 196: Ms: (string) \033]52;%p1%s;%p2%s\a
 197: Nobr: [missing]
`,
	"3.8 copy mode": `keetui-probe|3.8|external|on|/dev/pts/6|xterm-256color|1|1|1|0|1|copy-mode|@0|
keetui-window|$0|1|@0|1
keetui-client|/dev/pts/6|$0|attached,focused,UTF-8
Terminal 0: xterm-256color for /dev/pts/6, flags=0x20:
 195: kUP7: (string) \033[1;7A
 196: Ms: (string) \033]52;%p1%s;%p2%s\a
 197: Nobr: [missing]
`,
	"3.8 suspended": `keetui-probe|3.8|external|on|/dev/pts/7|xterm-256color|1|1|1|0|1||@0|
keetui-window|$0|1|@0|1
Terminal 0: xterm-256color for /dev/pts/7, flags=0x20:
 195: kUP7: (string) \033[1;7A
 196: Ms: (string) \033]52;%p1%s;%p2%s\a
 197: Nobr: [missing]
`,
	"3.8 grouped": `keetui-probe|3.8|external|all|/dev/pts/12|xterm-256color|1|0|1|0|1||@0|
keetui-window|$0|1|@0|1
keetui-window|$0|1|@2|0
keetui-window|$1|1|@0|0
keetui-window|$1|1|@2|1
keetui-window|$2|1|@3|1
keetui-window|$2|1|@0|0
keetui-client|/dev/pts/8|$0|attached,focused,UTF-8
keetui-client|/dev/pts/10|$1|attached,focused,UTF-8
keetui-client|/dev/pts/12|$2|attached,focused,UTF-8
Terminal 0: xterm-256color for /dev/pts/12, flags=0x20:
 195: kUP7: (string) \033[1;7A
 196: Ms: (string) \033]52;%p1%s;%p2%s\a
 197: Nobr: [missing]
Terminal 1: xterm-256color for /dev/pts/10, flags=0x20:
 195: kUP7: (string) \033[1;7A
 196: Ms: (string) \033]52;%p1%s;%p2%s\a
 197: Nobr: [missing]
Terminal 2: xterm-256color for /dev/pts/8, flags=0x20:
 195: kUP7: (string) \033[1;7A
 196: Ms: (string) \033]52;%p1%s;%p2%s\a
 197: Nobr: [missing]
`,
}

// tmuxFixtureVersion describes a captured server, independently of the
// parser: allow-passthrough as printed by default, when on, and in the
// grouped capture (3.0a and 3.2a have no such option and always allow it),
// and whether it is 3.2 or later, with window_active_clients, client_flags
// and client ttys in show-messages -T.
type tmuxFixtureVersion struct {
	name                     string
	major, minor             int
	ptDefault, ptOn, ptGroup string
	modern                   bool
}

var tmuxFixtureVersions = []tmuxFixtureVersion{
	{"3.0a", 3, 0, "", "", "", false},
	{"3.2a", 3, 2, "", "", "", true},
	{"3.3a", 3, 3, "0", "1", "1", true},
	{"3.5a", 3, 5, "off", "on", "all", true},
	{"3.7c", 3, 7, "off", "on", "all", true},
	{"3.8", 3, 8, "off", "on", "all", true},
}

type tmuxFixtureWant struct {
	passthrough        string
	tty                string
	attached           int
	active             bool
	windowClients      int
	zoomed, paneActive bool
	mode               string
	ms                 bool
	ttys               []string
	viewers, holders   int
	route              terminalRoute // 0: an error
	reached            int           // clients a passthrough copy reports, when more than one
	recipients         []string      // the ttys a passthrough copy records
	reason             string        // noClear, or the error's reason
	clear              clearOutcome  // clearing a passthrough copy made to tty
}

func tmuxFixtureWants(v tmuxFixtureVersion) map[string]tmuxFixtureWant {
	wc := func(n int) int {
		if v.modern {
			return n
		}
		return 0
	}
	// What passthrough cannot carry goes by load-buffer -w, or fails before 3.2.
	plan := func(w tmuxFixtureWant, reason string) tmuxFixtureWant {
		w.reason = reason
		if v.modern {
			w.route = routeTmuxLoadBuffer
		} else {
			w.reason += " and tmux " + v.name + " has no load-buffer -w"
		}
		return w
	}
	passthrough := func(w tmuxFixtureWant, recipients ...string) tmuxFixtureWant {
		w.route, w.recipients = routeTmuxPassthrough, recipients
		return w
	}
	one := func(dev string) tmuxFixtureWant {
		return tmuxFixtureWant{passthrough: v.ptOn, tty: dev, attached: 1, active: true, windowClients: wc(1),
			paneActive: true, ms: true, ttys: []string{dev}, viewers: 1, holders: 1, clear: clearDeferred}
	}
	def := one("/dev/pts/1")
	def.passthrough, def.clear = v.ptDefault, clearRequested
	if v.ptDefault == "" {
		def = passthrough(def, "/dev/pts/1")
	} else {
		def = plan(def, "allow-passthrough off")
		def.clear = clearUnsupported
	}
	// list-clients lists the screen client, attached first, first.
	two := one("/dev/pts/3")
	two.attached, two.windowClients, two.ttys, two.clear = 2, wc(2), []string{"/dev/pts/2", "/dev/pts/3"}, clearRequested
	two.viewers, two.holders = 2, 2
	if v.modern {
		two = plan(two, "2 clients share the window")
	} else {
		two = passthrough(two, "/dev/pts/2", "/dev/pts/3")
		two.reached = 2
	}
	detached := tmuxFixtureWant{passthrough: v.ptOn, active: true, paneActive: true, clear: clearDeferred}
	detached.reason = "no client attached"
	if !v.modern {
		detached.reason += " and tmux " + v.name + " has no load-buffer -w"
	}
	hidden := one("/dev/pts/4")
	hidden.active, hidden.windowClients, hidden.viewers = false, 0, 0
	zoomed := one("/dev/pts/5")
	zoomed.zoomed, zoomed.paneActive = true, false
	copyMode := one("/dev/pts/6")
	copyMode.mode = "copy-mode"
	// session_attached has not caught up with the suspend; client_flags
	// has. Before 3.2 nothing else tells, and tmux drops the clear.
	suspended := one("/dev/pts/7")
	if v.modern {
		suspended.viewers, suspended.holders = 0, 0
		suspended = plan(suspended, "no client attached")
	} else {
		suspended = passthrough(suspended, "/dev/pts/7")
		suspended.clear = clearRequested
	}
	// tmux picked the third session's client, which shows another window;
	// the copy goes to the main session's client. A clear for a copy made
	// to the third waits for it to show keetui's window, unless "all"
	// reaches it already.
	grouped := one("/dev/pts/12")
	grouped.passthrough, grouped.active, grouped.ttys = v.ptGroup, false, []string{"/dev/pts/8", "/dev/pts/10", "/dev/pts/12"}
	grouped.holders, grouped.clear = 3, clearRequested
	if v.ptGroup == "all" {
		grouped = plan(grouped, "3 clients share the window")
	} else {
		grouped = passthrough(grouped, "/dev/pts/8")
		grouped.clear = clearAway
	}
	return map[string]tmuxFixtureWant{
		"default":     def,
		"two clients": two,
		"detached":    detached,
		"hidden":      plan(hidden, "keetui's window is hidden"),
		"zoomed":      plan(zoomed, "another pane is zoomed"),
		"copy mode":   plan(copyMode, "keetui's pane is in copy-mode"),
		"suspended":   suspended,
		"grouped":     grouped,
	}
}

func TestParseTmuxProbeFixtures(t *testing.T) {
	checked := 0
	for _, v := range tmuxFixtureVersions {
		for state, w := range tmuxFixtureWants(v) {
			name := v.name + " " + state
			t.Run(name, func(t *testing.T) {
				out, ok := tmuxFixtures[name]
				if !ok {
					t.Fatal("no fixture")
				}
				p, err := parseTmuxProbe([]byte(out))
				if err != nil {
					t.Fatal(err)
				}
				if p.major != v.major || p.minor != v.minor || p.passthrough != w.passthrough || p.clientTTY != w.tty ||
					p.attached != w.attached || p.windowActive != w.active || p.windowClients != w.windowClients ||
					p.windowZoomed != w.zoomed || p.paneActive != w.paneActive || p.paneMode != w.mode ||
					p.clientMs != w.ms || !slices.Equal(p.clientTTYs, w.ttys) || p.windowID != "@0" ||
					p.viewers != w.viewers || p.holders != w.holders {
					t.Fatalf("probe = %+v", p)
				}
				if p.setClipboard != "external" || w.tty != "" && p.clientTerm != "xterm-256color" || p.nested() {
					t.Fatalf("probe = %+v", p)
				}
				res, err := planTmuxCopy(p)
				if w.route == 0 {
					var routeErr *tmuxRouteError
					if !errors.As(err, &routeErr) || routeErr.reason != w.reason {
						t.Fatalf("plan = %+v, %v", res, err)
					}
				} else if err != nil || res.route != w.route || res.noClear != w.reason || res.clients != w.reached ||
					!slices.Equal(res.reached, w.recipients) {
					t.Fatalf("plan = %+v, %v", res, err)
				}
				f := &fakeTmux{probe: out}
				last := terminalResult{route: routeTmuxPassthrough}
				if w.tty != "" {
					last.reached = []string{w.tty}
				}
				if outcome, err := f.clipboard("%0").clear(last); outcome != w.clear || err != nil {
					t.Fatalf("clear = %v, %v, want %v", outcome, err, w.clear)
				}
			})
			checked++
		}
	}
	if checked != len(tmuxFixtures) {
		t.Fatalf("checked %d of %d fixtures", checked, len(tmuxFixtures))
	}
}

func TestParseTmuxProbeIsDefensive(t *testing.T) {
	for _, bad := range []string{
		"",
		"error connecting to /tmp/tmux-0/default\n",
		"keetui-probe|3.8|external|on\n",                              // too few fields
		"keetui-probe|3.8|external|on|/dev/pts/1|xterm|1|1|1|\n",      // the format before zoom and mode
		"keetui-probe|3.8|external|on|/dev/pts/1|xterm|1|1|1|0|1||\n", // the format before window_id
		"probe|3.8|external|on|/dev/pts/1|xterm|1|1|1|0|1||@1|\n",     // wrong marker
		"Terminal 0: xterm for /dev/pts/1, flags=0x20:\n 1: Ms\n",     // no probe line
	} {
		if _, err := parseTmuxProbe([]byte(bad)); err == nil {
			t.Errorf("parseTmuxProbe(%q) accepted", bad)
		}
	}

	// The terminal's own XTVERSION reply comes last and may hold the
	// separator; an unknown TERM's Ms belongs to that client only.
	p, err := parseTmuxProbe([]byte("keetui-probe|next-3.6|on|all|/dev/pts/2|tmux-256color|1|1|1|0|1||@3|tmux 3.6|x\n" +
		"Terminal 0: xterm-256color for /dev/pts/9, flags=0x20:\n 193: Ms: (string) x\n" +
		"Terminal 1: tmux-256color for /dev/pts/2, flags=0x20:\n 193: Ms: [missing]\n"))
	if err != nil || p.major != 3 || p.minor != 6 || p.clientType != "tmux 3.6|x" || p.clientMs || !p.nested() ||
		!slices.Equal(p.clientTTYs, []string{"/dev/pts/9", "/dev/pts/2"}) {
		t.Fatalf("probe = %+v, %v", p, err)
	}
	for version, known := range map[string]bool{"3.4": true, "3.10": true, "openbsd-7.4": false, "master": false} {
		p, err := parseTmuxProbe([]byte("keetui-probe|" + version + "|external|on|/dev/pts/1|xterm|1|1|1|0|1||@1|\n"))
		if err != nil || (p.major != 0) != known {
			t.Errorf("version %q parsed as %d.%d (%v)", version, p.major, p.minor, err)
		}
	}
	if p, _ := parseTmuxProbe([]byte("keetui-probe|3.10|external|on|/dev/pts/1|xterm|1|1|1|0|1||@1|\n")); !p.atLeast(3, 4) || p.atLeast(4, 0) {
		t.Fatal("3.10 compares as an older release")
	}

	// Rows for other windows and malformed rows count no client; without a
	// row for keetui's window the probe line's counts stand.
	p, err = parseTmuxProbe([]byte("keetui-probe|3.8|external|on|/dev/pts/1|xterm|1|1|1|0|1||@1|\n" +
		"keetui-window|$1|1|@1|1|extra\nkeetui-window|$1|1|@2|1\n" +
		"keetui-client|/dev/pts/1|$1|attached\nkeetui-client|/dev/pts/2\nkeetui-client||$1|attached\n"))
	if err != nil || p.viewers != 1 || p.holders != 1 || !slices.Equal(p.awakeTTYs, []string{"/dev/pts/1"}) ||
		!slices.Equal(p.viewerTTYs, []string{"/dev/pts/1"}) || !slices.Equal(p.clientTTYs, []string{"/dev/pts/1"}) || p.listed {
		t.Fatalf("probe = %+v, %v", p, err)
	}
	// A window linked twice into one session counts its clients once.
	p, err = parseTmuxProbe([]byte("keetui-probe|3.8|external|all|/dev/pts/1|xterm|1|1|1|0|1||@1|\n" +
		"keetui-window|$1|1|@1|1\nkeetui-window|$1|1|@1|0\nkeetui-client|/dev/pts/1|$1|attached,UTF-8\n"))
	if err != nil || p.viewers != 1 || p.holders != 1 || p.recipient() != "/dev/pts/1" {
		t.Fatalf("probe = %+v, %v", p, err)
	}
}

// The whole option matrix, against the rules stated independently: never a
// raw OSC 52; passthrough when tmux forwards it now (preferring a targeted
// load-buffer over a broadcast); load-buffer with 3.2+ and Ms; else an
// error naming the reason.
func TestPlanTmuxCopyMatrix(t *testing.T) {
	versions := map[string][]string{
		"3.0a": {""}, "3.2a": {""}, "3.3a": {"0", "1"}, "3.4": {"off", "on", "all"},
		"3.5a": {"off", "on", "all"}, "3.6a": {"off", "on", "all"}, "3.7c": {"off", "on", "all"}, "3.8": {"off", "on", "all"},
	}
	type pane struct {
		zoomed, active bool
		mode           string
	}
	panes := []pane{{false, true, ""}, {true, true, ""}, {true, false, ""}, {false, false, ""}, {false, true, "copy-mode"}, {false, true, "tree-mode"}}
	cases := 0
	for version, options := range versions {
		v, _ := parseTmuxProbe([]byte("keetui-probe|" + version + "|external||||0|0|0|0|1||@1|\n"))
		for _, pt := range options {
			for viewers := range 3 {
				// Clients of sessions that hold the window without showing
				// it (grouped or linked), which only "all" reaches.
				for _, others := range []int{0, 1} {
					// Clients awake in any session; a popup's is among them.
					for awake := range 3 {
						for _, pn := range panes {
							for _, ms := range []bool{true, false} {
								for _, popup := range []bool{true, false} {
									for _, nested := range []bool{true, false} {
										p := tmuxProbe{version: version, major: v.major, minor: v.minor, passthrough: pt,
											viewers: viewers, holders: viewers + others, clientMs: ms, popup: popup,
											windowZoomed: pn.zoomed, paneActive: pn.active, paneMode: pn.mode,
											clientTerm: "xterm-256color", attached: viewers, windowActive: viewers > 0,
											viewerTTYs: []string{"/dev/pts/2", "/dev/pts/5"}[:viewers],
											holderTTYs: []string{"/dev/pts/3", "/dev/pts/6", "/dev/pts/9"}[:viewers+others],
											listed:     true}
										if awake > 0 || viewers+others > 0 {
											p.clientTTY = "/dev/pts/1"
										}
										switch awake {
										case 1:
											// Sometimes another client than the one tmux picks.
											p.awakeTTYs = []string{[]string{"/dev/pts/1", "/dev/pts/4"}[viewers%2]}
										case 2:
											p.awakeTTYs = []string{"/dev/pts/1", "/dev/pts/4"}
										}
										if nested {
											p.clientTerm, p.clientType = "tmux-256color", "tmux "+version
										}
										checkTmuxPlan(t, p)
										cases++
									}
								}
							}
						}
					}
				}
			}
		}
	}
	if cases < 10000 {
		t.Fatalf("only %d cases", cases)
	}
}

func checkTmuxPlan(t *testing.T, p tmuxProbe) {
	t.Helper()
	before := func(major, minor int) bool { return p.major < major || p.major == major && p.minor < minor }
	allowed := before(3, 3) || slices.Contains([]string{"on", "1", "all"}, p.passthrough)
	popupIgnored := p.popup && !before(3, 3) && before(3, 7)
	// A popup has no pane, and its passthrough goes to its own client;
	// otherwise tmux forwards nothing from a pane in a mode, and with "on"
	// nothing from a pane it does not draw.
	inMode := !p.popup && p.paneMode != ""
	drawn := !p.windowZoomed || p.paneActive
	recipients, ttys := 0, []string(nil)
	switch {
	case !allowed || popupIgnored || inMode:
	case p.popup:
		// One of the clients awake, unnamed unless there is one.
		recipients, ttys = min(len(p.awakeTTYs), 1), p.awakeTTYs
	case p.passthrough == "all":
		recipients, ttys = p.holders, p.holderTTYs
	case drawn:
		recipients, ttys = p.viewers, p.viewerTTYs
	}
	recipient := ""
	if recipients == 1 && len(ttys) == 1 {
		recipient = ttys[0]
	}
	// A copy records the terminals it reached, for its clear to wait for.
	var reached []string
	switch {
	case recipient != "":
		reached = []string{recipient}
	case recipients > 1:
		reached = ttys
	}
	// Nothing names a popup's client: load-buffer needs it to be the one
	// client awake.
	targeted := !before(3, 2) && p.clientTTY != "" && p.clientMs && (!p.popup || slices.Equal(p.awakeTTYs, []string{p.clientTTY}))

	res, err := planTmuxCopy(p)
	label := fmt.Sprintf("%+v -> %+v, %v", p, res, err)
	switch {
	case recipients > 1 && targeted:
		if err != nil || res.route != routeTmuxLoadBuffer || res.clearable() || res.noClear != fmt.Sprintf("%d clients share the window", recipients) {
			t.Fatal("shared window not targeted at one client: " + label)
		}
	case recipients > 0:
		if err != nil || res.route != routeTmuxPassthrough || res.clearable() == p.nested() {
			t.Fatal("passthrough expected: " + label)
		}
		if recipients > 1 && res.clients != recipients || recipients == 1 && res.clients != 0 || !slices.Equal(res.reached, reached) {
			t.Fatal("recipients misreported: " + label)
		}
	case targeted:
		if err != nil || res.route != routeTmuxLoadBuffer || res.clearable() {
			t.Fatal("load-buffer expected: " + label)
		}
	default:
		var routeErr *tmuxRouteError
		if !errors.As(err, &routeErr) || routeErr.reason == "" || !strings.HasPrefix(err.Error(), "tmux can't reach the terminal clipboard: ") {
			t.Fatal("error expected: " + label)
		}
		return
	}
	// The recipient's terminal is known only when it is the client tmux
	// picks, the one the probe describes.
	id := ""
	if res.route == routeTmuxPassthrough && recipients == 1 && recipient != "" && recipient == p.clientTTY {
		id = p.clientTerm + "\x00" + p.clientType
	}
	if res.nested != p.nested() || res.termName != p.clientTerm || res.clientID != id {
		t.Fatal("client terminal not recorded: " + label)
	}
}

func TestPlanTmuxCopyReasons(t *testing.T) {
	one := []string{"/dev/pts/1"}
	visible := tmuxProbe{version: "3.5a", major: 3, minor: 5, passthrough: "on", attached: 1, windowActive: true,
		windowClients: 1, paneActive: true, clientTTY: "/dev/pts/1", clientTerm: "xterm-256color", clientMs: true,
		viewers: 1, holders: 1, awakeTTYs: one, viewerTTYs: one, holderTTYs: one, listed: true}
	two := []string{"/dev/pts/1", "/dev/pts/2"}
	hidden := func(p *tmuxProbe) { p.windowActive, p.windowClients, p.viewers, p.viewerTTYs = false, 0, 0, nil }
	for _, tc := range []struct {
		name   string
		edit   func(*tmuxProbe)
		route  terminalRoute
		reason string // noClear, or the error reason
	}{
		{"passthrough", func(*tmuxProbe) {}, routeTmuxPassthrough, ""},
		{"all reaches a hidden window", func(p *tmuxProbe) { hidden(p); p.passthrough = "all" }, routeTmuxPassthrough, ""},
		{"nested", func(p *tmuxProbe) { p.clientTerm, p.clientType = "tmux-256color", "tmux 3.8" }, routeTmuxPassthrough, "nested tmux"},
		{"nested in screen", func(p *tmuxProbe) { p.clientTerm, p.clientMs = "screen-256color", false }, routeTmuxPassthrough, "nested tmux"},
		{"screen TERM, terminal named itself", func(p *tmuxProbe) { p.clientTerm, p.clientType = "screen-256color", "XTerm(390)" }, routeTmuxPassthrough, ""},
		// 3.0a reports a device class there, "VT100" for a client nested
		// in tmux 3.0a.
		{"3.0a device class", func(p *tmuxProbe) {
			p.version, p.major, p.minor, p.passthrough, p.windowClients = "3.0a", 3, 0, "", 0
			p.clientTerm, p.clientType = "screen", "VT100"
		}, routeTmuxPassthrough, "nested tmux"},
		{"3.0a xterm", func(p *tmuxProbe) {
			p.version, p.major, p.minor, p.passthrough, p.windowClients = "3.0a", 3, 0, "", 0
			p.clientType = "VT220"
		}, routeTmuxPassthrough, ""},
		{"off", func(p *tmuxProbe) { p.passthrough = "off" }, routeTmuxLoadBuffer, "allow-passthrough off"},
		{"popup 3.5a", func(p *tmuxProbe) { p.popup = true }, routeTmuxLoadBuffer, "tmux 3.5a ignores passthrough from popups"},
		{"popup 3.7c", func(p *tmuxProbe) { p.popup, p.version, p.minor = true, "3.7c", 7 }, routeTmuxPassthrough, ""},
		// A popup's passthrough goes to its own client, however many show
		// the window under it.
		{"popup 3.7c, window shared", func(p *tmuxProbe) {
			p.popup, p.version, p.minor, p.viewers, p.holders, p.viewerTTYs, p.holderTTYs = true, "3.7c", 7, 2, 2, two, two
		}, routeTmuxPassthrough, ""},
		{"popup of a suspended client", func(p *tmuxProbe) { p.popup, p.version, p.minor, p.awakeTTYs = true, "3.7c", 7, nil },
			0, "no client attached"},
		// Keys typed in a popup leave tmux's choice of client unchanged,
		// and nothing names the popup's: load-buffer could pick another
		// terminal.
		{"popup 3.5a, two clients", func(p *tmuxProbe) { p.popup, p.awakeTTYs = true, two }, 0,
			"tmux 3.5a ignores passthrough from popups and tmux can't tell keetui which client shows its popup"},
		{"popup 3.5a, the client awake is another", func(p *tmuxProbe) { p.popup, p.awakeTTYs = true, []string{"/dev/pts/4"} }, 0,
			"tmux 3.5a ignores passthrough from popups and tmux can't tell keetui which client shows its popup"},
		{"off without Ms", func(p *tmuxProbe) { p.passthrough, p.clientTerm, p.clientMs = "off", "screen-256color", false }, 0,
			"allow-passthrough off and no clipboard support for screen-256color"},
		{"hidden without Ms", func(p *tmuxProbe) { hidden(p); p.clientMs = false }, 0,
			"keetui's window is hidden and no clipboard support for xterm-256color"},
		{"detached", func(p *tmuxProbe) {
			hidden(p)
			p.attached, p.holders, p.holderTTYs, p.clientTTY, p.clientTerm, p.awakeTTYs = 0, 0, nil, "", "", nil
		}, 0, "no client attached"},
		{"off and detached", func(p *tmuxProbe) {
			hidden(p)
			p.passthrough, p.attached, p.holders, p.holderTTYs, p.clientTTY, p.awakeTTYs = "off", 0, 0, nil, "", nil
		}, 0, "allow-passthrough off and no client attached"},
		// From 3.2 client_flags names a suspended client, which gets no
		// passthrough; session_attached may still count it.
		{"only client suspended", func(p *tmuxProbe) {
			p.viewers, p.holders, p.viewerTTYs, p.holderTTYs, p.awakeTTYs = 0, 0, nil, nil, nil
		}, routeTmuxLoadBuffer, "no client attached"},
		{"another pane zoomed", func(p *tmuxProbe) { p.windowZoomed, p.paneActive = true, false }, routeTmuxLoadBuffer, "another pane is zoomed"},
		{"keetui's pane zoomed", func(p *tmuxProbe) { p.windowZoomed = true }, routeTmuxPassthrough, ""},
		{"another pane active", func(p *tmuxProbe) { p.paneActive = false }, routeTmuxPassthrough, ""},
		{"all reaches past a zoomed pane", func(p *tmuxProbe) { p.passthrough, p.windowZoomed, p.paneActive = "all", true, false }, routeTmuxPassthrough, ""},
		{"zoomed and hidden", func(p *tmuxProbe) {
			hidden(p)
			p.windowZoomed, p.paneActive = true, false
		}, routeTmuxLoadBuffer,
			"keetui's window is hidden"},
		{"copy mode", func(p *tmuxProbe) { p.paneMode = "copy-mode" }, routeTmuxLoadBuffer, "keetui's pane is in copy-mode"},
		{"all stops at copy mode", func(p *tmuxProbe) { p.passthrough, p.paneMode = "all", "tree-mode" }, routeTmuxLoadBuffer, "keetui's pane is in tree-mode"},
		{"copy mode without Ms", func(p *tmuxProbe) { p.paneMode, p.clientMs = "copy-mode", false }, 0,
			"keetui's pane is in copy-mode and no clipboard support for xterm-256color"},
		// The probe of a popup describes the pane under it.
		{"popup over copy mode", func(p *tmuxProbe) {
			p.popup, p.version, p.minor, p.paneMode, p.windowZoomed, p.paneActive = true, "3.7c", 7, "copy-mode", true, false
		}, routeTmuxPassthrough, ""},
		// tmux probes the most recently active session holding the window;
		// the counts come from every session's clients.
		{"grouped session shows it", func(p *tmuxProbe) { p.windowActive = false }, routeTmuxPassthrough, ""},
		{"only a grouped session is attached", func(p *tmuxProbe) { p.attached = 0 }, routeTmuxPassthrough, ""},
		{"on skips a grouped session elsewhere", func(p *tmuxProbe) { p.holders, p.holderTTYs = 2, two }, routeTmuxPassthrough, ""},
		{"all reaches a grouped session elsewhere", func(p *tmuxProbe) { p.passthrough, p.holders, p.holderTTYs = "all", 2, two },
			routeTmuxLoadBuffer, "2 clients share the window"},
		{"unknown version", func(p *tmuxProbe) { p.version, p.major, p.minor, p.passthrough = "openbsd-7.4", 0, 0, "off" }, 0,
			"allow-passthrough off and unrecognized tmux version openbsd-7.4"},
		{"three clients", func(p *tmuxProbe) {
			p.attached, p.windowClients, p.viewers, p.holders = 3, 3, 3, 3
			p.viewerTTYs = []string{"/dev/pts/1", "/dev/pts/2", "/dev/pts/3"}
			p.holderTTYs, p.awakeTTYs = p.viewerTTYs, p.viewerTTYs
		}, routeTmuxLoadBuffer,
			"3 clients share the window"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := visible
			tc.edit(&p)
			res, err := planTmuxCopy(p)
			if tc.route == 0 {
				if err == nil || err.Error() != "tmux can't reach the terminal clipboard: "+tc.reason {
					t.Fatalf("plan = %+v, %v", res, err)
				}
				return
			}
			if err != nil || res.route != tc.route || res.noClear != tc.reason {
				t.Fatalf("plan = %+v, %v", res, err)
			}
		})
	}
}

// A copy that passthrough would bring to several terminals says so, or
// goes to the one client by load-buffer when its terminal has Ms. Before
// 3.2 that is the only route, and grouped sessions showing the window
// count too.
func TestTmuxBroadcastIsReported(t *testing.T) {
	noMs := func(fixture string) string {
		return regexp.MustCompile(`(?m)^( \d+: Ms: ).*$`).ReplaceAllString(tmuxFixtures[fixture], "${1}[missing]")
	}
	// The grouped session shows keetui's window too.
	groupedShows := strings.Replace(tmuxFixtures["3.0a grouped"], "keetui-window|$1|1|@0|0", "keetui-window|$1|1|@0|1", 1)
	for _, tc := range []struct {
		probe   string
		clients int
		status  string
	}{
		{tmuxFixtures["3.0a two clients"], 2, "Sent password to 2 tmux clients' terminal clipboards (OSC 52)"},
		{tmuxFixtures["3.0a default"], 0, "Sent password to the terminal clipboard via tmux (OSC 52)"},
		{tmuxFixtures["3.0a grouped"], 0, "Sent password to the terminal clipboard via tmux (OSC 52)"},
		{groupedShows, 2, "Sent password to 2 tmux clients' terminal clipboards (OSC 52)"},
		// 3.2+ sends to the one client instead when its terminal has Ms.
		{tmuxFixtures["3.8 two clients"], 0, "Sent password via tmux (OSC 52); can't auto-clear: 2 clients share the window"},
		{noMs("3.8 two clients"), 2, "Sent password to 2 tmux clients' terminal clipboards (OSC 52)"},
		// "all" reaches every session holding the window, a grouped one on
		// another window and one that links it included.
		{tmuxFixtures["3.8 grouped"], 0, "Sent password via tmux (OSC 52); can't auto-clear: 3 clients share the window"},
		{noMs("3.8 grouped"), 3, "Sent password to 3 tmux clients' terminal clipboards (OSC 52)"},
		{tmuxFixtures["3.3a grouped"], 0, "Sent password to the terminal clipboard via tmux (OSC 52)"},
	} {
		f := &fakeTmux{probe: tc.probe}
		res, err := f.clipboard("%0").copy([]byte(tmuxSecret))
		if err != nil || res.clients != tc.clients {
			t.Fatalf("copy with %q = %+v, %v", tc.probe, res, err)
		}
		if got := copyStatus("password", copyResult{terminal: true, via: res}); got != tc.status {
			t.Errorf("status = %q, want %q", got, tc.status)
		}
	}
}

type tmuxCall struct {
	args  []string
	stdin []byte
}

// fakeTmux stands in for the tmux client and the controlling terminal.
type fakeTmux struct {
	probe    string
	probeErr error
	loadErr  error
	calls    []tmuxCall
	writes   []string
}

func (f *fakeTmux) run(stdin []byte, args ...string) ([]byte, error) {
	f.calls = append(f.calls, tmuxCall{slices.Clone(args), bytes.Clone(stdin)})
	switch args[0] {
	case "display-message":
		return []byte(f.probe), f.probeErr
	case "load-buffer":
		return nil, f.loadErr
	}
	return nil, nil
}

func (f *fakeTmux) write(seq string) error {
	f.writes = append(f.writes, seq)
	return nil
}

func (f *fakeTmux) clipboard(pane string) tmuxClipboard {
	return tmuxClipboard{run: f.run, write: f.write, pane: pane}
}

// probeText renders a probe answer for attached clients of keetui's
// session, the first on /dev/pts/7, with keetui's pane active and in no
// mode.
func probeText(version, passthrough string, attached int, active bool, ms bool) string {
	return paneProbeText(version, passthrough, attached, active, ms, "0|1|")
}

// paneProbeText takes window_zoomed_flag|pane_active|pane_mode as pane.
func paneProbeText(version, passthrough string, attached int, active bool, ms bool, pane string) string {
	tty, activeFlag, ms52 := "", 0, "[missing]"
	if attached > 0 {
		tty = "/dev/pts/7"
	}
	if active {
		activeFlag = 1
	}
	if ms {
		ms52 = `(string) \033]52;%p1%s;%p2%s\a`
	}
	text := fmt.Sprintf("keetui-probe|%s|external|%s|%s|xterm-256color|%d|%d|%d|%s|@1|\n",
		version, passthrough, tty, attached, activeFlag, min(attached, activeFlag), pane) +
		fmt.Sprintf("keetui-window|$1|%d|@1|%d\n", attached, activeFlag)
	for i := range attached {
		text += fmt.Sprintf("keetui-client|/dev/pts/%d|$1|attached,focused,UTF-8\n", 7+i)
	}
	return text + "Terminal 0: xterm-256color for " + tty + ", flags=0x20:\n 196: Ms: " + ms52 + "\n"
}

const tmuxSecret = "synthetic-Hunter2-secret"

var tmuxBufferName = regexp.MustCompile(`^keetui-[0-9a-f]{16}$`)

func TestTmuxCopyKeepsSecretOnStdin(t *testing.T) {
	for _, tc := range []struct {
		version, passthrough string
		chained              bool
	}{
		{"3.2a", "", false}, {"3.3a", "0", false}, {"3.4", "off", true}, {"3.5a", "off", true}, {"3.8", "off", true},
	} {
		t.Run(tc.version, func(t *testing.T) {
			// Hidden for 3.2a, which always allows passthrough.
			f := &fakeTmux{probe: probeText(tc.version, tc.passthrough, 1, tc.passthrough != "", true)}
			res, err := f.clipboard("%3").copy([]byte(tmuxSecret))
			if err != nil || res.route != routeTmuxLoadBuffer || res.clearable() {
				t.Fatalf("copy = %+v, %v", res, err)
			}
			if len(f.writes) != 0 {
				t.Fatalf("load-buffer copy also wrote the terminal: %q", f.writes)
			}
			probe := f.calls[0]
			if probe.stdin != nil || !slices.Equal(probe.args[:4], []string{"display-message", "-p", "-t", "%3"}) ||
				!slices.Equal(probe.args[len(probe.args)-3:], []string{";", "show-messages", "-T"}) {
				t.Fatalf("probe = %q", probe.args)
			}
			for _, call := range f.calls {
				if strings.Contains(strings.Join(call.args, " "), "Hunter2") {
					t.Fatalf("secret in argv: %q", call.args)
				}
			}
			load := f.calls[1]
			if string(load.stdin) != tmuxSecret || !slices.Equal(load.args[:2], []string{"load-buffer", "-b"}) ||
				!slices.Equal(load.args[3:7], []string{"-t", "/dev/pts/7", "-w", "-"}) {
				t.Fatalf("load = %q (stdin %q)", load.args, load.stdin)
			}
			name := load.args[2]
			if !tmuxBufferName.MatchString(name) {
				t.Fatalf("buffer name %q", name)
			}
			deleteArgs := []string{"delete-buffer", "-b", name}
			if tc.chained {
				if len(f.calls) != 2 || !slices.Equal(load.args[7:], append([]string{";"}, deleteArgs...)) {
					t.Fatalf("3.4+ did not delete in the same invocation: %q", f.calls)
				}
				return
			}
			if len(f.calls) != 3 || len(load.args) != 7 || !slices.Equal(f.calls[2].args, deleteArgs) || f.calls[2].stdin != nil {
				t.Fatalf("calls = %q", f.calls)
			}
		})
	}
}

func TestTmuxFailedLoadNeverDeletes(t *testing.T) {
	// Before 3.4, delete-buffer -b on a missing name deletes the user's
	// newest buffer.
	f := &fakeTmux{probe: probeText("3.2a", "", 1, false, true), loadErr: errors.New("tmux load-buffer: exit status 1")}
	if _, err := f.clipboard("%3").copy([]byte(tmuxSecret)); err == nil {
		t.Fatal("failed load-buffer reported success")
	}
	for _, call := range f.calls {
		if call.args[0] == "delete-buffer" {
			t.Fatalf("deleted after a failed load: %q", f.calls)
		}
	}
	// A popup has no $TMUX_PANE: the probe then targets the client.
	f = &fakeTmux{probe: probeText("3.5a", "on", 1, true, true)}
	if res, err := f.clipboard("").copy([]byte(tmuxSecret)); err != nil || res.route != routeTmuxLoadBuffer || slices.Contains(f.calls[0].args, "-t") {
		t.Fatalf("popup copy = %+v, %v (%q)", res, err, f.calls[0].args)
	}
}

func TestTmuxPassthroughCopyAndClear(t *testing.T) {
	f := &fakeTmux{probe: probeText("3.5a", "on", 1, true, true)}
	tmux := f.clipboard("%3")
	res, err := tmux.copy([]byte(tmuxSecret))
	if err != nil || res.route != routeTmuxPassthrough || !res.clearable() || res.unverified != "" {
		t.Fatalf("copy = %+v, %v", res, err)
	}
	want := ansi.TmuxPassthrough(ansi.SetSystemClipboard(tmuxSecret))
	if len(f.writes) != 1 || f.writes[0] != want || len(f.calls) != 1 {
		t.Fatalf("writes = %q, calls = %q", f.writes, f.calls)
	}

	// Clearing re-probes: deferred while keetui's pane is not on screen or
	// no client is attached, sent once tmux forwards it again.
	for _, state := range []string{
		probeText("3.5a", "on", 1, false, true),            // another window current
		probeText("3.5a", "on", 0, true, false),            // no client
		paneProbeText("3.5a", "on", 1, true, true, "1|0|"), // another pane zoomed
		paneProbeText("3.5a", "on", 1, true, true, "0|1|copy-mode"),
		paneProbeText("3.5a", "all", 1, true, true, "0|1|clock-mode"), // no setting gets past a mode
		tmuxFixtures["3.5a suspended"],                                // prefix C-z
		// 3.0a once tmux has recounted (any tmux command exiting): only
		// session_attached leaves the suspended client out.
		strings.Replace(tmuxFixtures["3.0a suspended"], "keetui-window|$0|1|", "keetui-window|$0|0|", 1),
		tmuxFixtures["3.8 suspended"], // 3.7+ lists no suspended client
		strings.Replace(tmuxFixtures["3.8 suspended"], "|on|", "|all|", 1),
	} {
		f.probe = state
		if outcome, err := tmux.clear(res); outcome != clearDeferred || err != nil || len(f.writes) != 1 {
			t.Fatalf("clear with %q = %v, %v (writes %q)", state, outcome, err, f.writes)
		}
	}
	// "all" gets past a zoomed pane and a hidden window.
	for _, state := range []string{paneProbeText("3.5a", "all", 1, true, true, "1|0|"), probeText("3.5a", "all", 1, false, true)} {
		f.probe = state
		if outcome, err := tmux.clear(res); outcome != clearRequested || err != nil || len(f.writes) != 2 {
			t.Fatalf("clear with %q = %v, %v (writes %q)", state, outcome, err, f.writes)
		}
		f.writes = f.writes[:1]
	}
	f.probe = probeText("3.5a", "on", 1, true, true)
	if outcome, err := tmux.clear(res); outcome != clearRequested || err != nil || f.writes[1] != ansi.TmuxPassthrough(ansi.SetSystemClipboard("")) {
		t.Fatalf("clear = %v, %v (writes %q)", outcome, err, f.writes)
	}
	// allow-passthrough turned off since the copy: nothing can clear it.
	f.probe = probeText("3.5a", "off", 1, true, true)
	if outcome, _ := tmux.clear(res); outcome != clearUnsupported || len(f.writes) != 2 {
		t.Fatalf("clear without passthrough = %v (writes %q)", outcome, f.writes)
	}
	for _, call := range f.calls {
		if call.args[0] != "display-message" {
			t.Fatalf("clear ran %q; it must never touch buffers", call.args)
		}
	}
}

// A clear that falls due after the terminal that got the copy detached
// goes to the client attached now only when that looks like the same
// terminal back on a new tty (an SSH reconnect); otherwise it would wipe
// only a newcomer's clipboard.
func TestTmuxClearSkipsADetachedClient(t *testing.T) {
	f := &fakeTmux{probe: probeText("3.8", "on", 1, true, true)}
	tmux := f.clipboard("%0")
	res, err := tmux.copy([]byte(tmuxSecret))
	if err != nil || !slices.Equal(res.reached, []string{"/dev/pts/7"}) || res.termName != "xterm-256color" || res.clientID != "xterm-256color\x00" {
		t.Fatalf("copy = %+v, %v", res, err)
	}
	// Another terminal, on /dev/pts/1, attached instead.
	other := strings.ReplaceAll(probeText("3.8", "on", 1, true, true), "/dev/pts/7", "/dev/pts/1")
	for _, tc := range []struct {
		probe string
		want  clearOutcome
	}{
		{tmuxFixtures["3.8 detached"], clearDeferred}, // nobody attached: it may come back
		{other, clearReattached},                      // same TERM and terminal type
		{strings.ReplaceAll(other, "xterm-256color", "foot"), clearDetached},
		{strings.Replace(other, "||@1|\n", "||@1|WezTerm 20240203\n", 1), clearDetached},
		// Neither is /dev/pts/7, and the clear would reach both.
		{tmuxFixtures["3.8 two clients"], clearDetached},
		{tmuxFixtures["3.8 suspended"], clearDeferred},
		{probeText("3.8", "on", 1, true, true), clearRequested}, // back on /dev/pts/7
	} {
		f.probe, f.writes = tc.probe, nil
		outcome, err := tmux.clear(res)
		sent := tc.want == clearRequested || tc.want == clearReattached
		if outcome != tc.want || err != nil || (len(f.writes) == 1) != sent {
			t.Fatalf("clear with %q = %v, %v (writes %q), want %v", tc.probe, outcome, err, f.writes, tc.want)
		}
	}
	// A client attached since before the copy, in a session showing
	// another window, is another terminal, though it reports the same type.
	f = &fakeTmux{probe: probeLines(
		"keetui-probe|3.8|external|on|/dev/pts/7|xterm-256color|1|1|1|0|1||@1|",
		"keetui-window|$1|1|@1|1",
		"keetui-client|/dev/pts/7|$1|attached,focused,UTF-8",
		"keetui-client|/dev/pts/1|$2|attached,focused,UTF-8",
	) + terminal("/dev/pts/7")}
	tmux = f.clipboard("%0")
	if res, err = tmux.copy([]byte(tmuxSecret)); err != nil || !slices.Equal(res.reached, []string{"/dev/pts/7"}) || res.clientID == "" {
		t.Fatalf("copy next to another client = %+v, %v", res, err)
	}
	f.probe = other
	if outcome, _ := tmux.clear(res); outcome != clearDetached {
		t.Fatalf("clear for a client attached at copy time = %v", outcome)
	}
	// list-clients names the attached ttys before 3.2 too.
	f = &fakeTmux{probe: tmuxFixtures["3.0a default"]}
	tmux = f.clipboard("%0")
	if res, err = tmux.copy([]byte(tmuxSecret)); err != nil || !slices.Equal(res.reached, []string{"/dev/pts/1"}) || res.clientID != "xterm-256color\x00Unknown" {
		t.Fatalf("3.0a copy = %+v, %v", res, err)
	}
	f.probe = strings.ReplaceAll(tmuxFixtures["3.0a default"], "/dev/pts/1", "/dev/pts/9")
	if outcome, _ := tmux.clear(res); outcome != clearReattached {
		t.Fatalf("3.0a clear = %v", outcome)
	}
	f.probe = strings.ReplaceAll(f.probe, "xterm-256color", "rxvt-unicode-256color")
	if outcome, _ := tmux.clear(res); outcome != clearDetached {
		t.Fatalf("3.0a clear for another terminal = %v", outcome)
	}
	// The copy reached main's client while tmux picked the third
	// session's: the recipient's terminal is unknown, so no newcomer is
	// taken for it.
	f = &fakeTmux{probe: tmuxFixtures["3.3a grouped"]}
	tmux = f.clipboard("%0")
	if res, err = tmux.copy([]byte(tmuxSecret)); err != nil || !slices.Equal(res.reached, []string{"/dev/pts/8"}) || res.clientID != "" {
		t.Fatalf("grouped copy = %+v, %v", res, err)
	}
	f.probe = strings.ReplaceAll(probeText("3.3a", "1", 1, true, true), "/dev/pts/7", "/dev/pts/9")
	if outcome, _ := tmux.clear(res); outcome != clearDetached {
		t.Fatalf("grouped clear = %v", outcome)
	}
	// A copy that reached several clients records all of them, and no
	// terminal to recognize on another tty.
	f = &fakeTmux{probe: tmuxFixtures["3.0a two clients"]}
	if res, _ := f.clipboard("%0").copy([]byte(tmuxSecret)); res.clients != 2 ||
		!slices.Equal(res.reached, []string{"/dev/pts/2", "/dev/pts/3"}) || res.clientID != "" {
		t.Fatalf("broadcast copy = %+v", res)
	}
	for outcome, want := range map[clearOutcome]string{
		clearDetached:   "Terminal clipboard not cleared: the terminal that got the copy detached",
		clearReattached: "Clipboard clear requested on the reattached terminal",
	} {
		if text, isError := clearStatus(outcome, nil); isError != (outcome == clearDetached) || text != want {
			t.Errorf("status = %q", text)
		}
		if !strings.Contains(clearDetail(outcome), "XTVERSION") {
			t.Errorf("detail for %v = %q", outcome, clearDetail(outcome))
		}
	}
}

// probeLines renders a probe answer from its lines. The probe line comes
// first; terminal(tty) adds a show-messages -T block without Ms.
func probeLines(lines ...string) string { return strings.Join(lines, "\n") + "\n" }

func terminal(tty string) string {
	return "Terminal 0: xterm-256color for " + tty + ", flags=0x20:\n 196: Ms: [missing]"
}

// A clear goes out only once tmux would write it to the terminal that got
// the copy. While that terminal is attached but out of reach (suspended,
// or its session shows another window), keetui keeps the copy and waits,
// however many other terminals show keetui's pane by then: they never got
// it.
func TestTmuxClearWaitsForTheTerminalThatGotTheCopy(t *testing.T) {
	// A (/dev/pts/7, session $1) shows keetui's window @1; B (/dev/pts/8,
	// grouped session $2) shows another window.
	f := &fakeTmux{probe: probeLines("keetui-probe|3.8|external|on|/dev/pts/7|xterm-256color|1|1|1|0|1||@1|",
		"keetui-window|$1|1|@1|1", "keetui-window|$2|1|@1|0",
		"keetui-client|/dev/pts/7|$1|attached,focused,UTF-8", "keetui-client|/dev/pts/8|$2|attached,UTF-8",
		terminal("/dev/pts/7"), terminal("/dev/pts/8"))}
	tmux := f.clipboard("%0")
	res, err := tmux.copy([]byte(tmuxSecret))
	if err != nil || res.route != routeTmuxPassthrough || !slices.Equal(res.reached, []string{"/dev/pts/7"}) {
		t.Fatalf("copy = %+v, %v", res, err)
	}
	for _, tc := range []struct {
		name, probe string
		want        clearOutcome
	}{
		{"A's session moved to another window, B's to keetui's", probeLines(
			"keetui-probe|3.8|external|on|/dev/pts/8|xterm-256color|1|1|1|0|1||@1|",
			"keetui-window|$1|1|@1|0", "keetui-window|$2|1|@1|1",
			"keetui-client|/dev/pts/7|$1|attached,UTF-8", "keetui-client|/dev/pts/8|$2|attached,focused,UTF-8",
			terminal("/dev/pts/8"), terminal("/dev/pts/7")), clearAway},
		// 3.7+ lists no suspended client, but show-messages -T still
		// names its terminal.
		{"A suspended, B shows keetui", probeLines(
			"keetui-probe|3.8|external|on|/dev/pts/8|xterm-256color|1|1|1|0|1||@1|",
			"keetui-window|$1|1|@1|1", "keetui-window|$2|1|@1|1",
			"keetui-client|/dev/pts/8|$2|attached,focused,UTF-8",
			terminal("/dev/pts/8"), terminal("/dev/pts/7")), clearAway},
		{"3.5a: A suspended, B shows keetui", probeLines(
			"keetui-probe|3.5a|external|on|/dev/pts/8|xterm-256color|1|1|1|0|1||@1|",
			"keetui-window|$1|1|@1|1", "keetui-window|$2|1|@1|1",
			"keetui-client|/dev/pts/7|$1|attached,suspended,UTF-8", "keetui-client|/dev/pts/8|$2|attached,focused,UTF-8",
			terminal("/dev/pts/8"), terminal("/dev/pts/7")), clearAway},
		// Before 3.2 the client rows carry no flags and show-messages -T
		// names no tty, but list-clients names each client's session.
		{"3.0a: A's session moved to another window", probeLines(
			"keetui-probe|3.0a|external||/dev/pts/8|xterm-256color|1|1||0|1||@1|Unknown",
			"keetui-window|$1|1|@1|0", "keetui-window|$2|1|@1|1",
			"keetui-client|/dev/pts/7|$1|", "keetui-client|/dev/pts/8|$2|",
			"Terminal 0: xterm-256color [references=2, flags=0x1]:", " 180: Ms: [missing]"), clearAway},
		// No row for keetui's window: the probe cannot name the
		// recipients, and the clear goes out as before.
		{"no window rows", probeLines(
			"keetui-probe|3.8|external|on|/dev/pts/8|xterm-256color|1|1|1|0|1||@1|",
			"keetui-client|/dev/pts/7|$1|attached,UTF-8", "keetui-client|/dev/pts/8|$2|attached,focused,UTF-8",
			terminal("/dev/pts/8"), terminal("/dev/pts/7")), clearRequested},
		{"both show keetui", probeLines(
			"keetui-probe|3.8|external|on|/dev/pts/8|xterm-256color|1|1|2|0|1||@1|",
			"keetui-window|$1|1|@1|1", "keetui-window|$2|1|@1|1",
			"keetui-client|/dev/pts/7|$1|attached,UTF-8", "keetui-client|/dev/pts/8|$2|attached,focused,UTF-8",
			terminal("/dev/pts/8"), terminal("/dev/pts/7")), clearRequested},
		{"A back on keetui's window", f.probe, clearRequested},
	} {
		f.probe, f.writes = tc.probe, nil
		outcome, err := tmux.clear(res)
		sent := tc.want == clearRequested
		if outcome != tc.want || err != nil || (len(f.writes) == 1) != sent {
			t.Fatalf("%s: clear = %v, %v (writes %q), want %v", tc.name, outcome, err, f.writes, tc.want)
		}
	}

	// With "all", a client gets passthrough while its session holds
	// keetui's window: A switched to a session ($3) that does not.
	f = &fakeTmux{probe: probeLines("keetui-probe|3.8|external|all|/dev/pts/7|xterm-256color|1|1|1|0|1||@1|",
		"keetui-window|$1|1|@1|1",
		"keetui-client|/dev/pts/7|$1|attached,focused,UTF-8", "keetui-client|/dev/pts/8|$3|attached,UTF-8",
		terminal("/dev/pts/7"), terminal("/dev/pts/8"))}
	tmux = f.clipboard("%0")
	if res, err = tmux.copy([]byte(tmuxSecret)); err != nil || !slices.Equal(res.reached, []string{"/dev/pts/7"}) {
		t.Fatalf("all copy = %+v, %v", res, err)
	}
	f.probe = probeLines("keetui-probe|3.8|external|all|/dev/pts/8|xterm-256color|1|1|1|0|1||@1|",
		"keetui-window|$1|1|@1|1",
		"keetui-client|/dev/pts/7|$3|attached,UTF-8", "keetui-client|/dev/pts/8|$1|attached,focused,UTF-8",
		terminal("/dev/pts/8"), terminal("/dev/pts/7"))
	if outcome, _ := tmux.clear(res); outcome != clearAway || len(f.writes) != 1 {
		t.Fatalf("all clear while A's session holds no keetui window = %v (writes %q)", outcome, f.writes)
	}
}

// A copy that reached several terminals records them all. Its clear waits
// while one of them is attached but out of reach, goes out once tmux
// would write it to every one still attached, and says so when some of
// them left meanwhile.
func TestTmuxBroadcastClearWaitsForEveryRecipient(t *testing.T) {
	// Two clients of one session show keetui's window, neither terminal
	// with Ms: passthrough reaches both.
	both := probeLines("keetui-probe|3.8|external|on|/dev/pts/7|xterm-256color|2|1|2|0|1||@1|",
		"keetui-window|$1|2|@1|1",
		"keetui-client|/dev/pts/7|$1|attached,focused,UTF-8", "keetui-client|/dev/pts/8|$1|attached,UTF-8",
		terminal("/dev/pts/7"), terminal("/dev/pts/8"))
	f := &fakeTmux{probe: both}
	tmux := f.clipboard("%0")
	res, err := tmux.copy([]byte(tmuxSecret))
	if err != nil || res.route != routeTmuxPassthrough || res.clients != 2 ||
		!slices.Equal(res.reached, []string{"/dev/pts/7", "/dev/pts/8"}) || res.clientID != "" {
		t.Fatalf("copy = %+v, %v", res, err)
	}
	for _, tc := range []struct {
		name, probe string
		want        clearOutcome
	}{
		{"B suspended", probeLines("keetui-probe|3.8|external|on|/dev/pts/7|xterm-256color|2|1|2|0|1||@1|",
			"keetui-window|$1|2|@1|1", "keetui-client|/dev/pts/7|$1|attached,focused,UTF-8",
			terminal("/dev/pts/7"), terminal("/dev/pts/8")), clearAway},
		{"3.5a: B suspended", probeLines("keetui-probe|3.5a|external|on|/dev/pts/7|xterm-256color|2|1|2|0|1||@1|",
			"keetui-window|$1|2|@1|1",
			"keetui-client|/dev/pts/7|$1|attached,focused,UTF-8", "keetui-client|/dev/pts/8|$1|attached,suspended,UTF-8",
			terminal("/dev/pts/7"), terminal("/dev/pts/8")), clearAway},
		{"B detached", probeText("3.8", "on", 1, true, false), clearPartial},
		{"both detached, another terminal attached", strings.ReplaceAll(probeText("3.8", "on", 1, true, false), "/dev/pts/7", "/dev/pts/9"),
			clearDetached},
		{"both detached", tmuxFixtures["3.8 detached"], clearDeferred},
		{"both back", both, clearRequested},
	} {
		f.probe, f.writes = tc.probe, nil
		outcome, err := tmux.clear(res)
		sent := tc.want == clearRequested || tc.want == clearPartial
		if outcome != tc.want || err != nil || (len(f.writes) == 1) != sent {
			t.Fatalf("%s: clear = %v, %v (writes %q), want %v", tc.name, outcome, err, f.writes, tc.want)
		}
	}
	text, isError := clearStatus(clearPartial, nil)
	if text != "Clipboard clear requested; a terminal that got the copy detached" || !isError ||
		!strings.Contains(clearDetail(clearPartial), "detached before the clear") {
		t.Fatalf("partial status = %q (error %v), detail %q", text, isError, clearDetail(clearPartial))
	}
}

// runTmux returns soon after its timeout even when the tmux it runs leaves
// a child holding its output open, as a wrapper script without exec does.
// These are scripts named tmux, never tmux itself.
func TestRunTmuxIsBounded(t *testing.T) {
	dir := t.TempDir()
	for _, tc := range []struct {
		name, script string
		timeout      time.Duration
	}{
		// Killed at the timeout; its sleep keeps stdout open.
		{"killed", "#!/bin/sh\nsleep 10\n", 200 * time.Millisecond},
		// Exits 0 at once, its sleep keeping stdout open. The timeout
		// leaves room for a slow first exec of a new script (macOS scans
		// it), which is not what this case measures.
		{"exited", "#!/bin/sh\nsleep 10 &\necho keetui-probe\n", 2 * time.Second},
	} {
		path := filepath.Join(dir, tc.name)
		if err := os.WriteFile(path, []byte(tc.script), 0o700); err != nil {
			t.Fatal(err)
		}
		start := time.Now()
		out, err := runTmuxAt(path, tc.timeout, nil, "display-message", "-p")
		if elapsed := time.Since(start); elapsed > tc.timeout+time.Second {
			t.Errorf("%s: runTmuxAt returned after %v", tc.name, elapsed.Round(10*time.Millisecond))
		}
		// A tmux that exited 0 succeeded, whatever its child holds.
		if exited := tc.name == "exited"; (err == nil) != exited || exited && string(out) != "keetui-probe\n" {
			t.Errorf("%s: runTmuxAt = %q, %v", tc.name, out, err)
		}
	}
}

func TestTmuxUnclearableCopiesAreNotCleared(t *testing.T) {
	for _, last := range []terminalResult{
		{route: routeTmuxLoadBuffer, noClear: "allow-passthrough off"},
		{route: routeTmuxPassthrough, nested: true, noClear: "nested tmux"},
	} {
		f := &fakeTmux{probe: probeText("3.8", "on", 1, true, true)}
		if outcome, err := f.clipboard("%3").clear(last); outcome != clearUnsupported || err != nil || len(f.calls)+len(f.writes) != 0 {
			t.Fatalf("clear(%+v) = %v, %v (calls %q, writes %q)", last, outcome, err, f.calls, f.writes)
		}
	}
}

func TestTmuxProbeFailureSendsPassthroughOnly(t *testing.T) {
	f := &fakeTmux{probeErr: errors.New("tmux: exec: \"tmux\": executable file not found in $PATH")}
	tmux := f.clipboard("%3")
	res, err := tmux.copy([]byte(tmuxSecret))
	if err != nil || res.route != routeTmuxPassthrough || res.unverified == "" || !res.clearable() {
		t.Fatalf("copy = %+v, %v", res, err)
	}
	if outcome, err := tmux.clear(res); outcome != clearRequested || err != nil {
		t.Fatalf("clear = %v, %v", outcome, err)
	}
	if len(f.writes) != 2 || f.writes[0] != passthroughOSC52([]byte(tmuxSecret)) || f.writes[1] != passthroughOSC52(nil) {
		t.Fatalf("writes = %q", f.writes)
	}
	if got := copyStatus("password", copyResult{terminal: true, via: res}); got != "Sent password via tmux passthrough (unverified; needs allow-passthrough on)" {
		t.Fatalf("status = %q", got)
	}
	help := clipboardHelp(copyResult{terminal: true, via: res}, nil)
	for _, want := range []string{"executable file not found", "set -g allow-passthrough on", "clipboard_osc52 always", "set-clipboard on"} {
		if !strings.Contains(help, want) {
			t.Errorf("help lacks %q: %q", want, help)
		}
	}
	if strings.Contains(help, "set-clipboard needs no change") {
		t.Errorf("help = %q", help)
	}

	// tmux before 3.3 (from XTVERSION) always lets passthrough by.
	f = &fakeTmux{probeErr: errors.New("tmux: no server running")}
	res, _ = f.clipboard("%3").with(terminalKind{class: terminalTmux, tmux: "3.2a"}, false).copy([]byte(tmuxSecret))
	if got := copyStatus("password", copyResult{terminal: true, via: res}); got != "Sent password via tmux passthrough (OSC 52)" {
		t.Fatalf("status for 3.2a = %q", got)
	}

	// clipboard_osc52 always adds a plain OSC 52, which tmux forwards with
	// set-clipboard on, and the clear sends both empty copies.
	f = &fakeTmux{probeErr: errors.New("tmux: no server running")}
	tmux = f.clipboard("%3").with(terminalKind{class: terminalTmux, tmux: "3.5a"}, true).(tmuxClipboard)
	res, err = tmux.copy([]byte(tmuxSecret))
	if err != nil || !res.plain || res.version != "3.5a" {
		t.Fatalf("always copy = %+v, %v", res, err)
	}
	_, _ = tmux.clear(res)
	plain := "\x1b]52;c;" + "c3ludGhldGljLUh1bnRlcjItc2VjcmV0" + "\a"
	if want := []string{passthroughOSC52([]byte(tmuxSecret)) + plain, passthroughOSC52(nil) + "\x1b]52;c;\a"}; !slices.Equal(f.writes, want) {
		t.Fatalf("always writes = %q, want %q", f.writes, want)
	}
	if got := copyStatus("password", copyResult{terminal: true, via: res}); got != "Sent password via tmux passthrough and OSC 52; with set-clipboard on, tmux keeps a copy" {
		t.Fatalf("always status = %q", got)
	}

	// A probe line before a failing show-messages is still an answer.
	f = &fakeTmux{probe: tmuxFixtures["3.5a detached"], probeErr: errors.New("tmux display-message: exit status 1: no current client")}
	if _, err := f.clipboard("%3").copy([]byte(tmuxSecret)); err == nil || !strings.Contains(err.Error(), "no client attached") || len(f.writes) != 0 {
		t.Fatalf("copy without a client = %v (writes %q)", err, f.writes)
	}
}

func TestTmuxRoutesNeverSendRawOSC52(t *testing.T) {
	for _, version := range []string{"3.0a", "3.2a", "3.3a", "3.5a", "3.8"} {
		for _, pt := range []string{"", "0", "1", "off", "on", "all"} {
			for attached := range 3 {
				for _, active := range []bool{true, false} {
					for _, ms := range []bool{true, false} {
						f := &fakeTmux{probe: probeText(version, pt, attached, active, ms)}
						tmux := f.clipboard("%3")
						res, err := tmux.copy([]byte(tmuxSecret))
						if err == nil {
							_, _ = tmux.clear(res)
						}
						for _, w := range f.writes {
							if !strings.HasPrefix(w, "\x1bPtmux;\x1b\x1b]52;c;") {
								t.Fatalf("%s pt=%q: raw write %q", version, pt, w)
							}
						}
						if err != nil && len(f.writes) != 0 {
							t.Fatalf("%s pt=%q: failed copy still wrote %q", version, pt, f.writes)
						}
					}
				}
			}
		}
	}
	f := &fakeTmux{probe: probeText("3.8", "on", 1, true, true)}
	if _, err := f.clipboard("%3").copy(nil); !errors.Is(err, errEmptyTmuxCopy) || len(f.calls)+len(f.writes) != 0 {
		t.Fatalf("empty copy = %v (calls %q, writes %q)", err, f.calls, f.writes)
	}
}

func TestRemoteTmuxAndTTYClipboards(t *testing.T) {
	var writes []string
	write := func(seq string) error {
		writes = append(writes, seq)
		return nil
	}
	remote := remoteTmuxClipboard{write: write}
	res, err := remote.copy([]byte(tmuxSecret))
	if err != nil || res.route != routeRemoteTmux || res.unverified == "" || !res.clearable() {
		t.Fatalf("remote copy = %+v, %v", res, err)
	}
	if outcome, _ := remote.clear(res); outcome != clearRequested {
		t.Fatalf("remote clear = %v", outcome)
	}
	if _, err := remote.copy(nil); !errors.Is(err, errEmptyTmuxCopy) {
		t.Fatal("remote tmux accepted an empty value")
	}
	tty := ttyClipboard{write: write}
	if res, err := tty.copy([]byte(tmuxSecret)); err != nil || !reflect.DeepEqual(res, terminalResult{}) {
		t.Fatalf("tty copy = %+v, %v", res, err)
	}
	if outcome, _ := tty.clear(terminalResult{}); outcome != clearRequested {
		t.Fatalf("tty clear = %v", outcome)
	}
	want := []string{
		passthroughOSC52([]byte(tmuxSecret)), passthroughOSC52(nil),
		"\x1b]52;c;" + "c3ludGhldGljLUh1bnRlcjItc2VjcmV0" + "\a", "\x1b]52;c;\a",
	}
	if !slices.Equal(writes, want) {
		t.Fatalf("writes = %q", writes)
	}
	if got := copyStatus("password", copyResult{terminal: true, via: res}); got != "Sent password via tmux passthrough (unverified; needs allow-passthrough on)" {
		t.Fatalf("remote status = %q", got)
	}
	help := clipboardHelp(copyResult{terminal: true, via: res}, nil)
	for _, want := range []string{"$TMUX is unset", "sudo or su", "set -g allow-passthrough on", "clipboard_osc52 always"} {
		if !strings.Contains(help, want) {
			t.Errorf("remote help lacks %q: %q", want, help)
		}
	}
	// No remote tmux advice that only fits a tmux keetui can ask.
	for _, wrong := range []string{"terminal-features", "set-clipboard needs no change"} {
		if strings.Contains(help, wrong) {
			t.Errorf("remote help has %q: %q", wrong, help)
		}
	}
}

// clipboard_osc52 always: the remote tmux route also sends a plain OSC 52,
// and its clear both empty copies; auto stays passthrough only.
func TestRemoteTmuxPlainLegFollowsMode(t *testing.T) {
	for _, tc := range []struct {
		mode, version string
		writes        []string
		status        string
		leg           string
	}{
		{config.OSC52Auto, "3.5a", []string{passthroughOSC52([]byte(tmuxSecret)), passthroughOSC52(nil)},
			"Sent password via tmux passthrough (unverified; needs allow-passthrough on)",
			"Copied password (also sent via tmux passthrough, unverified; needs allow-passthrough on)"},
		{config.OSC52Auto, "3.2a", []string{passthroughOSC52([]byte(tmuxSecret)), passthroughOSC52(nil)},
			"Sent password via tmux passthrough (OSC 52)", "Copied password (also sent via tmux passthrough)"},
		{config.OSC52Always, "3.5a", []string{
			passthroughOSC52([]byte(tmuxSecret)) + "\x1b]52;c;c3ludGhldGljLUh1bnRlcjItc2VjcmV0\a",
			passthroughOSC52(nil) + "\x1b]52;c;\a",
		}, "Sent password via tmux passthrough and OSC 52; with set-clipboard on, tmux keeps a copy",
			"Copied password (also sent via tmux passthrough and OSC 52; with set-clipboard on, tmux keeps a copy)"},
	} {
		var writes []string
		remote := remoteTmuxClipboard{write: func(seq string) error { writes = append(writes, seq); return nil }}
		c := &systemClipboard{native: &fakeNative{}, terminal: terminalRoutes{tty: &fakeTerminal{}, remote: remote}}
		res, err := c.copy(tmuxSecret, copyOptions{osc52: tc.mode, terminal: terminalKind{class: terminalTmux, tmux: tc.version}})
		if err != nil || res.via.route != routeRemoteTmux {
			t.Fatalf("%s %s: copy = %+v, %v", tc.mode, tc.version, res, err)
		}
		if got := copyStatus("password", res); got != tc.status {
			t.Errorf("%s %s: status = %q, want %q", tc.mode, tc.version, got, tc.status)
		}
		if got := copyStatus("password", copyResult{native: true, marked: true, terminal: true, via: res.via}); got != tc.leg {
			t.Errorf("%s %s: leg status = %q, want %q", tc.mode, tc.version, got, tc.leg)
		}
		if outcome, err := c.release(); outcome != clearRequested || err != nil {
			t.Fatalf("%s: release = %v, %v", tc.mode, outcome, err)
		}
		if !slices.Equal(writes, tc.writes) {
			t.Errorf("%s %s: writes = %q, want %q", tc.mode, tc.version, writes, tc.writes)
		}
		if help := clipboardHelp(res, nil); tc.mode == config.OSC52Always && !strings.Contains(help, "paste buffer") {
			t.Errorf("always help = %q", help)
		}
	}
}

func TestTerminalRoutesPick(t *testing.T) {
	env := map[string]string{}
	getenv := func(k string) string { return env[k] }
	routes := newTerminalRoutes(getenv)
	tmuxKind := terminalKind{class: terminalTmux, tmux: "3.5a"}
	for _, kind := range []terminalKind{{}, {class: terminalOther}} {
		if _, ok := routes.pick(kind, false).(ttyClipboard); !ok {
			t.Fatalf("plain terminal (%v) not routed to the tty", kind)
		}
	}
	if _, ok := routes.pick(tmuxKind, false).(remoteTmuxClipboard); !ok {
		t.Fatal("XTVERSION tmux without $TMUX not routed as remote tmux")
	}
	if remote := routes.pick(tmuxKind, true).(remoteTmuxClipboard); remote.settle == nil || !remote.plain || remote.version != "3.5a" {
		t.Fatalf("remote tmux route = %#v", remote)
	}
	if remote := routes.pick(tmuxKind, false).(remoteTmuxClipboard); remote.plain {
		t.Fatal("auto remote tmux copies send a plain OSC 52")
	}
	env["TMUX"], env["TMUX_PANE"] = "/tmp/tmux-1000/default,1234,0", "%5"
	routes = newTerminalRoutes(getenv)
	for _, kind := range []terminalKind{{}, tmuxKind} {
		if tmux, ok := routes.pick(kind, true).(tmuxClipboard); !ok || tmux.pane != "%5" || tmux.settle == nil || !tmux.plain || tmux.version != kind.tmux {
			t.Fatalf("$TMUX not routed through tmux: %#v", routes.pick(kind, true))
		}
	}
	// $TMUX set, but the terminal named itself: nothing is sent, neither
	// raw nor through a tmux pane keetui is not in.
	refused := routes.pick(terminalKind{class: terminalOther}, false)
	if _, ok := refused.(refusedTerminal); !ok {
		t.Fatalf("inherited $TMUX routed to %#v", refused)
	}
	if res, err := refused.copy([]byte(tmuxSecret)); !errors.Is(err, errTerminalNotTmux) || !reflect.DeepEqual(res, terminalResult{}) {
		t.Fatalf("inherited $TMUX copy = %+v, %v", res, err)
	}
	if outcome, err := refused.clear(terminalResult{}); outcome != clearNothing || err != nil {
		t.Fatalf("inherited $TMUX clear = %v, %v", outcome, err)
	}
}

// Passthrough waits for the renderer's pending frame before tmux is asked
// and before anything is written: tmux drops passthrough that arrives
// right behind a synchronized-output frame.
func TestPassthroughSettlesFirst(t *testing.T) {
	var events []string
	f := &fakeTmux{probe: probeText("3.7c", "on", 1, true, true)}
	tmux := tmuxClipboard{
		run: func(stdin []byte, args ...string) ([]byte, error) {
			events = append(events, args[0])
			return f.run(stdin, args...)
		},
		write:  func(string) error { events = append(events, "write"); return nil },
		settle: func() { events = append(events, "settle") },
		pane:   "%1",
	}
	res, err := tmux.copy([]byte(tmuxSecret))
	if err != nil {
		t.Fatal(err)
	}
	_, _ = tmux.clear(res)
	remote := remoteTmuxClipboard{
		write:  func(string) error { events = append(events, "write"); return nil },
		settle: func() { events = append(events, "settle") },
	}
	res, _ = remote.copy([]byte(tmuxSecret))
	_, _ = remote.clear(res)
	want := []string{"settle", "display-message", "write", "settle", "display-message", "write", "settle", "write", "settle", "write"}
	if !slices.Equal(events, want) {
		t.Fatalf("events = %q, want %q", events, want)
	}
}

func TestTerminalCopyStatus(t *testing.T) {
	for _, tc := range []struct {
		via  terminalResult
		want string
	}{
		{terminalResult{}, "Sent password to the terminal clipboard (OSC 52)"},
		{terminalResult{route: routeTmuxPassthrough}, "Sent password to the terminal clipboard via tmux (OSC 52)"},
		{terminalResult{route: routeTmuxPassthrough, clients: 2}, "Sent password to 2 tmux clients' terminal clipboards (OSC 52)"},
		{terminalResult{route: routeTmuxPassthrough, nested: true, noClear: "nested tmux"},
			"Sent password via nested tmux (OSC 52, unverified); outer tmux may keep a copy"},
		{terminalResult{route: routeTmuxLoadBuffer, nested: true, noClear: "2 clients share the window"},
			"Sent password via nested tmux (OSC 52, unverified); outer tmux may keep a copy"},
		{terminalResult{route: routeTmuxLoadBuffer, noClear: "allow-passthrough off"},
			"Sent password via tmux (OSC 52); can't auto-clear: allow-passthrough off"},
		{terminalResult{route: routeTmuxLoadBuffer, noClear: "2 clients share the window"},
			"Sent password via tmux (OSC 52); can't auto-clear: 2 clients share the window"},
		{terminalResult{route: routeRemoteTmux, unverified: "remote"}, "Sent password via tmux passthrough (unverified; needs allow-passthrough on)"},
		{terminalResult{route: routeRemoteTmux, unverified: "remote", version: "3.2a"}, "Sent password via tmux passthrough (OSC 52)"},
		{terminalResult{route: routeRemoteTmux, unverified: "remote", version: "3.5a", plain: true}, "Sent password via tmux passthrough and OSC 52; with set-clipboard on, tmux keeps a copy"},
	} {
		if got := copyStatus("password", copyResult{terminal: true, via: tc.via}); got != tc.want {
			t.Errorf("copyStatus(%+v) = %q, want %q", tc.via, got, tc.want)
		}
	}
	both := copyResult{native: true, marked: true, terminal: true, via: terminalResult{route: routeTmuxPassthrough}}
	if got := copyStatus("password", both); got != "Copied password (also sent via tmux OSC 52)" {
		t.Errorf("native+tmux status = %q", got)
	}
	failed := copyResult{native: true, marked: true, termErr: &tmuxRouteError{reason: "allow-passthrough off"}}
	if got := copyStatus("password", failed); got != "Copied password (OSC 52 failed: tmux can't reach the terminal clipboard: allow-passthrough off)" {
		t.Errorf("native+failed tmux status = %q", got)
	}
}

func TestClipboardHelpExplainsTmuxSetup(t *testing.T) {
	routeErr := &tmuxRouteError{reason: "allow-passthrough off and no clipboard support for screen-256color", termName: "screen-256color"}
	for _, tc := range []struct {
		res  copyResult
		err  error
		want string
	}{
		{copyResult{}, routeErr, "set -as terminal-features ',screen-256color:clipboard'"},
		{copyResult{native: true, termErr: routeErr}, nil, "set -g allow-passthrough on"},
		{copyResult{terminal: true, via: terminalResult{route: routeTmuxLoadBuffer, termName: "foot"}}, nil, "',foot:clipboard'"},
	} {
		if got := clipboardHelp(tc.res, tc.err); !strings.Contains(got, tc.want) || !strings.Contains(got, "set-clipboard needs no change") {
			t.Errorf("clipboardHelp(%+v, %v) = %q", tc.res, tc.err, got)
		}
	}
	// Without a tmux to ask, set-clipboard matters for the plain OSC 52
	// that clipboard_osc52 always adds.
	unverified := copyResult{terminal: true, via: terminalResult{route: routeTmuxPassthrough, unverified: "socket gone"}}
	if got := clipboardHelp(unverified, nil); !strings.Contains(got, "socket gone") || strings.Contains(got, "set-clipboard needs no change") {
		t.Errorf("unverified help = %q", got)
	}
	// Nested tmux: the outer tmux needs set-clipboard on.
	nested := copyResult{terminal: true, via: terminalResult{route: routeTmuxPassthrough, nested: true, noClear: "nested tmux"}}
	if got := clipboardHelp(nested, nil); !strings.Contains(got, "set -g set-clipboard on") || strings.Contains(got, "set-clipboard needs no change") {
		t.Errorf("nested help = %q", got)
	}
	for _, res := range []copyResult{{native: true, marked: true}, {terminal: true}} {
		if got := clipboardHelp(res, errors.New("no tty")); strings.Contains(got, "tmux") {
			t.Errorf("non-tmux copy got tmux help: %q", got)
		}
	}
	for _, tc := range []struct {
		res copyResult
		err error
	}{{copyResult{}, errTerminalNotTmux}, {copyResult{native: true, termErr: errTerminalNotTmux}, nil}} {
		if got := clipboardHelp(tc.res, tc.err); !strings.Contains(got, "env -u TMUX keetui") {
			t.Errorf("inherited $TMUX help = %q", got)
		}
	}
}
