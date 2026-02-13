# CLAUDE.md - AI Assistant Guide for keetui

## Project Overview

**keetui** is a Go project (currently in early/initial stage). The repository is set up with Go-specific tooling and conventions.

## Repository Structure

```
keetui/
├── .gitignore          # Go-specific ignore patterns
├── README.md           # Project readme
└── CLAUDE.md           # This file
```

As the project grows, expect standard Go project layout:
- `cmd/` — main application entry points
- `internal/` — private application code
- `pkg/` — library code safe for external use
- `go.mod` / `go.sum` — Go module dependency files

## Language & Tooling

- **Language**: Go
- **Module system**: Go modules (initialize with `go mod init`)
- **Build**: `go build`
- **Test**: `go test ./...`
- **Lint**: Use `go vet ./...` and `staticcheck` if available

## Development Workflow

### Building
```sh
go build ./...
```

### Running Tests
```sh
go test ./...
```

### Formatting
```sh
gofmt -w .
# or
goimports -w .
```

### Vetting
```sh
go vet ./...
```

## Code Conventions

- Follow standard Go conventions: https://go.dev/doc/effective_go
- Exported identifiers use `PascalCase`; unexported use `camelCase`
- Package names are lowercase, single-word where possible
- Error handling: check and return errors explicitly; do not ignore them
- Use `context.Context` as the first parameter for functions that do I/O or may be cancelled
- Keep functions focused and short
- Write table-driven tests using the standard `testing` package

## Git Conventions

- **Default branch**: `master`
- Write clear, imperative commit messages (e.g., "Add user authentication endpoint")
- Keep commits focused on a single logical change

## Environment

- `.env` files are gitignored — never commit secrets
- Editor/IDE config directories (`.idea/`, `.vscode/`) are commented out in .gitignore; uncomment if the team agrees to ignore them

## Key Notes for AI Assistants

- This project is in its early stage — there is no source code yet beyond scaffolding
- When adding new code, initialize Go modules first (`go mod init`) if `go.mod` does not exist
- Always run `go vet ./...` and `go test ./...` after making changes
- Respect the existing .gitignore patterns
- Do not commit binary artifacts, coverage files, or `.env` files
