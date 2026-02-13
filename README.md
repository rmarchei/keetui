# keetui

A terminal user interface (TUI) for reading and writing KeePass `.kdbx` database files, built with Go.

## Features

- Unlock `.kdbx` databases with password or password + key file
- Browse groups and entries with vim-style navigation
- View entry details (username, password, URL, notes)
- TOTP/2FA support -- live code display with countdown, compatible with KeePassXC and KeePass2
- Copy username, password, URL, or TOTP code to clipboard
- Create, edit, and delete entries and groups
- Save changes back to the `.kdbx` file

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

## Usage

```sh
keetui database.kdbx
keetui -keyfile key.key database.kdbx
```

## Keyboard Shortcuts

### Browser

| Key | Action |
|-----|--------|
| `j` / `k` | Navigate up/down |
| `Enter` / `l` | Open group or view entry |
| `Esc` / `h` | Go back |
| `n` | New entry |
| `g` | New group |
| `d` | Delete entry/group |
| `s` | Save database to file |
| `q` | Quit |

### Entry Detail

| Key | Action |
|-----|--------|
| `p` | Toggle password visibility |
| `e` | Edit entry |
| `1` | Copy username to clipboard |
| `2` | Copy password to clipboard |
| `3` | Copy URL to clipboard |
| `4` | Copy TOTP code to clipboard |
| `Esc` | Back to browser |

### Editor

| Key | Action |
|-----|--------|
| `Tab` / `Shift+Tab` | Navigate fields |
| `Ctrl+S` | Save |
| `Esc` | Cancel |

## TOTP Support

keetui reads TOTP secrets from entries using two common conventions:

- **KeePassXC**: `otp` field containing an `otpauth://` URI
- **KeePass2**: `TimeOtp-Secret-Base32` field containing a raw base32 secret

When viewing an entry with a TOTP secret, a live 6-digit code is displayed with a visual countdown bar. The code refreshes automatically every second.

## Clipboard

Clipboard support uses platform-native commands:

- **macOS**: `pbcopy`
- **Linux (X11)**: `xclip` or `xsel`
- **Linux (Wayland)**: `wl-copy`

## License

See [LICENSE](LICENSE) for details.
