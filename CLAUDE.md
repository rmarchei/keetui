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
│   ├── keepass/
│   │   ├── keepass.go           # KeePass DB operations (open, save, CRUD)
│   │   ├── keepass_test.go
│   │   ├── attachment.go        # File attachment operations
│   │   ├── audit.go             # Batch password audit (weak, reused, empty)
│   │   ├── diceware.go          # Diceware passphrase generation
│   │   ├── export.go            # CSV/XML import and export
│   │   ├── hibp.go              # Have I Been Pwned breach check
│   │   ├── merge.go             # Merge from another .kdbx file
│   │   ├── stats.go             # Database statistics
│   │   ├── strength.go          # Password strength evaluation
│   │   └── *_test.go            # Corresponding tests for each
│   └── ui/
│       ├── app.go               # Top-level app model, view orchestration
│       ├── audit.go             # Security audit results view
│       ├── browser.go           # Group/entry list browser with navigation
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

CI, release builds, and the Makefile's container image are pinned to Go 1.27.1.
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
- Store clamped scroll offsets during updates, using the same bounds as rendering; do not repair scrolling only in a value-receiver `View`. Track form rows incrementally and keep mouse hit-testing aligned with the rendered window.
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
- The `keepass.DB` struct holds a reference to the underlying `gokeepasslib.Database` — call `LockProtectedEntries()` before encoding and `UnlockProtectedEntries()` after decoding
- `go.mod` retains the `rmarchei/gokeepasslib` fork (local clone: `../gokeepasslib`), based on upstream v3.7.0 plus subsequent dependency updates. Protected KDBX 3.1 binaries are supported upstream, but the fork still handles empty KeeWeb timestamps and moves unused-binary cleanup after decryption to prevent corruption on save. Drop the replace only when both fixes are available upstream and `TestKeewebCompatibility` passes. Its fixtures are synthetic; never substitute a user's database
- All DB-sourced strings rendered in the TUI must go through `sanitizeDisplay()` / `sanitizeNoteLines()` (`internal/ui/sanitize.go`) — raw control bytes corrupt the Bubble Tea frame. Clipboard/QR paths use raw values
- `keepass.Open`/`OpenWithKeyFile` set `DB.SuspectCorruption()` when decoded protected values look like binary garbage; the app then forces read-only and headless mutations refuse to run
- The editor uses `ctrl+` keybindings (ctrl+s, ctrl+r, ctrl+g, etc.) because regular keys go to text inputs
- Clipboard support uses platform-specific commands (`pbcopy` on macOS, `xclip`/`xsel`/`wl-copy` on Linux)
- Do not commit binary artifacts, coverage files, or `.env` files
