package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/rmarchei/keetui/internal/config"
)

const cliHelperEnv = "KEETUI_TEST_CLI_HELPER"

// Run main in a child so flag parsing, os.Exit and stdout/stderr are tested
// together. Every child gets an existing scratch config: XDG alone would
// otherwise allow the legacy macOS lookup to reach the user's settings.
func TestCLIHelperProcess(t *testing.T) {
	if os.Getenv(cliHelperEnv) != "1" {
		return
	}
	separator := slices.Index(os.Args, "--")
	if separator < 0 {
		os.Exit(99)
	}
	os.Args = append([]string{os.Args[0]}, os.Args[separator+1:]...)
	flag.CommandLine = flag.NewFlagSet("keetui", flag.ExitOnError)
	if os.Getenv("KEETUI_TEST_READ_PASSWORD") == "1" {
		password, err := readPassword("Synthetic password: ")
		if err != nil {
			os.Exit(1)
		}
		fmt.Printf("read %d bytes\n", len(password))
		os.Exit(0)
	}
	main()
	os.Exit(0)
}

func newCLICommand(t *testing.T, configJSON string, args ...string) *exec.Cmd {
	t.Helper()
	root := t.TempDir()
	dir := filepath.Join(root, "keetui")
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "config.json"), []byte(configJSON), 0600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	t.Cleanup(cancel)
	cmd := exec.CommandContext(ctx, os.Args[0], append([]string{"-test.run=^TestCLIHelperProcess$", "--"}, args...)...)
	cmd.Env = append(os.Environ(), cliHelperEnv+"=1", "XDG_CONFIG_HOME="+root, "GORACE=atexit_sleep_ms=0")
	// Go's coverage exit hooks also handle a test binary acting as the
	// command under test. Include child counters in the parent's report.
	if coverage := flag.Lookup("test.gocoverdir"); coverage != nil && coverage.Value.String() != "" {
		cmd.Env = append(cmd.Env, "GOCOVERDIR="+coverage.Value.String())
	}
	return cmd
}

func runCLI(t *testing.T, configJSON string, args ...string) (stdout, stderr string, exitCode int) {
	t.Helper()
	cmd := newCLICommand(t, configJSON, args...)
	var out, errOut bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errOut
	err := cmd.Run()
	if err != nil {
		var exit *exec.ExitError
		if !errors.As(err, &exit) {
			t.Fatal(err)
		}
		exitCode = exit.ExitCode()
	}
	return out.String(), errOut.String(), exitCode
}

func TestCLIRejectsUnsafeArgumentsBeforeReadingPasswords(t *testing.T) {
	dir := t.TempDir()
	vault, key, source := filepath.Join(dir, "vault.kdbx"), filepath.Join(dir, "vault.key"), filepath.Join(dir, "source.kdbx")
	for _, path := range []string{vault, key, source} {
		if err := os.WriteFile(path, []byte("untouched synthetic input"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	hardlink, symlink := filepath.Join(dir, "alias.kdbx"), filepath.Join(dir, "link.kdbx")
	if err := os.Link(vault, hardlink); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(vault, symlink); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name string
		args []string
		want string
	}{
		{"missing path", nil, "Usage:"},
		{"misplaced read-only", []string{vault, "-read-only"}, "Options must come before"},
		{"multiple paths", []string{vault, source}, "unexpected arguments"},
		{"legacy format", []string{filepath.Join(dir, "old.KDB")}, "KeePass 1.x"},
		{"missing vault", []string{filepath.Join(dir, "missing.kdbx")}, "does not exist"},
		{"missing key", []string{"-keyfile", filepath.Join(dir, "missing.key"), vault}, "key file"},
		{"existing new vault", []string{"-new", vault}, "already exists"},
		{"dry run without merge", []string{"-dry-run", vault}, "--dry-run requires --merge"},
		{"orphan merge key", []string{"-merge-keyfile", key, vault}, "--merge-keyfile requires --merge"},
		{"missing merge key", []string{"-merge", source, "-merge-keyfile", filepath.Join(dir, "missing.key"), vault}, "key file"},
		{"read-only CSV import", []string{"-read-only", "-import-csv", source, vault}, "not allowed in read-only"},
		{"read-only XML import", []string{"-read-only", "-import-xml", source, vault}, "not allowed in read-only"},
		{"read-only merge", []string{"-read-only", "-merge", source, vault}, "not allowed in read-only"},
		{"read-only upgrade", []string{"-read-only", "-upgrade-kdbx4", vault}, "not allowed in read-only"},
		{"CSV overwrites vault", []string{"-export-csv", vault, vault}, "it is the database"},
		{"XML overwrites vault", []string{"-export-xml", vault, vault}, "it is the database"},
		{"export follows hardlink", []string{"-export-csv", hardlink, vault}, "it is the database"},
		{"export follows symlink", []string{"-export-csv", symlink, vault}, "it is the database"},
		{"export overwrites key", []string{"-keyfile", key, "-export-xml", key, vault}, "it is the key file"},
		{"export overwrites merge source", []string{"-merge", source, "-export-csv", source, vault}, "it is the merge source"},
		{"export overwrites merge key", []string{"-merge", source, "-merge-keyfile", key, "-export-csv", key, vault}, "it is the merge key file"},
		{"export overwrites CSV import", []string{"-import-csv", source, "-export-csv", source, vault}, "it is the CSV import"},
		{"export overwrites XML import", []string{"-import-xml", source, "-export-xml", source, vault}, "it is the XML import"},
		{"exports share path", []string{"-export-csv", filepath.Join(dir, "out"), "-export-xml", filepath.Join(dir, "out"), vault}, "both go to"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, stderr, code := runCLI(t, "{}", tc.args...)
			if code != 1 || !strings.Contains(stderr, tc.want) {
				t.Fatalf("exit %d, stderr %q; want exit 1 and %q", code, stderr, tc.want)
			}
			if strings.Contains(stderr, "Enter master password") {
				t.Fatal("invalid arguments reached password entry")
			}
			for _, path := range []string{vault, key, source} {
				data, err := os.ReadFile(path)
				if err != nil || string(data) != "untouched synthetic input" {
					t.Fatalf("rejected command changed input %s: %v", path, err)
				}
			}
		})
	}
}

func TestCLIConfigOutputSeparatesJSONFromDiagnostics(t *testing.T) {
	for _, tc := range []struct {
		name, input string
		exit        int
		wantDelay   int
	}{
		{"valid config", `{"auto_lock_minutes":10,"keybindings":{"quit":"X"},"privacy_mode":true}`, 0, 10},
		{"invalid JSON", `{"auto_lock_minutes":`, 1, 5},
		{"partial type error", `{"auto_lock_minutes":10,"password_length":"wrong"}`, 1, 5},
		{"conflicting keys", `{"keybindings":{"edit":"j"}}`, 1, 5},
		{"invalid color", `{"colors":{"dark":{"primary":"not a color"}}}`, 1, 5},
	} {
		t.Run(tc.name, func(t *testing.T) {
			stdout, stderr, code := runCLI(t, tc.input, "-config")
			if code != tc.exit || !json.Valid([]byte(stdout)) || !strings.Contains(stderr, "Config file:") {
				t.Fatalf("exit %d, stdout %q, stderr %q", code, stdout, stderr)
			}
			var cfg config.Config
			if err := json.Unmarshal([]byte(stdout), &cfg); err != nil {
				t.Fatal(err)
			}
			if cfg.AutoLockMinutes != tc.wantDelay || cfg.KeyBindingsPtr == nil || cfg.Keys().Validate() != nil {
				t.Fatalf("config output is incomplete or unsafe: %+v", cfg)
			}
			if tc.exit != 0 && !strings.Contains(stderr, "Warning:") {
				t.Fatal("failed config load did not report its warning")
			}
			if tc.name == "valid config" && (cfg.Keys().Quit != "X" || !cfg.HidePasswords()) {
				t.Fatal("config output dropped customized settings")
			}
		})
	}
}

func TestCLIVersionReportsExecutableDigest(t *testing.T) {
	stdout, stderr, code := runCLI(t, "{}", "-version")
	if code != 0 || stderr != "" {
		t.Fatalf("version exit %d: %s", code, stderr)
	}
	executable, err := os.ReadFile(os.Args[0])
	if err != nil {
		t.Fatal(err)
	}
	want := fmt.Sprintf("keetui %s\nsha256: %x\n", version, sha256.Sum256(executable))
	if stdout != want {
		t.Fatalf("version output %q, want %q", stdout, want)
	}
}

func TestCLIUnknownFlagFails(t *testing.T) {
	_, stderr, code := runCLI(t, "{}", "-unknown-keetui-flag")
	if code != 2 || !strings.Contains(stderr, "flag provided but not defined") {
		t.Fatalf("unknown flag: exit %d, stderr %q", code, stderr)
	}
}
