package x11clip

import (
	"bufio"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/jezek/xgb"
)

// X authority families (X11/Xauth.h).
const (
	familyInternet  = 0
	familyInternet6 = 6
	familyLocal     = 256
	familyWild      = 65535
)

const cookieName = "MIT-MAGIC-COOKIE-1"

// display is a parsed X display name, as xgb parses it.
type display struct {
	network, address string // what to dial
	host             string // the host part; "" for a local socket
	number, screen   int
}

func parseDisplay(name string) (display, error) {
	if name == "" {
		name = os.Getenv("DISPLAY")
	}
	if name == "" {
		return display{}, errors.New("empty display string")
	}
	bad := errors.New("bad display string: " + name)
	colon := strings.LastIndex(name, ":")
	if colon < 0 {
		return display{}, bad
	}
	var d display
	protocol, socket := "", ""
	if name[0] == '/' {
		socket = name[:colon] // a launchd socket path
	} else if slash := strings.LastIndex(name[:colon], "/"); slash >= 0 {
		protocol, d.host = name[:slash], name[slash+1:colon]
	} else {
		d.host = name[:colon]
	}
	number, screen, hasScreen := strings.Cut(name[colon+1:], ".")
	n, err := strconv.Atoi(number)
	if err != nil || n < 0 {
		return display{}, bad
	}
	d.number = n
	if hasScreen {
		if d.screen, err = strconv.Atoi(screen); err != nil {
			return display{}, bad
		}
	}
	switch {
	case socket != "":
		d.network, d.address, d.host = "unix", socket+":"+number, ""
	case d.host != "" && d.host != "unix":
		if protocol == "" {
			protocol = "tcp"
		}
		d.network, d.address = protocol, net.JoinHostPort(strings.Trim(d.host, "[]"), strconv.Itoa(6000+n))
	default:
		d.network, d.address, d.host = "unix", "/tmp/.X11-unix/X"+number, ""
	}
	return d, nil
}

// connect opens a connection to an X display and authenticates with the
// display's cookie from authority, or by default from $XAUTHORITY or
// ~/.Xauthority. It looks the cookie up as libxcb does, by the address of
// the server it reached: this host's name for a local socket or 127.0.0.1,
// otherwise the IP address, which sshd's xauth stores for a display such
// as hostname:10.0 (X11UseLocalhost no). Entries for this host's name
// written for a remote host name, which xgb matched, still count after
// those.
func connect(name, authority string) (*xgb.Conn, error) {
	d, err := parseDisplay(name)
	if err != nil {
		return nil, err
	}
	conn, err := net.Dial(d.network, d.address)
	if err != nil {
		return nil, fmt.Errorf("cannot connect to %s: %w", name, err)
	}
	keys := authKeys(d, conn.RemoteAddr())
	cookie, _ := findCookie(authorityFile(authority), d.number, keys)
	var c *xgb.Conn
	if cookie != nil {
		c, err = xgb.NewConnNetWithCookieHex(conn, hex.EncodeToString(cookie))
		clear(cookie)
	} else {
		c, err = xgb.NewConnNet(conn) // tries without a cookie
	}
	if err != nil {
		_ = conn.Close()
		return nil, err
	}
	c.DisplayNumber, c.DefaultScreen = d.number, d.screen
	return c, nil
}

// authorityFile is the X authority file to read: the given one, else
// $XAUTHORITY, else ~/.Xauthority.
func authorityFile(authority string) string {
	if authority != "" {
		return authority
	}
	if f := os.Getenv("XAUTHORITY"); f != "" {
		return f
	}
	if home := os.Getenv("HOME"); home != "" {
		return filepath.Join(home, ".Xauthority")
	}
	return ""
}

// authKey is an authority entry's family and address.
type authKey struct {
	family uint16
	addr   string
}

// authKeys lists the entries that may serve a connection, best first: the
// one libxcb looks up, the one xgb looks up (this host's name, or the host
// part of the display name), then this host's name and the server's IP
// address.
func authKeys(d display, remote net.Addr) []authKey {
	hostname, _ := os.Hostname()
	local := authKey{familyLocal, hostname}
	first, server := local, authKey{}
	if tcp, ok := remote.(*net.TCPAddr); ok && d.network != "unix" {
		if ip4 := tcp.IP.To4(); ip4 != nil {
			server = authKey{familyInternet, string(ip4)}
		} else {
			server = authKey{familyInternet6, string(tcp.IP.To16())}
		}
		// libxcb looks loopback up by this host's name.
		loopback := tcp.IP.Equal(net.IPv4(127, 0, 0, 1)) || tcp.IP.To4() == nil && tcp.IP.IsLoopback()
		if !loopback {
			first = server
		}
	}
	keys := []authKey{first}
	add := func(k authKey) {
		if k != (authKey{}) && !slices.Contains(keys, k) {
			keys = append(keys, k)
		}
	}
	if d.host != "" && d.host != "localhost" {
		add(authKey{familyLocal, d.host})
	}
	add(local)
	add(server)
	return keys
}

// findCookie returns the MIT-MAGIC-COOKIE-1 for display number from an
// authority file: the first entry for the best key, or for any host.
func findCookie(file string, number int, keys []authKey) ([]byte, error) {
	if file == "" {
		return nil, errors.New("no X authority file")
	}
	f, err := os.Open(file)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	entries, err := readAuthority(bufio.NewReader(f))
	defer func() {
		for _, e := range entries {
			clear(e.data)
		}
	}()
	want := strconv.Itoa(number)
	for _, key := range keys {
		for _, e := range entries {
			if e.name != cookieName || len(e.data) != 16 || e.number != "" && e.number != want {
				continue
			}
			if e.family == familyWild || e.family == key.family && e.addr == key.addr {
				return append([]byte(nil), e.data...), nil
			}
		}
	}
	if err != nil {
		return nil, err
	}
	return nil, errors.New("no X authority entry for the display")
}

type authEntry struct {
	family             uint16
	addr, number, name string
	data               []byte
}

// readAuthority reads the entries of an X authority file, up to the first
// malformed one.
func readAuthority(r io.Reader) ([]authEntry, error) {
	var entries []authEntry
	field := func() ([]byte, error) {
		var n uint16
		if err := binary.Read(r, binary.BigEndian, &n); err != nil {
			return nil, err
		}
		b := make([]byte, n)
		_, err := io.ReadFull(r, b)
		return b, err
	}
	for {
		var e authEntry
		if err := binary.Read(r, binary.BigEndian, &e.family); err != nil {
			if errors.Is(err, io.EOF) {
				return entries, nil
			}
			return entries, err
		}
		var parts [4][]byte
		for i := range parts {
			b, err := field()
			if err != nil {
				return entries, fmt.Errorf("malformed X authority file: %w", err)
			}
			parts[i] = b
		}
		e.addr, e.number, e.name, e.data = string(parts[0]), string(parts[1]), string(parts[2]), parts[3]
		entries = append(entries, e)
	}
}
