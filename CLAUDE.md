# CLAUDE.md - AI Assistant Guide for keetui

## Project Overview

**keetui** is a terminal user interface (TUI) for reading and writing KeePass `.kdbx` database files. It is written in Go and targets macOS and Linux.

## Repository Structure

```
keetui/
├── cmd/
│   └── keetui/
│       └── main.go              # CLI entry point, argument parsing
├── internal/
│   ├── keepass/
│   │   ├── keepass.go           # KeePass DB operations (open, save, CRUD)
│   │   └── keepass_test.go      # Tests for keepass package
│   └── ui/
│       ├── app.go               # Top-level app model, view orchestration
│       ├── browser.go           # Group/entry list browser with drill-down navigation
│       ├── confirm.go           # Yes/no confirmation dialog
│       ├── detail.go            # Read-only entry detail view
│       ├── editor.go            # Entry create/edit form
│       ├── group_input.go       # New group name input
│       ├── styles.go            # Lip Gloss style definitions
│       └── unlock.go            # Password entry / unlock screen
├── go.mod
├── go.sum
├── .gitignore
├── README.md
└── CLAUDE.md
```

## Key Dependencies

| Package | Purpose |
|---------|---------|
| `github.com/tobischo/gokeepasslib/v3` | KeePass .kdbx file reading/writing |
| `github.com/charmbracelet/bubbletea` | TUI framework (Elm-style MVU architecture) |
| `github.com/charmbracelet/bubbles` | TUI components (text input) |
| `github.com/charmbracelet/lipgloss` | Terminal styling |

## Architecture

The application follows the **Bubble Tea Model-View-Update (MVU)** pattern:

- **`internal/keepass`** — Pure data layer. Wraps `gokeepasslib` with a simplified API (`Open`, `Save`, `AddEntry`, `UpdateEntry`, `DeleteEntry`, `AddGroup`, `DeleteGroup`). No TUI dependencies.
- **`internal/ui`** — TUI layer. Each screen is a separate Bubble Tea model (`UnlockModel`, `BrowserModel`, `DetailModel`, `EditorModel`, etc.). The `AppModel` in `app.go` orchestrates view switching via message passing.
- **`cmd/keetui`** — CLI entry point. Parses flags and launches the TUI program.

### View Flow

```
UnlockModel → BrowserModel ↔ DetailModel
                   ↕              ↓
              GroupInputModel  EditorModel
                   ↕              ↕
              ConfirmModel    ConfirmModel
```

### Message Passing

Views communicate through typed messages (e.g., `ViewEntryMsg`, `SaveEntryMsg`, `BackToBrowserMsg`). The `AppModel.Update()` method handles cross-view messages and delegates view-specific messages to the active sub-model.

## Development Workflow

### Building

```sh
go build ./...                          # check compilation
go build -o keetui ./cmd/keetui         # build binary
```

### Running

```sh
./keetui database.kdbx
./keetui -keyfile key.key database.kdbx
```

### Testing

```sh
go test ./...                           # run all tests
go test -v ./internal/keepass/...       # run keepass tests with verbose output
```

### Formatting & Vetting

```sh
gofmt -w .
go vet ./...
```

## Code Conventions

- Follow standard Go conventions: https://go.dev/doc/effective_go
- Exported identifiers use `PascalCase`; unexported use `camelCase`
- Package names are lowercase, single-word where possible
- Error handling: wrap errors with `fmt.Errorf("context: %w", err)`
- TUI models implement the `tea.Model` interface (`Init`, `Update`, `View`)
- Cross-view communication uses typed message structs (e.g., `type FooMsg struct{}`)
- Styles are centralized in `internal/ui/styles.go`

## Git Conventions

- **Default branch**: `master`
- Write clear, imperative commit messages (e.g., "Add entry deletion with confirmation")
- Keep commits focused on a single logical change

## Key Notes for AI Assistants

- Always run `go vet ./...` and `go test ./...` after making changes
- When adding a new view, create a new file in `internal/ui/`, implement `tea.Model`, and add the view state and message handling to `app.go`
- When adding new KeePass operations, add them to `internal/keepass/keepass.go` with corresponding tests in `keepass_test.go`
- The `keepass.DB` struct holds a reference to the underlying `gokeepasslib.Database` — call `LockProtectedEntries()` before encoding and `UnlockProtectedEntries()` after decoding
- Clipboard support uses platform-specific commands (`pbcopy` on macOS, `xclip`/`xsel`/`wl-copy` on Linux)
- Do not commit binary artifacts, coverage files, or `.env` files
