# keetui

A terminal user interface (TUI) for reading and writing KeePass `.kdbx` database files, built with Go.

## Features

- Unlock `.kdbx` databases with password or password + key file
- Create new `.kdbx` databases from scratch
- Browse groups and entries with vim-style navigation
- Search entries across all groups with fuzzy matching (searches title, username, URL, notes, custom fields)
- Favorite/pin entries -- mark entries with `f` key, shown first with `[*]` indicator (uses KeePass Tags)
- Tag filtering -- cycle through tags with `t` to filter entries by any KeePass tag
- Sort options -- cycle through name, modification date, or creation date with `S`
- Entry count per group -- groups show recursive entry count (e.g., `[+] Email (3)`)
- Entries sorted alphabetically by default (favorites first, then groups, then entries)
- View entry details (username, password, URL, notes, custom fields)
- Per-field password toggle -- reveal individual protected fields independently
- TOTP/2FA support -- live code display with countdown, compatible with KeePassXC and KeePass2
- Display any field as a scannable QR code (useful over SSH from a phone)
- Copy username, password, URL, or TOTP code to clipboard (auto-cleared after 15s)
- Mouse support -- click to select fields and entries, scroll wheel navigation
- Open entry URL in system browser
- Password generator with preset picker (PIN, Alphanumeric, Strong, Passphrase, Like current, Custom)
- Password strength indicator with character class breakdown and HIBP breach check
- Create, edit, and delete entries and groups
- Rename groups
- Duplicate entries
- Move entries and groups between groups (cut/paste)
- Custom fields support (add, edit, delete extra key-value pairs)
- File attachments -- add, extract, and delete attachments on entries
- Entry expiry dates with visual warnings for expired entries
- Password history -- view previous passwords for an entry
- Recycle bin support -- deleted items go to recycle bin, with restore and permanent delete
- Database stats and batch password audit (weak, reused, empty, expired)
- Import/export (CSV, XML) and merge from other `.kdbx` files (with `--dry-run` preview)
- Upgrade KDBX 3.1 (AES) databases to KDBX 4 (Argon2) format via TUI or CLI
- Change database master password
- Undo last action with `Ctrl+Z` (delete, edit, move, rename, duplicate, add)
- Status bar showing database name, entry count, and modified state
- Optional backup (`.kdbx.bak`) before every save (enable in settings)
- Password generator character set options (toggle uppercase, lowercase, digits, symbols)
- Read-only mode (`--read-only`) to prevent accidental modifications
- Keyboard shortcut help overlay (`?` key)
- Save changes back to the `.kdbx` file, or save as a new file path
- Auto-lock after 5 minutes of inactivity
- Color themes -- dark, light, and high-contrast presets via config or settings UI
- Configurable keybindings and settings via `~/.config/keetui/config.json`

## Installation

```sh
go install github.com/rmarchei/keetui/cmd/keetui@latest
```

Or build from source:

```sh
git clone https://github.com/rmarchei/keetui.git
cd keetui
go build -o keetui ./cmd/keetui
```

### Build with Make

CI, release builds, and container-based Make targets use **Go 1.27.1**.
macOS binaries built with this toolchain require **macOS 13 Ventura or newer**.
The module retains Go 1.26.0 as its minimum source version; local builds without
a container use the installed Go compiler.

```sh
make                  # build for current OS/arch (uses container if available)
make test             # run tests with race detector (uses container if available)
make lint             # gofmt check + golangci-lint (uses container if available)
make check            # run lint + test (uses container if available)
make all              # cross-compile all platforms (requires docker/podman/container)
make linux-amd64      # build a single target
make install          # install to /usr/local/bin
make clean            # remove built binaries
```

Binaries are written to `dist/`. Override variables as needed:

```sh
VERSION=v1.0.0 make
CONTAINER_RUNTIME=podman make
CONTAINER_RUNTIME= make       # skip container, use local go
```

## Usage

```sh
keetui database.kdbx                    # open existing database
keetui -keyfile key.key database.kdbx   # open with key file (leave password empty for keyfile-only databases)
keetui -new database.kdbx              # create a new database
keetui -new -keyfile key.key database.kdbx  # create a database protected by a key file
keetui -read-only database.kdbx        # open in read-only mode
```

### Import / Export / Merge

Batch operations are available via CLI flags. These run headlessly (no TUI) and require the database password on stdin or via terminal prompt.

```sh
# Export
keetui --export-csv entries.csv database.kdbx
keetui --export-xml entries.xml database.kdbx

# Import (adds entries to existing database)
keetui --import-csv entries.csv database.kdbx
keetui --import-xml entries.xml database.kdbx

# Merge from another .kdbx file
keetui --merge other.kdbx database.kdbx
keetui --merge other.kdbx --dry-run database.kdbx  # preview without modifying
keetui --merge other.kdbx --merge-keyfile other.key database.kdbx  # keyfile-protected source
```

- **CSV** format includes columns: Group, Title, Username, Password, URL, Notes, TOTP, Attachments
- **XML** format preserves group hierarchy, custom fields, and attachments
- **Merge** matches entries by UUID; newer entries overwrite older ones; new groups are created as needed. Merged entries keep their UUID, timestamps, history, and attachments. Use `--dry-run` to preview changes without modifying the database

### Format Upgrade

Upgrade a KDBX 3.1 (AES) database to KDBX 4 (Argon2) format:

```sh
keetui --upgrade-kdbx4 database.kdbx
```

You can also upgrade from the TUI: press `s` to open the save dialog, then `Tab` to the format selector and choose KDBX 4 (Argon2) before saving.

## Keyboard Shortcuts

All keybindings shown below are defaults and can be customized via the config file (see [Keybindings](#keybindings)).

Screens adapt to the terminal size. Settings and entry forms scroll to keep the
focused field visible; use `Tab` / `Shift+Tab` to move between fields. Shortcut
hints use up to two rows, showing essential actions when space is limited.
Press `F1` in any unlocked screen for the full help (and the full current status
message); `Esc` / `F1` returns without losing form edits. `?` also opens help in
the browser and entry detail, but remains ordinary text inside inputs.
Use `PageUp` / `PageDown` to read long unlock errors, and `j` / `k` or
`PageUp` / `PageDown` to scroll database statistics.

### Browser

| Key | Action |
|-----|--------|
| `j` / `k` | Navigate up/down |
| `Enter` / `l` | Open group or view entry |
| `Esc` / `h` | Go back |
| `/` | Search entries (fuzzy, all fields) |
| `S` | Cycle sort mode (name/modified/created) |
| `t` | Filter by tag (cycle through tags) |
| `n` | New entry |
| `g` | New group |
| `r` | Rename group (when cursor is on a group) |
| `y` | Duplicate entry |
| `f` | Toggle favorite on entry |
| `x` | Cut entry or group (for move) |
| `v` | Paste cut item into current group |
| `d` | Delete entry/group |
| `i` | Database stats |
| `I` | Password audit |
| `c` | Settings |
| `P` | Change master password |
| `Ctrl+Z` | Undo last action |
| `s` / `Ctrl+S` | Save database (edit path, select format) |
| `?` | Keyboard shortcut help |
| `q` | Quit |

### Entry Detail

| Key | Action |
|-----|--------|
| `j` / `k` | Navigate fields |
| `Enter` | Copy selected field to clipboard |
| `p` | Toggle visibility of selected protected field |
| `e` | Edit entry |
| `o` | Open URL in system browser |
| `a` | Attachments (add/extract/delete) |
| `H` | Password history |
| `1` | Copy username to clipboard |
| `2` | Copy password to clipboard |
| `3` | Copy URL to clipboard |
| `4` | Copy TOTP code to clipboard |
| `5` | Check password against HIBP breaches |
| `Q` | Display selected field as QR code |
| `?` | Keyboard shortcut help |
| `Esc` | Back to browser |

### Editor

| Key | Action |
|-----|--------|
| `Tab` / `Shift+Tab` | Navigate fields |
| `Ctrl+R` | Toggle password/TOTP visibility |
| `Ctrl+G` | Generate password (preset picker) |
| `Ctrl+A` | Add custom field |
| `Ctrl+T` | Toggle protected (hidden) on custom field |
| `Ctrl+D` | Delete current custom field |
| `Ctrl+S` | Save |
| `Esc` | Cancel |

### Recycle Bin

When browsing inside the recycle bin group, these additional keys are available:

| Key | Action |
|-----|--------|
| `d` | Permanently delete item |
| `R` | Restore item to root group |
| `E` | Empty recycle bin |

## Search

Press `/` in the browser to search across all entries in the database. Matches filter by title, username, URL, notes, and custom fields. Fuzzy matching is supported — typing "git" will match "GitHub". Exact substring matches are shown first, followed by fuzzy matches. Press `Enter` to confirm the search results or `Esc` to cancel.

## TOTP Support

keetui reads TOTP secrets from entries using two common conventions:

- **KeePassXC**: `otp` field containing an `otpauth://` URI
- **KeePass2**: `TimeOtp-Secret-Base32` field containing a raw base32 secret

When viewing an entry with a TOTP secret, a live 6-digit code is displayed with a visual countdown bar. The code refreshes automatically every second.

## Configuration

Settings are stored in `~/.config/keetui/config.json`:

```json
{
  "clipboard_clear_seconds": 15,
  "auto_lock_minutes": 5,
  "password_length": 20,
  "show_password_strength": true,
  "password_uppercase": true,
  "password_lowercase": true,
  "password_digits": true,
  "password_symbols": true,
  "default_preset": "Strong",
  "theme": "dark",
  "confirm_quit": true,
  "backup_on_save": false
}
```

| Setting | Default | Description |
|---------|---------|-------------|
| `clipboard_clear_seconds` | 15 | Seconds before clipboard is automatically cleared (0: disable) |
| `auto_lock_minutes` | 5 | Minutes of inactivity before the database locks (0: disable) |
| `password_length` | 20 | Length of generated passwords (minimum 8) |
| `show_password_strength` | true | Show password strength indicator in detail and editor views |
| `password_uppercase` | true | Include uppercase letters (A-Z) in generated passwords |
| `password_lowercase` | true | Include lowercase letters (a-z) in generated passwords |
| `password_digits` | true | Include digits (0-9) in generated passwords |
| `password_symbols` | true | Include symbols (!@#$%^&*()-_=+) in generated passwords |
| `default_preset` | `"Strong"` | Default password preset: `PIN`, `Alphanumeric`, `Strong`, `Passphrase`, `Custom` |
| `theme` | `"dark"` | Color theme: `dark`, `light`, or `high-contrast` |
| `confirm_quit` | true | Confirm before quitting, even without unsaved changes (`false`: disable) |
| `backup_on_save` | false | Create a `.kdbx.bak` backup copy before each save |

Pressing `q` opens a confirmation with **No** selected. Press `y` to quit,
or `n`/`Esc` to return. To disable it, press `c` in the browser and set
**Confirm on quit** to Off with `Space`, or set `"confirm_quit": false` in the config.
Existing explicit `false` settings remain respected; unsaved changes always
require confirmation regardless of this setting.

Use `keetui -config` to view current settings as JSON (including all keybindings). The output is valid JSON that can be used as a config file:

```sh
keetui -config > ~/.config/keetui/config.json
```

Settings can also be edited from the TUI (press `c` in the browser).
Use `Space` to toggle booleans or open the preset/theme picker, and `Ctrl+S`
to save. Numeric errors appear beside the field as you type. Choose
**Keybindings** to record a shortcut by pressing it; `Esc` cancels recording,
and `Backspace` restores that action's default. Conflicting or reserved keys
are rejected. Changes remain a draft until you save.

### Keybindings

Keybindings can be customized by adding a `keybindings` section to the config file. Only specify the keys you want to change; omitted keys use defaults:

```json
{
  "keybindings": {
    "delete": "D",
    "show_qr": "Q",
    "quit": "q"
  }
}
```

Run `keetui -config` to see all available keybinding names and their current values.

## Development

Enable the pre-commit hook to catch formatting issues before committing:

```sh
git config core.hooksPath .githooks
```

## Security

- **Clipboard auto-clear**: Copied passwords and TOTP codes are automatically cleared from the clipboard (configurable, default 15s)
- **Auto-lock**: The database locks after inactivity (configurable, default 5 minutes), requiring the master password to be re-entered
- **Password generator**: Generates random passwords via `crypto/rand` with configurable character sets (uppercase, lowercase, digits, symbols)
- **Backup on save**: When enabled, the original `.kdbx` file is copied to `.kdbx.bak` before each save, providing a one-save-ago fallback (disabled by default; enable via `backup_on_save` setting)
- **Read-only mode**: Open with `--read-only` to browse a database without risk of accidental modification
- **Input validation**: Attachment extraction rejects path traversal attempts; config values are sanitized on load; URL opening restricted to http/https schemes
- **Display sanitization**: Values containing control characters or invalid UTF-8 are rendered with `�` placeholders so they cannot corrupt the terminal; clipboard copies always use the raw value

## Compatibility

### KeeWeb / kdbxweb compatibility

KeeWeb (via its kdbxweb library) can store file attachments in **KDBX 3.1** files as *protected binaries* in the Meta section, encrypted with the same inner stream cipher as passwords. Before `gokeepasslib` v3.7.0, these binaries were skipped during decryption, making subsequent protected values appear as random garbage. Upstream v3.7.0 includes the protected-binary decoding fix and now reports XML parsing errors instead of silently dropping entries.

keetui still needs the [rmarchei/gokeepasslib](https://github.com/rmarchei/gokeepasslib) fork (see `replace` in `go.mod`) for two compatibility fixes on top of v3.7.0:

- Accept empty timestamps written by some KeeWeb files, including metadata and entry history.
- Decrypt protected values **before** removing unused attachments during save. Removing a protected binary too early shifts the stream and corrupts the remaining passwords.

Regression tests use synthetic kdbxweb databases to check passwords, history, protected custom fields and attachments across saving, deleting entries and converting to KDBX 4. The fork should only be removed once both remaining fixes are available upstream and these tests pass. These fixes prevent corruption; they cannot recover passwords that have already been saved incorrectly.

### Corruption guard

If decoded protected values look like binary garbage (e.g. the database was written by a tool with an incompatible inner-stream implementation and opened with an unpatched build), keetui shows a `[corrupted?]` indicator in the status bar and **forces read-only mode** so the corrupted values can never be written back to disk. Headless `-import` / `-merge` / `--upgrade-kdbx4` operations refuse to run in that state. If you see this indicator: do not save the file with any tool that displays garbled passwords — open it with a compatible tool (e.g. KeePassXC or an up-to-date keetui) instead.

## Clipboard

Clipboard support uses platform-native commands:

- **macOS**: `pbcopy`
- **Linux (X11)**: `xclip` or `xsel`
- **Linux (Wayland)**: `wl-copy`

## License

See [LICENSE](LICENSE) for details.
