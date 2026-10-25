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
│   │   ├── diceware.go          # Diceware passphrase generation
│   │   ├── export.go            # CSV/XML import and export
│   │   ├── hibp.go              # Have I Been Pwned breach check
│   │   ├── merge.go             # Merge from another .kdbx file
│   │   ├── snapshot.go          # Self-contained entry/group copies for undo
│   │   ├── stats.go             # Database statistics
│   │   ├── strength.go          # Password strength evaluation
│   │   └── *_test.go            # Corresponding tests for each
│   ├── x11clip/
│   │   ├── owner.go             # In-process X11 CLIPBOARD owner (xgb): copy, blank, read-back (INCR)
│   │   ├── serve.go             # Selection targets: TARGETS, UTF8_STRING, STRING, hint, MULTIPLE
│   │   └── *_test.go            # Unit tests; xvfb_test.go needs KEETUI_X11_TEST_DISPLAY
│   └── ui/
│       ├── app.go               # Top-level app model, view orchestration
│       ├── audit.go             # Security audit results view
│       ├── browser.go           # Group/entry list browser with navigation
│       ├── clipboard*.go        # Clipboard routing (local, OSC 52, tmux), ownership, macOS/Linux backends
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
make                                    # build via Makefile (uses container if available)
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
| `password_length` | 20 | Default length for generated passwords (min: 8) |
| `show_password_strength` | true | Show password strength indicator in detail and editor views |
| `default_preset` | `"Strong"` | Default password preset: `PIN`, `Alphanumeric`, `Strong`, `Passphrase`, `Custom` |
| `theme` | `"dark"` | Color theme: `dark`, `light`, or `high-contrast` |
| `confirm_quit` | true | Confirm before quitting, even without unsaved changes (`false`: disable) |
| `backup_on_save` | false | Create `.kdbx.bak` backup before each save |
| `keybindings` | (object) | Override default keybindings (see `keybindings.go` for keys) |

- **CLI**: Run `keetui -config` to display the config file path and current settings as valid JSON.
- **TUI**: Press `c` in the browser view to open the settings editor. Changes are saved to disk and take effect immediately.
- If the config file does not exist, defaults are used. The file is created automatically when settings are saved from the TUI.

## Code Conventions

- Follow standard Go conventions: https://go.dev/doc/effective_go
- Exported identifiers use `PascalCase`; unexported use `camelCase`
- Package names are lowercase, single-word where possible
- Error handling: wrap errors with `fmt.Errorf("context: %w", err)`
- TUI models implement the `tea.Model` interface (`Init`, `Update`, `View`)
- Charm v2 views return `tea.View`; nested string composition uses `.View().Content`. Only `AppModel.View` sets alternate-screen and mouse modes. Inputs use virtual cursors so their positions remain local to the rendered strings.
- Handle key presses with `tea.KeyPressMsg`, not the `tea.KeyMsg` interface (which includes releases). Forward non-key input messages as well: bracketed paste is `tea.PasteMsg`, and clipboard paste commands/cursor ticks also need to reach active inputs. Keep paste events in the save-input gate and auto-lock activity tracking.
- Set `viewport.FillHeight = true` where fixed-height layout is required; v2 no longer fills empty rows by default.
- Use `layout.go` for terminal-cell-aware frames, whole-item hints, and filename-preserving paths. Reserve footer height before sizing scrollable content; keep the application status bar on exactly one row. New/returning views receive dimensions through `AppModel.Update`, not only terminal resize events. Form scrolling must follow focus, and mouse coordinates must account for the scroll offset.
- Help is an overlay: `CloseHelpMsg` restores the previous screen, including unsaved form input. `F1` is safe inside inputs; do not steal printable `?`. Forward non-input messages to the underlying views so blink, secret re-mask, TOTP, and resize updates continue without restarting timers. Quit prompts preserve any pending confirmation and its return context.
- Construct confirmations with an explicit return target; the target belongs to `ConfirmModel`, not shared app state. `viewNone` is zero and is not an overlay parent. Keep input gated during encryption and show wait feedback for blocked input rather than reading the changing database.
- Store clamped scroll offsets during updates, using the same bounds as rendering; do not repair scrolling only in a value-receiver `View`. Track form rows incrementally and keep mouse hit-testing aligned with the rendered window.
- Security countdowns use one generation-guarded display tick, independent of the clear/lock timers. Display ticks must not count as activity or erase status messages. Clipboard timers use generations so copying the same value again cannot be cleared by an earlier timer; clipboard tests use an injected backend, never the desktop clipboard (`internal/ui` TestMain installs a refusing process clipboard and terminal; pasteboard tests use private named pasteboards; Linux backend tests inject a fake X11 owner and fake tool runners, and the detach and timeout tests run `/bin/sh` in xclip's place). X11 tests that need a real server run only against `KEETUI_X11_TEST_DISPLAY` (CI starts `Xvfb :99 -noreset`) and skip otherwise; never read `DISPLAY` in tests. tmux tests inject the `tmuxRunner` and tty writer and never start tmux (the `runTmuxAt` bound test runs scripts named tmux); probe fixtures are trimmed real outputs of the whole probe chain, with expectations from each capture's setup, never re-derived from the parser.
- Entry counts must not score passwords. Stats/audit workers analyze a detached `HealthSnapshot`, never the mutable live DB. Cancel scans when leaving their view or locking/quitting, keep them alive under overlays, and reject stale results by generation.
- Save keybindings as sparse overrides and refresh the browser's cached keys after settings changes. Loading repairs only conflicting overrides; navigation must not dismiss a failed settings-save warning (full details remain in F1 help).
- Cross-view communication uses typed message structs (e.g., `type FooMsg struct{}`)
- Styles are centralized in `internal/ui/styles.go`, themes in `internal/ui/themes.go`
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
- Attachment references are binary pool IDs, valid only until the next save: encoding drops unreferenced binaries and renumbers the rest. Never keep raw entries/groups across saves; undo of a permanent delete uses `SnapshotEntry`/`SnapshotGroup` and `InsertEntrySnapshot`/`InsertGroupSnapshot`, which keep copies of the pooled binaries and put them back as stored. Re-adding decoded contents instead would drop their protection and, with the pinned fork, alter KDBX 4 attachments that look like base64 and truncate KDBX 3.1 gzip streams
- The `keepass.DB` struct holds a reference to the underlying `gokeepasslib.Database` — call `LockProtectedEntries()` before encoding and `UnlockProtectedEntries()` after decoding. Every save needs fresh seeds, IV, KDF salt and inner stream key, which gokeepasslib only draws for new headers: `SaveAs` refreshes them before locking, since changing the inner stream key while values are locked garbles them
- `go.mod` retains the `rmarchei/gokeepasslib` fork (local clone: `../gokeepasslib`), based on upstream v3.7.1. Protected KDBX 3.1 binaries are supported upstream, but the fork still handles empty KeeWeb timestamps, moves unused-binary cleanup after decryption to prevent corruption on save, writes (and on save repairs) complete gzip trailers for KDBX 3.1 attachments, and returns KDBX 4 attachments raw instead of base64-decoding them. Drop the replace only when all of these fixes are available upstream and `TestKeewebCompatibility` passes. Its fixtures are synthetic; never substitute a user's database
- All DB-sourced strings rendered in the TUI must go through `sanitizeDisplay()` / `sanitizeNoteLines()` (`internal/ui/sanitize.go`) — raw control bytes corrupt the Bubble Tea frame. Clipboard/QR paths use raw values
- `keepass.Open`/`OpenWithKeyFile` set `DB.SuspectCorruption()` when decoded protected values look like binary garbage; the app then forces read-only and headless mutations refuse to run
- The editor uses `ctrl+` keybindings (ctrl+s, ctrl+r, ctrl+g, etc.) because regular keys go to text inputs
- Clipboard: macOS writes NSPasteboard in-process (`internal/pasteboard`, purego) with Concealed/Transient markers and current-host-only, tracking ownership by changeCount; `/usr/bin/pbcopy` is an unmarked fallback. Linux uses `wl-copy` (with `--sensitive` when supported) on Wayland. On X11, `internal/x11clip` (xgb; imported only from `clipboard_linux.go`, so darwin binaries do not link it) owns CLIPBOARD from a window named `keetui`, offers `x-kde-passwordManagerHint=secret`, and tracks ownership through the server (SelectionClear, `GetSelectionOwner`). Never let the X11 clipboard go ownerless while it may hold a secret, because history managers that ignore the marker restore it: clearing blanks the copy and keeps ownership (reusing the acquisition timestamp, so a newer copy elsewhere is never taken), a timeout never drops the connection, a failed `Copy` blanks the previous copy, and at exit the `nativeCloser` hook hands an empty copy to `xclip`. Every X round trip, xclip/xsel run (`pipeDetached`, in its own session via `Setsid` so it survives keetui's terminal hangup) and clipboard read-back (`outputBounded`) is bounded to 2 s; an unanswered ownership or content check leaves the copy to the clear. xclip, then xsel, are unmarked fallbacks (owner failure other than a timeout, values over one X request: Copy serves no INCR); never clear with `xsel --clear` while the owner or xclip can take an empty copy. `Read` drains INCR transfers in the event loop, so no clipboard text stays in a property on keetui's window, and never deletes a property named by a SelectionNotify, which any client can forge. Keep `xgb.Logger` silenced: its stderr output corrupts the frame. Backends remember copies as a keyed hash and clear only their own copy; `main` calls `ui.ReleaseClipboard()` (bounded to 5 s) after `Run` and turns SIGHUP into a quit
- Terminal clipboard: OSC 52 is sent only per `clipboard_osc52`, through `terminalRoutes`: raw outside tmux, never raw inside it (`$TMUX`), where tmux drops it or keeps it in a paste buffer. `clipboard_tmux.go` probes tmux once per copy and clear (one invocation chaining `display-message`, `show-messages -T`, `list-windows -a` and `list-clients`; 2 s timeout plus `cmd.WaitDelay`), then uses passthrough or a self-deleting `load-buffer -w` (secret on stdin only; `delete-buffer` always with `-b`, chained only on 3.4+, else only after a successful load, since 3.2a deletes the newest buffer for a missing name). Recipients are counted the way tmux delivers passthrough: awake clients whose session shows keetui's window (`all`: holds it, grouped and linked sessions included), none from a pane in a mode, with `on` none past another zoomed pane, a popup's only to its own client; suspended clients get nothing (`client_flags` names them from 3.2). Copies record their recipients' ttys (`terminalResult.reached`). A clear tmux would drop (`clearDeferred`) or that would miss a recipient still attached (`clearAway`) keeps the held copy and is retried on the display tick and in `ReleaseClipboard`. A clear is never resent once out, since it could wipe a newer copy, and is not sent when it would reach only terminals that never got the copy (`clearDetached`), except a terminal that reconnected on a new tty with the same `TERM` and XTVERSION reply and was not attached at copy time (`clearReattached`). Without `$TMUX`, an XTVERSION reply starting `tmux ` (requested in `Init`) means passthrough only, reported unverified; with `$TMUX` set, a non-tmux reply means an inherited `$TMUX`, and terminal copies are refused, never sent raw. Sequences go to `/dev/tty` in one blocking write, not `tea.Raw`, whose `TEA_TRACE` log would record them; passthrough first waits `passthroughSettle`, because tmux drops passthrough that lands right behind a synchronized-output frame until the pane redraws.
- Do not commit binary artifacts, coverage files, or `.env` files
