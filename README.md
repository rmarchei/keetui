# keetui

A terminal user interface (TUI) for reading and writing KeePass `.kdbx` database files, built with Go.

## Features

- Unlock `.kdbx` databases with password or password + key file
- Create new `.kdbx` databases from scratch
- Browse groups and entries with vim-style navigation
- Search entries across all groups by title or username
- Entries sorted alphabetically (groups first, then entries)
- View entry details (username, password, URL, notes, custom fields)
- Per-field password toggle -- reveal individual protected fields independently
- TOTP/2FA support -- live code display with countdown, compatible with KeePassXC and KeePass2
- Display any field as a scannable QR code (useful over SSH from a phone)
- Copy username, password, URL, or TOTP code to clipboard (auto-cleared after 15s)
- Mouse support -- click to select fields and entries, scroll wheel navigation
- Open entry URL in system browser
- Password generator for creating strong random passwords and diceware passphrases
- Password strength indicator and HIBP breach check
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
- Import/export (CSV, XML) and merge from other `.kdbx` files
- Change database master password
- Save changes back to the `.kdbx` file
- Auto-lock after 5 minutes of inactivity
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

```sh
make                  # build for current OS/arch (uses container if available)
make test             # run tests (uses container if available)
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
keetui -keyfile key.key database.kdbx   # open with key file
keetui -new database.kdbx              # create a new database
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
```

- **CSV** format includes columns: Group, Title, Username, Password, URL, Notes
- **XML** format preserves group hierarchy, custom fields, and attachments
- **Merge** matches entries by UUID; newer entries overwrite older ones; new groups are created as needed

## Keyboard Shortcuts

### Browser

| Key | Action |
|-----|--------|
| `j` / `k` | Navigate up/down |
| `Enter` / `l` | Open group or view entry |
| `Esc` / `h` | Go back |
| `/` | Search entries by title/username |
| `n` | New entry |
| `g` | New group |
| `r` | Rename group (when cursor is on a group) |
| `y` | Duplicate entry |
| `x` | Cut entry or group (for move) |
| `v` | Paste cut item into current group |
| `d` | Delete entry/group |
| `i` | Database stats |
| `I` | Password audit |
| `c` | Settings |
| `P` | Change master password |
| `s` / `Ctrl+S` | Save database to file |
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
| `Esc` | Back to browser |

### Editor

| Key | Action |
|-----|--------|
| `Tab` / `Shift+Tab` | Navigate fields |
| `Ctrl+G` | Generate random password |
| `Ctrl+P` | Generate diceware passphrase |
| `Ctrl+A` | Add custom field |
| `Ctrl+D` | Delete current custom field |
| `Ctrl+S` | Save |
| `Esc` | Cancel |

## Search

Press `/` in the browser to search across all entries in the database. Matches filter by title and username. Press `Enter` to confirm the search results or `Esc` to cancel.

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
  "show_password_strength": true
}
```

| Setting | Default | Description |
|---------|---------|-------------|
| `clipboard_clear_seconds` | 15 | Seconds before clipboard is automatically cleared (0: disable) |
| `auto_lock_minutes` | 5 | Minutes of inactivity before the database locks (0: disable) |
| `password_length` | 20 | Length of generated passwords (minimum 8) |
| `show_password_strength` | true | Show password strength indicator in detail and editor views |

Use `keetui -config` to view current settings. Settings can also be edited from the TUI (press `c` in the browser).

## Security

- **Clipboard auto-clear**: Copied passwords and TOTP codes are automatically cleared from the clipboard (configurable, default 15s)
- **Auto-lock**: The database locks after inactivity (configurable, default 5 minutes), requiring the master password to be re-entered
- **Password generator**: Generates random passwords using uppercase, lowercase, digits, and symbols via `crypto/rand`

## Clipboard

Clipboard support uses platform-native commands:

- **macOS**: `pbcopy`
- **Linux (X11)**: `xclip` or `xsel`
- **Linux (Wayland)**: `wl-copy`

## License

See [LICENSE](LICENSE) for details.
