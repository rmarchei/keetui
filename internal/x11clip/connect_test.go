package x11clip

import (
	"bytes"
	"encoding/binary"
	"io"
	"net"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"testing"
	"time"
)

func TestParseDisplay(t *testing.T) {
	for _, tc := range []struct {
		name string
		want display
	}{
		{":0", display{network: "unix", address: "/tmp/.X11-unix/X0"}},
		{":1.2", display{network: "unix", address: "/tmp/.X11-unix/X1", number: 1, screen: 2}},
		{"unix:3", display{network: "unix", address: "/tmp/.X11-unix/X3", number: 3}},
		{"localhost:10.0", display{network: "tcp", address: "localhost:6010", host: "localhost", number: 10}},
		{"host.example:11", display{network: "tcp", address: "host.example:6011", host: "host.example", number: 11}},
		{"tcp/host:12.1", display{network: "tcp", address: "host:6012", host: "host", number: 12, screen: 1}},
		{"/private/tmp/launch-x/org.xquartz:0", display{network: "unix", address: "/private/tmp/launch-x/org.xquartz:0"}},
	} {
		got, err := parseDisplay(tc.name)
		if err != nil || got != tc.want {
			t.Errorf("parseDisplay(%q) = %+v, %v; want %+v", tc.name, got, err, tc.want)
		}
	}
	for _, bad := range []string{"localhost", "host:", "host:x", ":-1", ":1.x", ":1.2.3"} {
		if _, err := parseDisplay(bad); err == nil {
			t.Errorf("parseDisplay(%q) succeeded", bad)
		}
	}
}

func TestAuthKeys(t *testing.T) {
	hostname, _ := os.Hostname()
	local := authKey{familyLocal, hostname}
	ip := func(s string) string { return string(net.ParseIP(s).To4()) }
	ip6 := func(s string) string { return string(net.ParseIP(s).To16()) }
	tcp := func(s string) net.Addr { return &net.TCPAddr{IP: net.ParseIP(s), Port: 6010} }
	for _, tc := range []struct {
		display string
		remote  net.Addr
		want    []authKey
	}{
		{":0", &net.UnixAddr{Name: "/tmp/.X11-unix/X0", Net: "unix"}, []authKey{local}},
		// ssh -X: sshd's listener on loopback, cookie stored for this host.
		{"localhost:10.0", tcp("127.0.0.1"), []authKey{local, {familyInternet, ip("127.0.0.1")}}},
		{"localhost:10.0", tcp("::1"), []authKey{local, {familyInternet6, ip6("::1")}}},
		// X11UseLocalhost no: xauth stores the cookie for the address.
		{"remotehost:10.0", tcp("192.0.2.5"), []authKey{{familyInternet, ip("192.0.2.5")}, {familyLocal, "remotehost"}, local}},
		{"[2001:db8::5]:10", tcp("2001:db8::5"), []authKey{{familyInternet6, ip6("2001:db8::5")}, {familyLocal, "[2001:db8::5]"}, local}},
	} {
		d, err := parseDisplay(tc.display)
		if err != nil {
			t.Fatal(err)
		}
		if got := authKeys(d, tc.remote); !slices.Equal(got, tc.want) {
			t.Errorf("authKeys(%q, %v) = %+v, want %+v", tc.display, tc.remote, got, tc.want)
		}
	}
}

type testEntry struct {
	family             uint16
	addr, number, name string
	data               []byte
}

func writeAuthority(t *testing.T, entries ...testEntry) string {
	t.Helper()
	var b bytes.Buffer
	put := func(s []byte) {
		_ = binary.Write(&b, binary.BigEndian, uint16(len(s)))
		b.Write(s)
	}
	for _, e := range entries {
		_ = binary.Write(&b, binary.BigEndian, e.family)
		put([]byte(e.addr))
		put([]byte(e.number))
		put([]byte(e.name))
		put(e.data)
	}
	path := filepath.Join(t.TempDir(), "Xauthority")
	if err := os.WriteFile(path, b.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func cookie(b byte) []byte { return bytes.Repeat([]byte{b}, 16) }

func TestFindCookie(t *testing.T) {
	ip := string(net.ParseIP("192.0.2.5").To4())
	file := writeAuthority(t,
		testEntry{familyLocal, "desk", "11", "XDM-AUTHORIZATION-1", cookie(9)}, // unsupported
		testEntry{familyLocal, "desk", "10", cookieName, cookie(2)},
		testEntry{familyInternet, ip, "10", cookieName, cookie(1)},
		testEntry{familyLocal, "desk", "12", cookieName, []byte("short")},
		testEntry{familyWild, "", "", cookieName, cookie(3)},
	)
	for _, tc := range []struct {
		number int
		keys   []authKey
		want   []byte
	}{
		{10, []authKey{{familyInternet, ip}, {familyLocal, "desk"}}, cookie(1)},
		{10, []authKey{{familyLocal, "desk"}, {familyInternet, ip}}, cookie(2)},
		{11, []authKey{{familyLocal, "desk"}}, cookie(3)}, // only the wildcard serves MIT-MAGIC-COOKIE-1
		{12, []authKey{{familyLocal, "desk"}}, cookie(3)},
		{0, []authKey{{familyLocal, "other"}}, cookie(3)},
	} {
		got, err := findCookie(file, tc.number, tc.keys)
		if err != nil || !bytes.Equal(got, tc.want) {
			t.Errorf("findCookie(%d, %+v) = %x, %v; want %x", tc.number, tc.keys, got, err, tc.want)
		}
	}
	file = writeAuthority(t, testEntry{familyLocal, "desk", "10", cookieName, cookie(2)})
	if got, err := findCookie(file, 11, []authKey{{familyLocal, "desk"}}); got != nil || err == nil {
		t.Fatalf("no entry: %x, %v", got, err)
	}
	if _, err := findCookie(filepath.Join(t.TempDir(), "none"), 0, nil); err == nil {
		t.Fatal("a missing file gave a cookie")
	}
	// A truncated file still serves the entries before the damage.
	data, _ := os.ReadFile(file)
	_ = os.WriteFile(file, append(data, 0, 1, 0), 0o600)
	if got, _ := findCookie(file, 10, []authKey{{familyLocal, "desk"}}); !bytes.Equal(got, cookie(2)) {
		t.Fatalf("truncated file: %x", got)
	}
}

// readSetup reads an X connection setup request and returns its
// authorization name and data.
func readSetup(c net.Conn) (name string, data []byte, err error) {
	_ = c.SetReadDeadline(time.Now().Add(3 * time.Second))
	head := make([]byte, 12)
	if _, err := io.ReadFull(c, head); err != nil {
		return "", nil, err
	}
	nameLen, dataLen := binary.LittleEndian.Uint16(head[6:]), binary.LittleEndian.Uint16(head[8:])
	pad := func(n uint16) int { return (int(n) + 3) &^ 3 }
	rest := make([]byte, pad(nameLen)+pad(dataLen))
	if _, err := io.ReadFull(c, rest); err != nil {
		return "", nil, err
	}
	return string(rest[:nameLen]), rest[pad(nameLen) : pad(nameLen)+int(dataLen)], nil
}

// The owner authenticates with the cookie from its Authority file, not
// from $XAUTHORITY.
func TestConnectSendsTheAuthorityCookie(t *testing.T) {
	type setup struct {
		name string
		data []byte
	}
	got := make(chan setup, 1)
	display := fakeDisplay(t, func(c net.Conn) {
		name, data, _ := readSetup(c)
		got <- setup{name, data}
		_ = c.Close()
	})
	hostname, _ := os.Hostname()
	file := writeAuthority(t,
		testEntry{familyLocal, hostname, "1", cookieName, cookie(7)},
		testEntry{familyLocal, hostname, "0", cookieName, cookie(5)},
	)
	o := &Owner{Display: display, Authority: file, Timeout: time.Second}
	if err := o.Copy([]byte("synthetic-secret"), true); err == nil {
		t.Fatal("Copy succeeded against a server that hung up")
	}
	select {
	case s := <-got:
		if s.name != cookieName || !bytes.Equal(s.data, cookie(5)) {
			t.Fatalf("setup sent %q %x", s.name, s.data)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("no connection setup")
	}
}

// Over TCP to a loopback address, a cookie stored for the IP address (as
// xauth does for DISPLAY=127.0.0.1:N) is found after this host's name.
func TestConnectFindsACookieForTheAddress(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Skip("no loopback TCP: " + err.Error())
	}
	t.Cleanup(func() { _ = l.Close() })
	port := l.Addr().(*net.TCPAddr).Port
	if port < 6000 {
		t.Skipf("port %d is below the X range", port)
	}
	number := strconv.Itoa(port - 6000)
	got := make(chan []byte, 1)
	go func() {
		c, err := l.Accept()
		if err != nil {
			return
		}
		_, data, _ := readSetup(c)
		got <- data
		_ = c.Close()
	}()
	t.Setenv("XAUTHORITY", filepath.Join(t.TempDir(), "none"))
	file := writeAuthority(t, testEntry{familyInternet, string(net.IPv4(127, 0, 0, 1).To4()), number, cookieName, cookie(4)})
	o := &Owner{Display: "127.0.0.1:" + number, Authority: file, Timeout: time.Second}
	_ = o.Copy([]byte("synthetic-secret"), true)
	select {
	case data := <-got:
		if !bytes.Equal(data, cookie(4)) {
			t.Fatalf("setup sent %x", data)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("no connection setup")
	}
}
