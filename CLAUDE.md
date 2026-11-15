# CLAUDE.md - AI Assistant Guide for keetui

## Project Overview

**keetui** is a terminal user interface (TUI) for reading and writing KeePass `.kdbx` database files. It is written in Go and targets macOS and Linux.

## Repository Structure

```
keetui/
├── cmd/
│   └── keetui/
│       ├── main.go              # CLI entry point, argument parsing
│       └── main_test.go
├── internal/
│   ├── config/
│   │   ├── config.go            # Configuration loading, saving, defaults
│   │   ├── overrides.go         # Per-database settings layered over the config
│   │   ├── config_test.go
│   │   ├── keybindings.go       # Keybinding definitions and customization
│   │   └── keybindings_test.go
│   ├── pasteboard/
│   │   └── pasteboard_darwin.go # In-process macOS pasteboard (purego): markers, changeCount
│   ├── keepass/
│   │   ├── keepass.go           # KeePass DB operations (open, save, CRUD)
│   │   ├── keepass_test.go
│   │   ├── attachment.go        # File attachment operations
│   │   ├── audit.go             # Batch password audit (weak, reused, empty)
│   │   ├── customdata.go        # Database-level custom data (per-database settings)
│   │   ├── diceware.go          # Diceware passphrase generation
│   │   ├── export.go            # CSV/XML import and export
│   │   ├── hibp.go              # Have I Been Pwned breach check
│   │   ├── merge.go             # Merge from another .kdbx file
│   │   ├── disk.go              # What keetui last read or wrote: save guard (CheckDisk, DiskError)
│   │   ├── sync.go              # Merge the changes another program made to the file (MergeDisk)
│   │   ├── snapshot.go          # Self-contained entry/group copies for undo
│   │   ├── stats.go             # Database statistics
│   │   ├── strength.go          # Password strength evaluation
│   │   └── *_test.go            # Corresponding tests for each
│   ├── x11clip/
│   │   ├── connect.go           # Display parsing, dial, cookie lookup (libxcb order, Owner.Authority)
│   │   ├── owner.go             # In-process X11 CLIPBOARD owner (xgb): copy, blank, read-back (INCR)
│   │   ├── serve.go             # Selection targets: TARGETS, UTF8_STRING, STRING, hint, MULTIPLE
│   │   └── *_test.go            # Unit tests; xvfb_test.go needs KEETUI_X11_TEST_DISPLAY
│   └── ui/
│       ├── app.go               # Top-level app model, view orchestration
│       ├── audit.go             # Security audit results view
│       ├── browser.go           # Group/entry list browser with navigation
│       ├── clipboard*.go        # Clipboard routing (local, OSC 52, tmux, herdr), remote-session detection, backends
│       ├── confirm.go           # Yes/no confirmation dialog
│       ├── detail.go            # Read-only entry detail view
│       ├── editor.go            # Entry create/edit form
│       ├── group_input.go       # New group name input
│       ├── help.go              # Keyboard shortcut help overlay
│       ├── history.go           # Entry password history view
│       ├── password.go          # Master password change dialog
│       ├── path_input.go        # Save-as file path input
│       ├── preset.go            # Password generation preset picker
│       ├── settings.go          # Settings editor view
│       ├── settings_database.go # "This database" settings editor
│       ├── stats.go             # Database stats view
│       ├── styles.go            # Lip Gloss style definitions
│       ├── themes.go            # Color theme definitions
│       ├── undo.go              # Undo stack for reversible actions
│       ├── unlock.go            # Password entry / unlock screen
│       └── *_test.go            # Corresponding tests for each
├── .githooks/
│   └── pre-commit               # gofmt check
├── .github/
│   └── workflows/
│       ├── ci.yml               # CI: vet, test, lint on push/PR
│       └── release.yml          # Build and release on tag push
├── go.mod
├── go.sum
├── Makefile
├── README.md
├── SECURITY.md               # How to report a vulnerability
└── CLAUDE.md
```

## Key Dependencies

| Package | Purpose |
|---------|---------|
| `github.com/tobischo/gokeepasslib/v3` | KeePass .kdbx file reading/writing |
| `charm.land/bubbletea/v2` | TUI framework (Elm-style MVU architecture) |
| `charm.land/bubbles/v2` | TUI components (text input, viewport) |
| `charm.land/lipgloss/v2` | Terminal styling |
| `github.com/pquerna/otp` | TOTP/2FA code generation |
| `github.com/boombuler/barcode` | QR code generation |
| `github.com/ebitengine/purego` | AppKit calls without cgo (macOS pasteboard) |
| `github.com/jezek/xgb` | X11 protocol without cgo (Linux clipboard owner, `internal/x11clip`) |
| `golang.org/x/sys` | sysctl process reads on macOS (remote-session detection) |

## Architecture

The application follows the **Bubble Tea Model-View-Update (MVU)** pattern:

- **`internal/keepass`** — Pure data layer. Wraps `gokeepasslib` with a simplified API (`Open`, `Save`, `SaveAs`, `AddEntry`, `UpdateEntry`, `DeleteEntry`, `AddGroup`, `DeleteGroup`, `Merge`, `MergePreview`, `ConvertToKDBX4`, etc.). No TUI dependencies.
- **`internal/config`** — Configuration and keybinding management. Loads/saves JSON config, provides defaults, validates settings.
- **`internal/ui`** — TUI layer. Each screen is a separate Bubble Tea model. The `AppModel` in `app.go` orchestrates view switching via message passing.
- **`cmd/keetui`** — CLI entry point. Parses flags, handles headless operations (import/export/merge/upgrade), and launches the TUI program.

### View Flow

```
UnlockModel → BrowserModel ↔ DetailModel
                   ↕              ↓
              GroupInputModel  EditorModel ↔ PresetPickerModel
                   ↕              ↕
              ConfirmModel    ConfirmModel
                   ↕
              SettingsModel / StatsModel / AuditModel / HelpModel / PasswordModel / PathInputModel
```

### Message Passing

Views communicate through typed messages (e.g., `ViewEntryMsg`, `SaveEntryMsg`, `BackToBrowserMsg`). The `AppModel.Update()` method handles cross-view messages and delegates view-specific messages to the active sub-model.

## Development Workflow

### Setup

```sh
git config core.hooksPath .githooks   # enable pre-commit gofmt check
```

### Building

CI, release builds, and the Makefile's container image are pinned to Go 1.27.3.
Keep `go 1.26.0` in `go.mod` as the minimum source version unless newer language
features are required. Go 1.27-built macOS binaries require macOS 13 Ventura or
newer; the generated Homebrew formula declares that requirement.

```sh
go build ./...                          # check compilation
go build -o keetui ./cmd/keetui         # build binary
make                                    # build locally with the installed Go toolchain
make CONTAINER_RUNTIME=podman            # opt into a running container runtime
```

### Running

```sh
./keetui database.kdbx
./keetui -keyfile key.key database.kdbx
./keetui -config                        # print config path and current settings
./keetui --upgrade-kdbx4 database.kdbx  # upgrade KDBX 3.1 → 4
```

### Testing

```sh
go test ./...                           # run all tests
go test -v ./internal/keepass/...       # run keepass tests with verbose output
make check                              # lint + test (gofmt, golangci-lint, go test -race)
Xvfb :99 -nolisten tcp -noreset &       # X11 owner tests: a dedicated server (Linux/container)
KEETUI_X11_TEST_DISPLAY=:99 go test -race ./internal/x11clip/
```

### Formatting & Vetting

```sh
gofmt -w .
go vet ./...
```

## Configuration

Settings are stored in `~/.config/keetui/config.json` (or `$XDG_CONFIG_HOME/keetui/config.json`).

| Setting | Default | Description |
|---------|---------|-------------|
| `clipboard_clear_seconds` | 15 | Seconds before clipboard is cleared after copying (0: disable, min: 15) |
| `clipboard_osc52` | `"auto"` | Terminal clipboard (OSC 52): `auto` (only without a local clipboard), `always`, `never` |
| `auto_lock_minutes` | 5 | Minutes of inactivity before database is locked (0: disable, min: 5) |
| `privacy_mode` | false | Keep passwords hidden while editing (`Config.HidePasswords`): no brief reveal when typing, generated passwords masked in the editor and preset preview |
| `password_length` | 20 | Default length for generated passwords (min: 8) |
| `show_password_strength` | true | Show password strength indicator in detail and editor views |
| `default_preset` | `"Strong"` | Default password preset: `PIN`, `Alphanumeric`, `Strong`, `Passphrase`, `Custom` |
| `pin_length` | 6 | Digits in a generated PIN (4 to 16) |
| `passphrase_words` | 8 | Words in a generated passphrase (4 to 12; about 10 bits each, so 8 give about 80) |
| `passphrase_separator` | `"-"` | What separates passphrase words: 1 to 3 visible characters, no spaces |
| `theme` | `"auto"` | Color theme: `auto` (follows the terminal background, dark without an answer), `dark`, `light`, `high-contrast`, or `terminal` (ANSI palette, never painted) |
| `colors` | (none) | Per-theme color overrides: theme name → role (`config.ColorRoles`) → `#RGB`/`#RRGGBB`, ANSI `0`-`255`, or `""` (terminal default) |
| `confirm_quit` | true | Confirm before quitting, even without unsaved changes (`false`: disable) |
| `backup_on_save` | false | Create `.kdbx.bak` backup before each save |
| `keybindings` | (object) | Override default keybindings (see `keybindings.go` for keys) |

- **CLI**: Run `keetui -config` to display the config file path and current settings as valid JSON.
- **TUI**: Press `c` in the browser view, or `Ctrl+O` there or on the unlock screen (reserved in the keybinding validator), to open the settings editor. Changes are saved to disk and take effect immediately. Opened before a database is unlocked (`m.db == nil`), settings skip `setDatabase`, and both leaving and saving return to the unlock screen; the typed password is cleared on the way in. On the unlock screen `Esc` quits like `Ctrl+C` (`quitsUnlock`, also while unlocking); its footer keeps naming `ctrl+c`. While it shows, the unlock screen stats the database and key file in a command (`checkUnlockFiles`, every `fileCheckInterval`) and warns about a missing file or a folder (`UnlockModel.missing`); `watchUnlockFiles` starts the check over (generation `fileCheckGen`) wherever the screen is shown again (lock, return from settings), and results stop the chain once another screen shows. A failed unlock names a missing file plainly (`openError`).
- If the config file does not exist, defaults are used. The file is created automatically when settings are saved from the TUI.
- **Per-database settings**: `config.Overrides` (JSON under the KDBX metadata custom data key `KEETUI_SETTINGS`, read and written with `keepass.DB.CustomData`/`SetCustomData`) layer over the global config while a database is open. `AppModel.globalCfg` is the config file; `AppModel.cfg` is what is in effect (`globalCfg.Apply(dbOverrides)`, via `setOverrides`, cleared on lock). Only `config.json`'s global values may be saved to it: settings saves and the preset picker's default go through `ConfigSavedMsg`/`SetDefaultPresetMsg`, never `cfg.Save()` on the merged config. Changing database settings marks the database dirty. Theme, strength display, privacy mode and keybindings are global only.

## Code Conventions

- Follow standard Go conventions: https://go.dev/doc/effective_go
- Exported identifiers use `PascalCase`; unexported use `camelCase`
- Package names are lowercase, single-word where possible
- Error handling: wrap errors with `fmt.Errorf("context: %w", err)`
- TUI models implement the `tea.Model` interface (`Init`, `Update`, `View`)
- Charm v2 views return `tea.View`; nested string composition uses `.View().Content`. Only `AppModel.View` sets alternate-screen and mouse modes. Inputs use virtual cursors so their positions remain local to the rendered strings.
- Handle key presses with `tea.KeyPressMsg`, not the `tea.KeyMsg` interface (which includes releases). Forward non-key input messages as well: bracketed paste is `tea.PasteMsg`, and clipboard paste commands/cursor ticks also need to reach active inputs. Keep paste events in the save-input gate and auto-lock activity tracking.
- Set `viewport.FillHeight = true` where fixed-height layout is required; v2 no longer fills empty rows by default.
- Use `layout.go` for terminal-cell-aware frames, whole-item hints, and filename-preserving paths. Screens start with `header(width, right, crumbs...)`, one bar in the status bar's colors (the app name, then where the user is). Footers use `renderHints` (one row, key in the accent color; pass essentials and the help key first, the rest stay in F1 help). Draw a selected list row with `selectedRow` (a full-width bar; the marker takes the same two cells as the unselected gutter, so rows never shift) and align list columns with `padCell`; text laid over a background (bars, the status bar) goes through `onBackground`, since a styled segment's reset would otherwise clear it. Reserve footer height before sizing scrollable content; keep the application status bar on exactly one row (a status message that fits whole gets the widest context it fits beside, so remedies are not cut off). New/returning views receive dimensions through `AppModel.Update`, not only terminal resize events. Form scrolling must follow focus, and mouse coordinates must account for the scroll offset.
- Help is an overlay: `CloseHelpMsg` restores the previous screen, including unsaved form input. `F1` is safe inside inputs; do not steal printable `?`. Forward non-input messages to the underlying views so blink, secret re-mask, TOTP, and resize updates continue without restarting timers. Quit prompts preserve any pending confirmation and its return context.
- Construct confirmations with an explicit return target; the target belongs to `ConfirmModel`, not shared app state. The app draws a confirmation as a centered box over its return screen, dimmed; `ConfirmModel.dialog` lays it out and owns the buttons' click areas. Mark a dialog whose Yes deletes or overwrites with `.Destructive()`. `viewNone` is zero and is not an overlay parent. Keep input gated during encryption and show wait feedback for blocked input rather than reading the changing database.
- Store clamped scroll offsets during updates, using the same bounds as rendering; do not repair scrolling only in a value-receiver `View`. Track form rows incrementally and keep mouse hit-testing aligned with the rendered window.
- Security countdowns use one generation-guarded display tick, independent of the clear/lock timers. Display ticks must not count as activity or erase status messages. Clipboard timers use generations so copying the same value again cannot be cleared by an earlier timer; tests never read or write the user's configuration: the `internal/ui` and `internal/config` TestMains point `HOME` and `XDG_CONFIG_HOME` at a scratch directory, because config lookup also falls back to `~/.config` and, on macOS, to `~/Library/Application Support` (setting only `XDG_CONFIG_HOME` is not enough); clipboard tests use an injected backend, never the desktop clipboard (`internal/ui` TestMain installs a refusing process clipboard and terminal; pasteboard tests use private named pasteboards; Linux backend tests inject a fake X11 owner and fake tool runners, and the detach and timeout tests run `/bin/sh` in xclip's place). X11 tests that need a real server run only against `KEETUI_X11_TEST_DISPLAY` (CI starts `Xvfb :99 -noreset`) and skip otherwise; never read `DISPLAY` in tests. tmux tests inject the `tmuxRunner` and tty writer and never start tmux (the `runTmuxAt` bound test runs scripts named tmux); probe fixtures are trimmed real outputs of the whole probe chain, with expectations from each capture's setup, never re-derived from the parser. Session detection tests inject the process table, tmux environment and `getenv`; the one real process scan re-executes the test binary as a fake herdr client (`KEETUI_TEST_FAKE_HERDR`, handled in TestMain). Never start herdr, tmux or sshd in tests.
- Entry counts must not score passwords. Stats/audit workers analyze a detached `HealthSnapshot`, never the mutable live DB. Cancel scans when leaving their view or locking/quitting, keep them alive under overlays, and reject stale results by generation.
- Save keybindings as sparse overrides and refresh the browser's cached keys after settings changes. Loading repairs only conflicting overrides; navigation must not dismiss a failed settings-save warning (full details remain in F1 help).
- Cross-view communication uses typed message structs (e.g., `type FooMsg struct{}`). A message that acts on the open database or shows what is in it belongs in `needsDatabase`: while locked (`m.db == nil`) such messages are dropped, since a command running at lock time would otherwise reopen an entry or use the closed database; locking also forgets the screens that show it (`dropDatabaseViews`). Auto-lock and the manual lock (`lock` binding, `Ctrl+L`) share `lockDatabase`; the manual one works only where `lockable` allows (browsing screens and help over them, never a form or a pending question), asks before saving unsaved changes (`confirmLockMsg` saves through `handleSaveAs`, then `lockAfterSave` locks once `saveResultMsg` reports success), and waits for a save in flight
- Styles are centralized in `internal/ui/styles.go`, themes in `internal/ui/themes.go`; `ApplyTheme` builds every style, so a new style needs a theme color there, not a literal. Always apply `themeFor(cfg)`, which resolves `auto` and lays the `colors` overrides of the resolved theme over it; a new color role needs an entry in `config.ColorRoles` and in `Theme.set`. The `auto` theme follows the terminal background (`tea.RequestBackgroundColor` in `Init`, `lightBackground`); re-pull styles captured at construction when it changes. A theme chosen by name whose tone differs from the terminal's reported background is painted on its `Canvas` color (`canvasStyle`/`paintCanvas` in `AppModel.View`), so its text colors stay readable
- Always run `gofmt -w .` before committing (enforced by pre-commit hook)

## Git Conventions

- **Default branch**: `main`
- Write clear, imperative commit messages (e.g., "Add entry deletion with confirmation")
- Keep commits focused on a single logical change
- Pre-commit hook runs `gofmt` check automatically (requires `git config core.hooksPath .githooks`)

## Release Process

1. Tag the commit: `git tag v0.X.Y && git push origin v0.X.Y`
2. The `release.yml` workflow builds binaries for all platforms and creates a GitHub release with checksums
3. Update the Homebrew formula in `rmarchei/homebrew-tap`:
   - Download `SHA256SUMS` from the release: `gh release download v0.X.Y --pattern SHA256SUMS --output -`
   - Update `version` and all `sha256` values in `Formula/keetui.rb`
   - Commit and push

## Related Repositories

- **Homebrew tap**: [rmarchei/homebrew-tap](https://github.com/rmarchei/homebrew-tap) — Homebrew tap for installing keetui (local path: `../homebrew-tap`)

## Key Notes for AI Assistants

- Always run `go vet ./...` and `go test ./...` after making changes
- Always run `gofmt -w .` before committing
- When adding a new view, create a new file in `internal/ui/`, implement `tea.Model`, and add the view state and message handling to `app.go`
- When adding new KeePass operations, add them to the appropriate file in `internal/keepass/` with corresponding tests
- Attachment references are binary pool IDs, valid only until the next save: encoding drops unreferenced binaries and renumbers the rest. Never keep raw entries/groups across saves; undo of a permanent delete uses `SnapshotEntry`/`SnapshotGroup` and `InsertEntrySnapshot`/`InsertGroupSnapshot`, which keep copies of the pooled binaries and put them back as stored; restoring a history version (`RestoreHistory`, which adds the current state to the history first) is undone with `ReplaceEntrySnapshot` of a snapshot taken before. Re-adding decoded contents instead would drop their protection and, with the pinned fork, alter KDBX 4 attachments that look like base64 and truncate KDBX 3.1 gzip streams
- The `keepass.DB` struct holds a reference to the underlying `gokeepasslib.Database` — call `LockProtectedEntries()` before encoding and `UnlockProtectedEntries()` after decoding. Every save needs fresh seeds, IV, KDF salt and inner stream key, which gokeepasslib only draws for new headers: `SaveAs` refreshes them before locking, since changing the inner stream key while values are locked garbles them
- `go.mod` retains the `rmarchei/gokeepasslib` fork (local clone: `../gokeepasslib`), based on upstream v3.7.1. Protected KDBX 3.1 binaries are supported upstream, but the fork still handles empty KeeWeb timestamps, moves unused-binary cleanup after decryption to prevent corruption on save, writes (and on save repairs) complete gzip trailers for KDBX 3.1 attachments, and returns KDBX 4 attachments raw instead of base64-decoding them. Drop the replace only when all of these fixes are available upstream and `TestKeewebCompatibility` passes. Its fixtures are synthetic; never substitute a user's database
- All DB-sourced strings rendered in the TUI must go through `sanitizeDisplay()` / `sanitizeNoteLines()` (`internal/ui/sanitize.go`) — raw control bytes corrupt the Bubble Tea frame. Clipboard/QR paths use raw values
- Changes made elsewhere (`keepass/disk.go`, `sync.go`): `DB.disk` (`diskStamp`) is what keetui last read from or wrote to `Path`: a SHA-256 of the bytes, the credentials the file is encrypted with, and its entries' modification times and group UUIDs. Open, Create, every successful write and `MergeDisk` renew it. `SaveAs` on the database's own file runs `CheckDisk` first and returns a `*DiskError` (`DiskChanged`, `DiskMissing`, `DiskNoFolder`) instead of writing; `Overwrite` skips the check; a successful `SaveAs` makes its path the database's. `MergeDisk` reads the file with the stamp's credentials and merges it in (`syncer`): entries by UUID, newer `LastModificationTime` wins with both histories joined (`joinHistory`), newer `LocationChanged` moves entries and groups, an entry deleted here (in the stamp, gone here) comes back only if changed there since, an entry deleted there goes only when their `DeletedObjects` records it after its last change here (`applyDeletions`; then groups left empty), one merely missing there is kept (an older copy must not delete), attachments go through the snapshot repooling, and a newer file format (KeePassXC upgrades 3.1 to 4.x on save; 4.1 fields such as `PreviousParentGroup` cannot be written to 3.1) is adopted with its header first (`adoptFormat`). Custom icons the file has join (`joinCustomIcons`, shared with the CLI `Merge`; an older target drops the 4.1 icon fields), name, description and default user name follow their `*Changed` times, and the metadata custom data merges item by item, three-way against `diskStamp.customData` (`syncCustomData`: a change or removal on one side wins, both changed keeps the newer 4.1 time, else ours); `mergedIn` re-reads the per-database settings. keetui records its own permanent deletions in `DeletedObjects` (`recordDeleted`: permanent deletes, deletes without a recycle bin, emptying the bin) and drops the record when undo puts the item back (`forgetDeleted` in `Insert*Snapshot`). Verified against keepassxc-cli 2.7 in a container (its CLI merge applies no deletions, even its own: not synchronize mode). It relies on keetui recording its own changes: `touch` an entry on any change (favorite, attachments), and moves set `LocationChanged`. In the app a `DiskError` from a save becomes a question (`askDisk`: merge, then `diskOverwriteAskMsg`; `ConfirmModel.OnNo` gives No its own action while esc cancels); `saveOnDisk` runs the answer gated like any save, and `mergedIn` marks the database dirty, clears undo and refreshes. Auto-lock merges without asking (`saveBeforeLock`)
- `keepass.Open`/`OpenWithKeyFile` set `DB.SuspectCorruption()` when decoded protected values look like binary garbage; the app then forces read-only and headless mutations refuse to run
- The editor uses `ctrl+` keybindings (ctrl+s, ctrl+r, ctrl+g, etc.) because regular keys go to text inputs
- Remote sessions (`clipboard_remote.go`, evaluated per copy): in a herdr pane, herdr client processes of the same user decide (executable named `herdr` or like `$HERDR_BIN_PATH`, any version; argv whitelist, never `herdr server`; only clients of keetui's server, by the client socket herdr would resolve from argv and environment; `/proc` on Linux, `kern.proc.uid` and `kern.procargs2` on macOS): remote if one has `SSH_CONNECTION`/`SSH_TTY` or is `remote-client-bridge` (`remoteSession.herdrSSH`); with clients at both ends, a local client whose terminal had input within `localInputWindow` (9 s: `processTable.lastInput`, the tty access time, which Linux updates per 8-second slot) keeps the copy local and marked, since herdr hands OSC 52 to the client that last had input and a local herdr client writes the clipboard unmarked; a local copy in a herdr pane goes to the display of the local client that typed last (`remoteSession.desktop`, only `desktopVars` kept from its environment; Linux `localRoute`, whose `nativeRoute.env` every tool run for that copy uses, and `nativeRoute.client` names the client in F1), since panes keep the server's environment, which has no display when the server was started over SSH; then the tmux session environment (in tmux started from a herdr pane, either one saying remote counts), an `sshd`/`sshd-session` ancestor, and keetui's own SSH variables. macOS uses the pasteboard only locally. Linux over SSH ignores `WAYLAND_DISPLAY` and uses only a forwarded X display (host part, display number >= 10; tmux session `DISPLAY` and `XAUTHORITY` first), with an OSC 52 leg in `auto`, no xclip/xsel fallback and no exit hand-off. keetui keeps one X11 owner per display (`x11Slot`) until exit, blanking the old one when the display changes, never closing it. A failed native copy, and every clear of a native copy, in a remote session repaints the screen (`tea.ClearScreen`, `clearResult.repaint`): the ssh client prints over the frame when the forwarded display is gone; a forwarded server that went away (`Owner.Lost`) counts as cleared, and is not asked again
- Clipboard: macOS writes NSPasteboard in-process (`internal/pasteboard`, purego) with Concealed/Transient markers and current-host-only, tracking ownership by changeCount; `/usr/bin/pbcopy` is an unmarked fallback. Linux uses `wl-copy` (with `--sensitive` when supported) on Wayland. On X11, `internal/x11clip` (xgb; imported only from `clipboard_linux.go`, so darwin binaries do not link it; it dials and looks up the cookie itself, as libxcb does, from `Owner.Authority` or `$XAUTHORITY`) owns CLIPBOARD from a window named `keetui`, offers `x-kde-passwordManagerHint=secret`, and tracks ownership through the server (SelectionClear, `GetSelectionOwner`). Never let the X11 clipboard go ownerless while it may hold a secret, because history managers that ignore the marker restore it: clearing blanks the copy and keeps ownership (reusing the acquisition timestamp, so a newer copy elsewhere is never taken), a timeout never drops the connection, a failed `Copy` blanks the previous copy, and at exit the `nativeCloser` hook hands an empty copy to `xclip` (except on a display forwarded over SSH). Every X round trip, xclip/xsel run (`pipeDetached`, in its own session via `Setsid` so it survives keetui's terminal hangup) and clipboard read-back (`outputBounded`) is bounded to 2 s; an unanswered ownership or content check leaves the copy to the clear. xclip, then xsel, are unmarked fallbacks (owner failure other than a timeout, values over one X request: Copy serves no INCR); never clear with `xsel --clear` while the owner or xclip can take an empty copy. `Read` drains INCR transfers in the event loop, so no clipboard text stays in a property on keetui's window, and never deletes a property named by a SelectionNotify, which any client can forge. Keep `xgb.Logger` silenced: its stderr output corrupts the frame. Backends remember copies as a keyed hash and clear only their own copy; a copy that does not replace the previous one (local, then terminal-only after the session turned remote, or the reverse) clears what that one left (`clearSuperseded`), and a failed local clear keeps the copy tracked for the next release; `main` calls `ui.ReleaseClipboard()` (bounded to 5 s) after `Run` and turns SIGHUP into a quit
- Terminal clipboard: OSC 52 is sent only per `clipboard_osc52`, through `terminalRoutes`: raw outside tmux, never raw inside it (`$TMUX`), where tmux drops it or keeps it in a paste buffer. `clipboard_tmux.go` probes tmux once per copy and clear (one invocation chaining `display-message`, `show-messages -T`, `list-windows -a` and `list-clients`; 2 s timeout plus `cmd.WaitDelay`), then uses passthrough or a self-deleting `load-buffer -w` (secret on stdin only; `delete-buffer` always with `-b`, chained only on 3.4+, else only after a successful load, since 3.2a deletes the newest buffer for a missing name). Recipients are counted the way tmux delivers passthrough: awake clients whose session shows keetui's window (`all`: holds it, grouped and linked sessions included), none from a pane in a mode, with `on` none past another zoomed pane, a popup's only to its own client; suspended clients get nothing (`client_flags` names them from 3.2). Copies record their recipients' ttys (`terminalResult.reached`). A clear tmux would drop (`clearDeferred`) or that would miss a recipient still attached (`clearAway`) keeps the held copy and is retried on the display tick and in `ReleaseClipboard`. A clear is never resent once out, since it could wipe a newer copy, and is not sent when it would reach only terminals that never got the copy (`clearDetached`), except a terminal that reconnected on a new tty with the same `TERM` and XTVERSION reply and was not attached at copy time (`clearReattached`). Without `$TMUX`, an XTVERSION reply starting `tmux ` (requested in `Init`; `terminalKind` keeps its version) means passthrough, reported unverified with the remedy unless the version is below 3.3; a failed probe does the same. With `always`, both also send a plain OSC 52 after the passthrough, and their clears both empties. With `$TMUX` set, a non-tmux reply means an inherited `$TMUX`, and terminal copies are refused, never sent raw. In herdr panes OSC 52 goes raw; herdr drops an empty one, so the clear copies a single space, only while a herdr client runs over SSH (`clearOverwritten`, else `clearNoRemoteClient`); tmux started from a herdr pane does the same with passthrough, and the herdr leg of a local copy made with only local herdr clients counts as cleared (`clearHerdrLocal`). The terminal leg of a local copy is cleared once after `clearDone` (`clearResult.leg`), never retried. Sequences go to `/dev/tty` in one blocking write, not `tea.Raw`, whose `TEA_TRACE` log would record them; passthrough first waits `passthroughSettle`, because tmux drops passthrough that lands right behind a synchronized-output frame until the pane redraws.
- Do not commit binary artifacts, coverage files, or `.env` files
