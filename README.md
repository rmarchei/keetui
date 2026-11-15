# keetui

A terminal user interface (TUI) for reading and writing KeePass `.kdbx` database files, built with Go.

## Features

- Unlock `.kdbx` databases with password or password + key file
- Create new `.kdbx` databases from scratch
- Browse groups and entries with vim-style navigation
- Search entries across all groups with literal substrings or optional fuzzy matching (title, username, URL, notes, custom fields)
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
- Password generator with preset picker (PIN, Alphanumeric, Strong, Passphrase, Like current, Custom); PIN length and passphrase words and separator are configurable
- Offline, pattern-aware password strength estimates and optional HIBP breach checks
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
- Never overwrites changes made elsewhere -- when another program or a sync changed the file since keetui read it, saving offers to merge them in first
- Auto-lock after 5 minutes of inactivity, or lock at once with `Ctrl+L`
- Color themes -- dark, light, and high-contrast presets, or auto (follows the terminal background), via config or settings UI
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

CI, release builds, and container-based Make targets use **Go 1.27.4**.
macOS binaries built with this toolchain require **macOS 13 Ventura or newer**.
The module retains Go 1.26.0 as its minimum source version. Make targets use
the installed Go toolchain by default; an installed container runtime is used
only when explicitly selected with `CONTAINER_RUNTIME`.

```sh
make                  # build for current OS/arch using local Go
make test             # run tests locally with the race detector
make lint             # local gofmt check + golangci-lint (must be installed)
make check            # run local lint + test
make all              # cross-compile all platforms using local Go
make linux-amd64      # cross-compile a single target using local Go
make install          # install to /usr/local/bin
make clean            # remove built binaries
```

Binaries are written to `dist/`. Override variables as needed:

```sh
VERSION=v1.0.0 make
make CONTAINER_RUNTIME=podman     # opt into a running Podman VM
make all CONTAINER_RUNTIME=docker # cross-compile using a running Docker daemon
make CONTAINER_RUNTIME=           # force local Go if the variable is set in your environment
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

The shortcuts below show the defaults. Browser and entry actions can be
customized via the config file (see [Keybindings](#keybindings)); forms keep
their editing controls.

Screens adapt to the terminal size. Settings and entry forms scroll to keep the
focused field visible; use `Tab` / `Shift+Tab` to move between fields. Shortcut
hints use up to two rows, showing essential actions when space is limited.
Press `F1` in any unlocked screen for context-specific help (and the full current
status message); its header shows keetui's version, as does the unlock screen's
bottom right (`keetui -version` prints it too). `Tab` / left / right switches help sections; wider terminals
show shortcuts in two columns. `Esc` / `F1` returns without losing form edits. `?` also opens help in
the browser and entry detail, but remains ordinary text inside inputs.
Use `PageUp` / `PageDown` to read long unlock errors, and `j` / `k` or
`PageUp` / `PageDown` to scroll database statistics. `Esc` or `Ctrl+C` quits
from the unlock screen; in settings opened from it, `Esc` returns to it first.
The unlock screen warns when the database or key file is missing, for example
on a volume unmounted while keetui was locked, and looks again every 2 seconds,
so the warning goes once the volume is mounted. keetui refuses to start on a
missing file or a folder.

### Browser

| Key | Action |
|-----|--------|
| `j` / `k` | Navigate up/down |
| `Enter` / `l` | Open group or view entry |
| `Esc` / `h` | Go back |
| `/` | Search entries (literal substring, all fields) |
| `Tab` while searching | Toggle literal / fuzzy matching |
| `S` | Cycle sort mode (name/modified/created) |
| `t` | Filter by tag (cycle through tags) |
| `n` | New entry |
| `g` | New group |
| `r` | Rename group (when cursor is on a group) |
| `y` | Duplicate entry |
| `f` | Toggle favorite on entry |
| `x` | Cut entry or group (for move) |
| `v` | Paste the cut item into the selected group, or into the current group when an entry is selected (never into the recycle bin) |
| `V` | Paste the cut item into the current group |
| `d` | Delete entry/group |
| `i` | Database stats |
| `I` | Password audit |
| `c` / `Ctrl+O` | Settings (`Ctrl+O` also on the unlock screen) |
| `P` | Change master password |
| `Ctrl+Z` | Undo last action |
| `s` / `Ctrl+S` | Save database (edit path, select format) |
| `Ctrl+L` | Lock (also in entry details, history, stats and audit) |
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
| `H` | History: earlier versions of the entry (`Enter` views one, `R` restores it) |
| `1` | Copy username to clipboard |
| `2` | Copy password to clipboard |
| `3` | Copy URL to clipboard |
| `4` | Copy TOTP code to clipboard |
| `b` | Check password against HIBP breaches |
| `Q` | Display selected field as QR code |
| `?` | Keyboard shortcut help |
| `Esc` | Back to browser |

Copy shortcuts are `1`–`4`; `Enter` copies the selected field, named in the
footer. Breach checking is a separate action on `b`. Existing explicit
`hibp_check` overrides (including `5`) remain respected.

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

Press `/` in the browser to search across all entries in the database. Each
new search starts in **literal** mode: `test` matches consecutive `test`
characters, ignoring case, in the title, username, URL, notes, or custom fields.

Press `Tab` while typing to toggle **fuzzy** mode. It also accepts the query's
characters in order with gaps between them: `gth` matches `GitHub`. Exact
substring matches appear first, followed by fuzzy matches. Press `Tab` again to
return to literal matching without changing the query.

The search row shows the mode, query, and result count. While you type, the
arrow keys (or the mouse wheel) move through the results; `Enter` opens the
selected entry. Going back preserves the results and search mode. `Esc`
cancels the search.

Matching characters in titles and usernames are bold and underlined. Results
matched through another field show a label such as “matches notes” or “matches
protected field”; field contents remain hidden. The position line also names
the selected result's matching field on narrow terminals.

## TOTP Support

keetui reads TOTP secrets from entries using two common conventions:

- **KeePassXC**: `otp` field containing an `otpauth://` URI
- **KeePass2**: `TimeOtp-Secret-Base32` field containing a raw base32 secret

When viewing an entry with a TOTP secret, a live 6-digit code is displayed with a visual countdown bar. The code refreshes automatically every second.

## Configuration

### Password strength

The meter uses [zxcvbn](https://github.com/trustelem/zxcvbn) to estimate how
many guesses a password might take, accounting for common words, substitutions,
dates, keyboard walks, and repeats. `~28 guess bits` means roughly 2^28 estimated
guesses, **not** 28 bits of proven entropy. Labels use estimated guess counts:
Weak below 10^6, Fair below 10^8, Good below 10^12, Strong below 10^16, then
Excellent (boundaries around 20, 27, 40 and 53 guess bits). These are keetui's
rating bands, not zxcvbn's online-attack score. The audit flags Weak passwords;
a higher rating does not guarantee resistance to offline cracking.
Dictionary coverage is mainly English; this is guidance, not a security guarantee.

Scoring happens entirely offline. HIBP remains a separate, explicitly requested
network check. To bound computation, passwords longer than 100 characters are
rated on their first 100 characters and the meter labels that limitation;
storage and copying always use the complete password. Statistics and audits
analyze a snapshot in the background; leaving the view cancels the scan.

### Settings file

Settings are stored in `~/.config/keetui/config.json`:

```json
{
  "clipboard_clear_seconds": 15,
  "clipboard_osc52": "auto",
  "auto_lock_minutes": 5,
  "password_length": 20,
  "show_password_strength": true,
  "password_uppercase": true,
  "password_lowercase": true,
  "password_digits": true,
  "password_symbols": true,
  "default_preset": "Strong",
  "theme": "auto",
  "confirm_quit": true,
  "backup_on_save": false
}
```

| Setting | Default | Description |
|---------|---------|-------------|
| `clipboard_clear_seconds` | 15 | Seconds before clipboard is automatically cleared (0: disable) |
| `clipboard_osc52` | `"auto"` | Copy through the terminal (OSC 52): `auto` (only without a local clipboard, e.g. over SSH), `always`, or `never`. See [Clipboard](#clipboard) |
| `auto_lock_minutes` | 5 | Minutes of inactivity before the database locks (0: disable) |
| `privacy_mode` | false | Keep passwords hidden while editing: no brief reveal when typing, generated passwords masked in the editor and the generator's preview (`p` or `Ctrl+R` shows them) |
| `password_length` | 20 | Length of generated passwords (minimum 8) |
| `show_password_strength` | true | Show password strength indicator in detail and editor views |
| `password_uppercase` | true | Include uppercase letters (A-Z) in generated passwords |
| `password_lowercase` | true | Include lowercase letters (a-z) in generated passwords |
| `password_digits` | true | Include digits (0-9) in generated passwords |
| `password_symbols` | true | Include symbols (!@#$%^&*()-_=+) in generated passwords |
| `default_preset` | `"Strong"` | Default password preset: `PIN`, `Alphanumeric`, `Strong`, `Passphrase`, `Custom` |
| `pin_length` | 6 | Digits in a generated PIN (4 to 16) |
| `passphrase_words` | 8 | Words in a generated passphrase (4 to 12; about 10 bits each, so 8 give about 80) |
| `passphrase_separator` | `"-"` | What separates passphrase words: 1 to 3 visible characters, no spaces |
| `theme` | `"auto"` | Color theme: `auto` (light or dark, following the terminal background; dark if the terminal does not say), `dark`, `light`, `high-contrast`, or `terminal` (the terminal's own palette). A theme chosen by name paints its own background when the terminal reports one of the other tone, such as `light` on a dark terminal |
| `colors` | (none) | Override theme colors, per theme: see [Colors](#colors) |
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

Settings can also be edited from the TUI: press `c` in the browser, or
`Ctrl+O` there or on the unlock screen, before any database is open. Opened
from the unlock screen, settings cover the global options only (a database's
own settings need it unlocked), the password typed so far is cleared, and
leaving or saving returns to the unlock screen in the theme just chosen.
Use `Space` or `Enter` to toggle booleans or open a choice picker. On numeric
fields, `Enter` moves to the next setting; `Tab` and `Shift+Tab` always navigate.
Only `Ctrl+S` saves settings. Numeric errors appear beside the field as you type. Choose
**Keybindings** to record a shortcut by pressing it; `Esc` cancels recording,
and `Backspace` restores that action's default. Conflicting or reserved keys
are rejected. While recording, navigation keys such as `Tab` and function keys
such as `F2` can be assigned to browser/detail actions; outside recording,
settings keeps its own navigation controls. Changes remain a draft until you save.

### Colors

The `terminal` theme uses the terminal's own palette (its 16 ANSI colors and
default text color), so keetui takes on whatever theme the terminal has, such
as Solarized, Catppuccin or Gruvbox. It never paints its own background.

Any theme's colors can be overridden in the config file under `colors`, keyed
by theme name and then by role:

```json
{
  "theme": "auto",
  "colors": {
    "dark": { "primary": "#89B4FA", "selection_bg": "#313244" },
    "light": { "primary": "#1E66F5" },
    "terminal": { "muted": "244" }
  }
}
```

`auto` uses the overrides of `dark` or `light`, whichever it picks. A color
is `#RGB` or `#RRGGBB`, an ANSI color number from `0` to `255` (`0`-`15` are
the terminal's palette), or `""` for the terminal's default color. The roles:

| Role | Used for |
|------|----------|
| `primary` | Accent: key hints, focus, group icons, the `keetui` chip, section headings |
| `label` | Field labels and secondary columns such as usernames |
| `muted` | Hints, hosts, timestamps, and the screen behind a dialog |
| `text` | Values and entry titles |
| `success`, `error`, `warn` | Messages, meters and markers |
| `title_fg` | Text on the `keetui` chip |
| `status_bg`, `status_fg` | The header and status bars |
| `selection_bg`, `selection_fg` | The selected row |
| `canvas` | The background painted when the theme's tone differs from the terminal's (`""`: never) |

Unknown roles and values that are not colors are ignored, with a warning when
keetui starts. Colors are edited in the config file only; saving settings from
the TUI keeps them.

### Per-database settings

A database can carry its own settings, which apply over the global ones
while it is open: choose **This database** at the end of the settings.
They are stored inside the `.kdbx` file (in its custom data, as KeePassXC
stores its own per-database options), so they are encrypted and
authenticated with the database: nobody can change them without the master
password, they travel with the vault to every machine, and other KeePass
clients keep them intact. Locking the database drops them.

These settings can be set per database: clipboard clear time, terminal
clipboard, auto-lock, password length and character sets, default preset,
confirm on quit, and backup on save. The theme, the strength display and
keybindings stay global. In the editor an empty setting inherits the global
one, shown beside it. `Space`, `Enter`, `←` and `→` cycle a choice or toggle;
`Backspace` or `Delete` resets that control to inherit. For numbers and text,
erase the value to inherit; Backspace and Delete keep their normal editing
behavior. `Ctrl+S` saves the global settings to the config file
and the database's into the database, which then counts as modified: save the
database to keep them. The global settings note the ones the database
overrides. A read-only database shows its settings without changing them.
Setting a default preset in the generator changes it where it is set: in the
database when the database sets one, else globally.

Batch operations (`--import-csv`, `--merge`, `--upgrade-kdbx4`) honor the
database's backup on save. Settings stored as JSON under the custom data key
`KEETUI_SETTINGS` use the config file's keys; unknown keys are ignored and
invalid values repaired as in the config file.

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

Navigation, quit and lock bindings also apply in entry details, history, attachments,
statistics, audit results, and Help (where navigation scrolls). Back applies to
the browsing screens; Help uses `Esc` or `F1` to close. Attachments use the
configured save and delete keys. Conflicts are checked across these screens.
Text inputs keep printable characters for typing; use `Ctrl+C` to request quit,
including while entering an attachment path or viewing a QR code.

## Development

Enable the pre-commit hook to catch formatting issues before committing:

```sh
git config core.hooksPath .githooks
```

## Security

To report a vulnerability, see [SECURITY.md](SECURITY.md).

- **Clipboard auto-clear**: Copied passwords and TOTP codes are automatically cleared from the clipboard (configurable, default 15s), and marked as sensitive where the platform supports it (see [Clipboard](#clipboard))
- **Auto-lock**: The database locks after inactivity (configurable, default 5 minutes), requiring the master password to be re-entered
- **History**: `H` in an entry lists its earlier versions; `Enter` shows one with what differs from the current entry (protected values masked until `p`), and `R` restores it after asking. The current state goes to the history first, the version brings back its fields, tags, attachments and expiry, and `Ctrl+Z` undoes the restore
- **Privacy mode**: With `privacy_mode` on, passwords stay hidden while you edit: typing in a protected field does not reveal it briefly, and generated passwords stay masked in the editor and the generator's preview until you press `p` or `Ctrl+R`
- **Lock now**: `Ctrl+L` (keybinding `lock`) locks the database at once and clears keetui's copy from the clipboard, on the browsing screens and over their help. With unsaved changes it first asks to save them: Yes saves, then locks (a failed save leaves the database open); No keeps it open. A save already running finishes first. Forms (the editor, group names, the master password, save as), search, and attachment path inputs keep their input: close them first. Database Undo (`Ctrl+Z`) also waits until you leave text input
- **Live security status**: The status bar shows `clip:15s` until clipboard clearing and `lock:4:59` until auto-lock. The lock countdown is highlighted in its final 30 seconds; normal input resets inactivity. Copying again restarts the clipboard timer, even for the same value. `off` means a protection is disabled, or that nothing can clear the copy (some tmux setups, see [Clipboard](#clipboard)), and `clip:wait` that a clear waits for keetui's tmux pane to be visible to the terminal that got the copy (`C:`/`L:` are compact labels on very narrow terminals). Clearing over OSC 52 cannot be verified; a local clipboard that has changed since the copy is left untouched.
- **Password generator**: Generates random passwords via `crypto/rand` with configurable character sets (uppercase, lowercase, digits, symbols)
- **Changes made elsewhere**: keetui remembers what it last read from or wrote to the database file. If another program (KeePassXC, a phone app syncing through Dropbox or Syncthing, a second keetui) changed the file since, saving asks first: **Yes** merges those changes into yours, then saves; **No** asks whether to overwrite them with your version; `Esc` cancels and keeps your changes unsaved. The merge matches entries by UUID: entries added there come in, an entry changed on both sides keeps the more recent version and puts the other in its history, and entries moved there (to the recycle bin, say) move here too. Entries you deleted stay deleted, unless they were changed there afterwards. Entries deleted there go here too when the file records their deletion (KeePass clients, keetui included, record what they delete for good) and you have not changed them since; entries merely missing from the file are kept, since it could be an older copy (a sync conflict, a restored backup). When the other program upgraded the file's format (KeePassXC turns KDBX 3.1 into 4 when it saves), keetui saves in that format too. Custom icons come along (a favicon downloaded in KeePassXC), and so do database settings: the name, description and default user name when they changed there more recently, and each application's per-database settings (keetui's own, KeePassXC's browser pairings) one by one, so a pairing added or revoked there stays that way. A master password changed elsewhere keeps keetui from reading the file to merge it: then only overwriting (which puts the file back under your master password) or cancelling remain. A file that is gone is written again if you say so; a folder that is gone (a volume unmounted) stops the save. Auto-lock cannot ask, so it merges and saves. The undo history is cleared after a merge. Command-line imports, merges and upgrades refuse to save over a file that changed while they ran
- **Backup on save**: When enabled, the original `.kdbx` file is copied to `.kdbx.bak` before each save, providing a one-save-ago fallback (disabled by default; enable via `backup_on_save` setting)
- **Read-only mode**: Open with `--read-only` to browse a database without risk of accidental modification
- **Input validation**: Attachment extraction rejects path traversal attempts; config values are sanitized on load; URL opening restricted to http/https schemes
- **Display sanitization**: Values containing control characters or invalid UTF-8 are rendered with `�` placeholders so they cannot corrupt the terminal; clipboard copies always use the raw value

## Compatibility

### KeeWeb / kdbxweb compatibility

KeeWeb (via its kdbxweb library) can store file attachments in **KDBX 3.1** files as *protected binaries* in the Meta section, encrypted with the same inner stream cipher as passwords. Before `gokeepasslib` v3.7.0, these binaries were skipped during decryption, making subsequent protected values appear as random garbage. Upstream v3.7.0 includes the protected-binary decoding fix and now reports XML parsing errors instead of silently dropping entries. Upstream v3.7.1 also decrypts protected values in document order, so files whose groups interleave entries and subgroups no longer have their later passwords corrupted.

keetui still needs the [rmarchei/gokeepasslib](https://github.com/rmarchei/gokeepasslib) fork (see `replace` in `go.mod`) for these fixes on top of v3.7.1:

- Accept empty timestamps written by some KeeWeb files, including metadata and entry history.
- Decrypt protected values **before** removing unused attachments during save. Removing a protected binary too early shifts the stream and corrupts the remaining passwords.
- Write complete KDBX 3.1 attachments. The last one or two bytes of their compressed data were dropped, so KeeWeb refused to open a file with an attachment added by keetui. Saving such a file with this version repairs it.
- Return KDBX 4 attachments exactly as stored. An attachment whose content happened to be valid base64, such as a key file, was decoded when extracted, copied or merged.

Regression tests use synthetic kdbxweb databases to check passwords, history, protected custom fields and attachments across saving, deleting entries and converting to KDBX 4. The fork should only be removed once all of these fixes are available upstream and these tests pass. These fixes prevent corruption; they cannot recover passwords that have already been saved incorrectly.

### Corruption guard

If decoded protected values look like binary garbage (e.g. the database was written by a tool with an incompatible inner-stream implementation and opened with an unpatched build), keetui shows a `[corrupted?]` indicator in the status bar and **forces read-only mode** so the corrupted values can never be written back to disk. Headless `-import` / `-merge` / `--upgrade-kdbx4` operations refuse to run in that state. If you see this indicator: do not save the file with any tool that displays garbled passwords — open it with a compatible tool (e.g. KeePassXC or an up-to-date keetui) instead.

## Clipboard

keetui copies to the local clipboard and marks copies as sensitive where the
platform allows, so clipboard history and managers can skip them:

- **macOS**: keetui writes the pasteboard directly. Copies carry the
  `org.nspasteboard.ConcealedType` marker (plus `TransientType` while
  auto-clear is on), which tells macOS to keep them out of clipboard history
  and is honored by managers such as Maccy, Alfred, Paste and Raycast. They
  also stay on this Mac (no Universal Clipboard). `/usr/bin/pbcopy` is a
  fallback that cannot mark copies.
- **Linux (Wayland)**: `wl-copy`. With wl-clipboard 2.3.0 or later, copies
  carry `x-kde-passwordManagerHint=secret`, honored by Klipper, CopyQ 8+,
  GPaste 51.1+, cliphist and clipman.
- **Linux (X11)**: keetui owns the clipboard itself, with no external tool,
  and copies carry `x-kde-passwordManagerHint=secret`. CopyQ, Klipper and
  clipcat skip them; GPaste 51.1 keeps them only as a masked password item
  that it does not save. GPaste 51.0 and older, ClipIt, Parcellite, Diodon,
  xfce4-clipman, qlipper, greenclip and clipmenu ignore the marker and store
  every copy. `xclip`, then `xsel`, are fallbacks that cannot mark copies:
  keetui uses them when it cannot reach or authenticate to the X server
  itself (see below), or for values larger than one X request (about
  256 KiB).

When a copy cannot be marked, the status bar says that clipboard managers may
keep it.

**X11 details.** clipmenu and greenclip can skip keetui by the name of its
clipboard window, `keetui`: set `CM_IGNORE_WINDOW='^keetui$'` for clipmenu, or
`blacklisted_applications = ["keetui"]` in greenclip's configuration. When
keetui clears a copy, it keeps the clipboard but empties it, because ClipIt,
Parcellite, Diodon, xfce4-clipman, qlipper and GPaste 45 would otherwise put
their newest entry, possibly the password, straight back. At exit keetui hands
an empty copy to `xclip` (except on a display forwarded over SSH, see Over
SSH below), so install xclip: without it the clipboard is left
without an owner, and Diodon and Parcellite then restore the password from
their history. keetui starts xclip in a session of its own, so the copy
outlives keetui even when keetui leads its terminal's session
(`xterm -e keetui`, `tmux new-window keetui`, `ssh -t host keetui`), whose
exit hangs up the processes on that terminal. If keetui is killed with
`kill -9` or crashes, Diodon and Parcellite restore the password, even after
the clear timer has emptied the clipboard. Fallback copies made with `xsel`
are cleared with `xsel --clear` only when keetui cannot take an empty copy
itself, with the same risk.

If the X server stops answering, keetui still wipes its copy at once and keeps
the clipboard, which is empty once the server answers again. At exit keetui
waits about 5 seconds at most for the server, then leaves the empty copy to
xclip, which takes the clipboard once the server answers. If the server does
not answer keetui's connection, the first copy waits 2 seconds for it, then
goes to the terminal with OSC 52 (unless `clipboard_osc52` is `never`)
without trying xclip or xsel, which would wait for the same server; later
copies do not wait for it again until it answers. A copy that falls back to
xclip or xsel for another reason waits at most 2 seconds for them: keetui then
stops the tool, so that the copy cannot reach the clipboard later without
keetui knowing, and sends it with OSC 52 instead. An empty copy that clears
the clipboard is left to finish once the server answers. Reading the clipboard back to check a
fallback copy also waits at most 2 seconds; without an answer keetui clears
the copy anyway, and the status bar reports the timeout.

With `clipboard_osc52` set to `always`, keetui reads the clipboard back when
it clears, to recognize the terminal's copy of its value. Large text that the
clipboard's owner sends in parts (INCR) is read to the end, and none of it is
left behind. A transfer that takes more than 2 seconds counts as someone
else's copy and is left in place.

keetui finds the X server's cookie in `$XAUTHORITY` (or `~/.Xauthority`) as
libxcb does: for a local socket or `127.0.0.1`, the entry for this host's
name, as display managers and `ssh -X` write it; for any other address, the
entry for the server's IP address, as sshd writes it with
`X11UseLocalhost no` (`DISPLAY=hostname:10.0`); and entries for any host.

**Terminal clipboard (OSC 52).** Over SSH, or when no local clipboard is
available, keetui sends the copy to the terminal with OSC 52 instead. These
copies cannot be marked, session recorders (`script`, asciinema, `tmux
pipe-pane`) and raw terminal logs record them, and keetui cannot verify that
they were cleared.

`clipboard_osc52` controls this: `auto` (the default) uses OSC 52 only when
needed, `always` also sends local copies to the terminal, and `never` disables
it. With `auto`, a local tmux on macOS changes nothing while you are at the
Mac: copies still go to the pasteboard. With `always`, the status bar also
carries the terminal copy's warnings (several tmux clients, nested tmux,
unverified, or a tmux route that cannot clear its copy). The timed clear
empties the local clipboard, then sends the terminal clear once (never when
the local clipboard has changed since, and never a second time, which could
wipe something copied since); the status bar says when the terminal copy
could not be cleared. Status messages that fit take the place of the file
name and entry count in the status bar, so that a warning or remedy shows
whole; F1 shows every message in full.

**Over SSH.** The local clipboard is yours only while you are at this
machine. keetui checks for each copy, since a terminal multiplexer session
can be attached from anywhere at any time, and stops at the first answer:

1. In a herdr pane (`HERDR_ENV=1` or `TERM_PROGRAM=herdr`), the clients of
   keetui's herdr server: keetui looks at your processes named `herdr` (or
   like `$HERDR_BIN_PATH`, any version) that run as a client (`herdr`,
   `herdr client`, `session attach`, `terminal attach`, `agent attach`),
   never at the server, and leaves out clients of other herdr sessions (as
   herdr resolves them, from `--session`, `session attach`,
   `HERDR_SOCKET_PATH` or `HERDR_SESSION`). You are remote if any of them
   runs over SSH (`SSH_CONNECTION` or `SSH_TTY` in its environment), or if
   `herdr --remote` is attached (`remote-client-bridge`). With clients at
   both ends, a local client whose terminal had input in the last 9 seconds
   typed the copy's keypress: herdr would hand the copy to it, and it would
   write this machine's clipboard unmarked, so keetui copies here, marked,
   instead. Linux records terminal input only every 8 seconds, so typing at
   both ends at once can go either way; F1 says which client decided.
2. In tmux, the session environment, which tmux refreshes on every attach,
   while panes keep the environment they started with. In a tmux started
   from a herdr pane, both count: you are remote if tmux was last attached
   over SSH or a herdr client runs over SSH.
3. An `sshd` (or `sshd-session`) among keetui's parent processes, which also
   covers `su -` and `sudo -i`.
4. `SSH_CONNECTION` or `SSH_TTY` in keetui's environment.

F1 shows what decided, and where the copy went. In a remote session:

- **macOS**: keetui does not touch the pasteboard; copies go to your terminal
  with OSC 52.
- **Linux**: keetui ignores `WAYLAND_DISPLAY`, which belongs to the remote
  host's compositor, and uses an X display only if sshd forwards it
  (`ssh -X` or `-Y`: `localhost:10.0` and up). Inside tmux the session's
  `DISPLAY` counts, not the pane's, and so does the session's `XAUTHORITY`
  (`~/.Xauthority` when the attaching client had none): a tmux started on a
  desktop keeps the display manager's cookie file in its panes. Any other
  display (`:0` of the remote desktop, a VNC server's `:1`) belongs to the
  remote host, and keetui leaves it alone. A forwarded display gets the
  marked copy and, with `auto`, a terminal copy as well, because the X server
  at your end may be XQuartz or an untrusted `ssh -X` connection whose copies
  cannot be pasted; the timed clear clears both. A terminal that applies
  OSC 52 to the same clipboard (`ssh -X` from a Linux desktop with kitty,
  foot, Alacritty, WezTerm or Ghostty) then replaces the marked copy with an
  unmarked one, which clipboard history keeps: set `clipboard_osc52` to
  `never` there. On a forwarded display keetui never falls back to xclip
  or xsel and hands nothing to xclip at exit: what they leave behind would
  hold the SSH session's X11 channel, so `ssh -t host keetui` would not exit
  after keetui quits. The trade-off: once keetui exits, clipboard managers on
  your desktop that ignore the marker may restore their newest entry. When
  the session's display changes (a tmux used at the desk, then attached over
  `ssh -X`), keetui empties its copy on the old display but keeps that
  clipboard until it exits, so that managers there have no ownerless
  clipboard to refill. Holding a forwarded display also holds its X11
  channel: after a copy in a tmux attached with `ssh -X`, ssh does not exit
  when you detach until keetui quits (or you type `~.`). If the forwarded X
  server is gone, the ssh client
  prints its error over the screen, so keetui redraws the whole screen after
  a failed copy and after each clear of a copy on that display. A copy on a
  forwarded X server that went away before the clear (XQuartz quit) went with
  it: keetui does not ask that server again, and still clears the terminal
  copy.

**herdr.** In a herdr pane keetui sends a plain OSC 52, which herdr hands to
the client that last typed: a client running over SSH writes it to its
terminal, while a local client writes this machine's clipboard itself, with
plain `wl-copy`, `xclip` or `pbcopy`, unmarked, so clipboard history keeps
it. So keetui copies locally whenever it can. A herdr pane keeps the
environment its server started with, which names no display, or an old
one, when the server was started over SSH (by `herdr --remote` attaching,
say) or from another desktop session; for a local copy on Linux keetui then
uses the display of the herdr client you typed on (its `WAYLAND_DISPLAY`,
or its `DISPLAY` and `XAUTHORITY`), so the copy stays marked instead of
going to herdr, and F1 names that client. herdr ignores an empty OSC 52,
so keetui clears a terminal copy by copying a single space over it, and only while a herdr
client runs over SSH at that moment; otherwise it sends nothing, since the
space would land on this machine's clipboard, and the status bar says the
copy was not cleared. Like any OSC 52 clear, the space also replaces
whatever was copied to that terminal's clipboard since. In a tmux started
from a herdr pane, tmux hands keetui's passthrough on to herdr, so its clear
copies a space too while a herdr client runs over SSH. With
`clipboard_osc52` set to `always` and only local herdr clients, the terminal
copy goes to a local client, which writes this machine's clipboard,
unmarked (on macOS herdr skips the write when the clipboard already holds
the same text); the local clear covers it, and keetui sends herdr nothing
more.

**tmux.** Inside tmux, keetui sends no plain OSC 52 (unless you ask for one
for a tmux it cannot query, see below): with the default
`set-clipboard external` tmux drops it, and with `on` it keeps it in a paste
buffer that any pane can read back. For each copy and each clear, keetui asks
tmux how to reach your terminal instead:

- **Passthrough** (`set -g allow-passthrough on`, tmux 3.3 or later; always
  allowed before): tmux forwards the copy unchanged without keeping it, and
  keetui can clear it. tmux sends it to every client that shows keetui's
  pane; with `all` (tmux 3.4 or later), to every client of a session that
  holds keetui's window, grouped and linked sessions included. It forwards
  nothing while keetui's pane is not visible: with `on`, while another
  window is current or another pane of keetui's window is zoomed, and with
  any setting while the pane is in copy mode (or another tmux mode,
  `display-panes` included from tmux 3.8) or the client is suspended
  (prefix C-z). A clear that falls due then waits (`clip:wait`) until the
  pane is visible again or the client resumes. `all` lets it through past
  another window or a zoomed pane, but not out of copy mode or to a
  suspended client. Before tmux 3.2 nothing names a suspended client:
  keetui notices a suspend only when it leaves no client awake, and only
  once tmux has recounted its clients, which the next tmux command makes it
  do, so a clear can be lost on a suspended terminal.

  With `on`, tmux also drops passthrough while it shows a command prompt, a
  message, a menu (before tmux 3.7), a popup (3.2) or `display-panes`
  (before 3.8) on the terminal. keetui cannot see these, so a clear that falls due then is lost
  although the status bar says it was requested. `all` lets copies and
  clears through all of them.
- **load-buffer** (tmux 3.2 or later): without passthrough, keetui passes the
  copy on stdin into a randomly named buffer, which tmux sends to your
  terminal and keetui deletes at once. tmux must know that your terminal
  supports OSC 52; if it does not, add
  `set -as terminal-features ',xterm-256color:clipboard'` with your outer
  `TERM`. tmux cannot clear these copies: the status bar says so and shows
  `clip:off`. keetui also uses this route when passthrough would reach other
  clients too (a shared session, or with `all` a grouped or linked one), so
  that the copy reaches only your terminal. Before tmux 3.2, or when tmux
  does not know that your terminal supports OSC 52, such a copy goes
  through passthrough to every one of them, and the status bar says how
  many. In a popup nothing tells keetui which client shows it, so it uses
  this route only while a single client is attached; tmux 3.3 to 3.6 ignore
  passthrough from popups.
- Otherwise nothing is copied, and the status bar says why.

`set-clipboard` needs no change in the tmux keetui runs in. In nested tmux
(for example keetui in a tmux reached over SSH from a pane of another tmux),
that tmux hands the copy on to the outer one as a plain OSC 52, which the
outer tmux forwards only with `set -g set-clipboard on`, keeping it in a
paste buffer. keetui can neither check nor clear it, so the status bar marks
the copy unverified and warns about the outer tmux.

A passthrough clear reaches whichever terminals show keetui's pane when it
is sent, not necessarily the ones that got the copy, so keetui remembers
which terminals tmux sent the copy to. While one of them is still attached
but out of reach (suspended, or its session shows another window; with
`all`, does not hold keetui's window), the clear waits (`clip:wait`)
instead of missing it. It goes out once tmux would deliver it to every
terminal that got the copy and is still attached, and also reaches any
other terminal showing keetui's pane at that moment. keetui sends each
clear only once: a second one could wipe something copied since. A copy
that went to several terminals is cleared the same way; if some of them
detached in the meantime, the clear goes to the others and the status bar
says that a terminal that got the copy detached. A new copy replaces one
whose clear is waiting, and its clear goes only to the terminals the new copy
reached: a terminal that got only the earlier copy keeps it.

If the terminal that got the copy has detached and another one is attached,
keetui sends the clear only when it would reach a single terminal that
reports the same `TERM` and terminal type (its XTVERSION reply) and was not
attached yet when keetui copied, as the same terminal does when it reconnects
over SSH on a new tty; the status bar then
says the clear went to the reattached terminal. Otherwise, and whenever all
the terminals a copy to several went to have detached, it does not send the
clear, which would only wipe other terminals' clipboards, and the status bar
says the copy was not cleared.

Over SSH from a tmux pane, keetui recognizes tmux from the terminal's reply
to XTVERSION (tmux 3.2 or later answers it), and sends copies through
passthrough, which leaves no paste buffer. keetui cannot ask that tmux
whether it allows passthrough, which tmux 3.3 and later drop by default, so
the status bar says the copy is unverified and that tmux needs
`allow-passthrough` (with tmux before 3.3, which always lets passthrough
through, it says only that the copy went through tmux).
Add `set -g allow-passthrough on` (or `all`) to that tmux's configuration,
on the machine you connect from; copies then arrive and are cleared on the
timer. Alternatively set `clipboard_osc52` to `always`: keetui then also
sends a plain OSC 52, which that tmux forwards with `set-clipboard on` but
also keeps in a paste buffer that keetui cannot clear, as the status bar
warns ("tmux may keep a copy"); the clear sends both empty copies. The same
goes for keetui in a tmux pane after `sudo` or `su`, which drop `$TMUX`, and
for a tmux that keetui cannot ask (no tmux on keetui's `PATH`). An older tmux at the other
end looks like a plain terminal and gets a plain OSC 52. The other way
round, when `$TMUX` is set but the terminal answers XTVERSION with another
name, keetui is not running directly in tmux: a terminal started from a tmux
pane inherits `$TMUX`. keetui then sends nothing to the terminal and says
so, because a terminal multiplexer running inside a tmux pane looks the same
and could pass a plain OSC 52 on to tmux; unset `TMUX` in such a terminal.

tmux hooks such as `after-load-buffer` and verbose logs (`tmux -vv`) still
see copies. F1 shows the full status message together with these settings.

**Clearing.** keetui clears only its own copy: if you have copied something
else since, the clipboard is left untouched. It clears on the timer, on lock,
on quit, and when the process is terminated (SIGTERM, SIGINT, or SIGHUP when
the terminal is closed). A terminal clear that waits for keetui's tmux pane,
or for the terminal that got the copy, keeps retrying after a lock and is
tried once more when keetui exits. After `kill -9`, a copy on macOS or
Wayland, an X11 fallback copy made with xclip or xsel, or a terminal copy
stays until something replaces it. keetui's own X11 copy ends with the
process instead, and clipboard managers that ignore the marker may then
restore it (see X11 details).

## License

keetui is released under the MIT License; see [LICENSE](LICENSE).

The passphrase word list (`internal/keepass/diceware.go`) draws in part on
the [EFF Short Wordlist #1](https://www.eff.org/dice) by the Electronic
Frontier Foundation, used under
[CC BY 3.0 US](https://creativecommons.org/licenses/by/3.0/us/).
