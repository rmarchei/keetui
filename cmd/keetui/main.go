package main

import (
	"crypto/sha256"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"

	tea "charm.land/bubbletea/v2"
	"golang.org/x/term"

	"github.com/rmarchei/keetui/internal/config"
	"github.com/rmarchei/keetui/internal/keepass"
	"github.com/rmarchei/keetui/internal/ui"
)

var version = "dev"

func main() {
	keyFile := flag.String("keyfile", "", "path to key file")
	showVersion := flag.Bool("version", false, "show version")
	showConfig := flag.Bool("config", false, "show config file path and current settings")
	newDB := flag.Bool("new", false, "create a new database")
	exportCSV := flag.String("export-csv", "", "export entries to CSV file")
	exportXML := flag.String("export-xml", "", "export entries to XML file")
	importCSV := flag.String("import-csv", "", "import entries from CSV file")
	importXML := flag.String("import-xml", "", "import entries from XML file")
	mergeDB := flag.String("merge", "", "merge entries from another .kdbx file")
	mergeKeyFile := flag.String("merge-keyfile", "", "path to key file for the merge source database (use with -merge)")
	dryRun := flag.Bool("dry-run", false, "preview changes without modifying (use with --merge)")
	readOnly := flag.Bool("read-only", false, "open database in read-only mode")
	upgradeKDBX4 := flag.Bool("upgrade-kdbx4", false, "upgrade database from KDBX 3.1 to KDBX 4 format")
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "keetui - a TUI for KeePass databases\n\n")
		fmt.Fprintf(os.Stderr, "Usage: keetui [options] <database.kdbx>\n\n")
		fmt.Fprintf(os.Stderr, "Options:\n")
		flag.PrintDefaults()
	}
	flag.Parse()

	if *showVersion {
		fmt.Printf("keetui %s\n", version)
		if h, err := selfHash(); err == nil {
			fmt.Printf("sha256: %s\n", h)
		}
		os.Exit(0)
	}

	if *showConfig {
		cfg, err := config.Load()
		fmt.Fprintf(os.Stderr, "Config file: %s\n\n", config.Path())
		if err != nil {
			fmt.Fprintf(os.Stderr, "Warning: %s\n", err)
			fmt.Fprintln(os.Stderr, "Using default values. Fix the file or delete it to regenerate.")
		}
		// Output must be valid JSON so that `keetui -config > config.json` works.
		// Populate keybindings with effective (merged) values so they appear in output.
		keys := cfg.Keys()
		cfg.KeyBindingsPtr = &keys
		output := cfg.String()
		// Sanity check: verify the output is valid JSON.
		if !json.Valid([]byte(output)) {
			fmt.Fprintln(os.Stderr, "Error: config output is not valid JSON")
			os.Exit(1)
		}
		fmt.Println(output)
		if err != nil {
			os.Exit(1)
		}
		os.Exit(0)
	}

	if flag.NArg() < 1 {
		flag.Usage()
		os.Exit(1)
	}

	dbPath := flag.Arg(0)

	// The flag package stops parsing at the first positional argument, so
	// options placed after the path would be silently ignored (e.g. a
	// -read-only that never takes effect).
	if flag.NArg() > 1 {
		fmt.Fprintf(os.Stderr, "Error: unexpected arguments after database path: %v\n", flag.Args()[1:])
		fmt.Fprintln(os.Stderr, "Options must come before the database path, e.g.: keetui -export-csv out.csv db.kdbx")
		os.Exit(1)
	}

	if strings.HasSuffix(strings.ToLower(dbPath), ".kdb") {
		fmt.Fprintln(os.Stderr, "Error: KeePass 1.x .kdb files are not supported. Please convert to .kdbx format using KeePass 2 or KeePassXC.")
		os.Exit(1)
	}

	if *keyFile != "" {
		if _, err := os.Stat(*keyFile); os.IsNotExist(err) {
			fmt.Fprintf(os.Stderr, "Error: key file %q does not exist\n", *keyFile)
			os.Exit(1)
		}
	}

	if *newDB {
		if _, err := os.Stat(dbPath); err == nil {
			fmt.Fprintf(os.Stderr, "Error: file %q already exists\n", dbPath)
			os.Exit(1)
		}
		// Prompt for password
		fmt.Printf("Creating new database: %s\n", dbPath)
		password, err := readPassword("Enter master password: ")
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error reading password: %v\n", err)
			os.Exit(1)
		}
		// A keyfile-only database has no master password.
		if password == "" && *keyFile == "" {
			fmt.Fprintln(os.Stderr, "Error: password cannot be empty")
			os.Exit(1)
		}
		if password != "" {
			confirm, err := readPassword("Confirm master password: ")
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error reading password: %v\n", err)
				os.Exit(1)
			}
			if password != confirm {
				fmt.Fprintln(os.Stderr, "Error: passwords do not match")
				os.Exit(1)
			}
		}
		if _, err := keepass.CreateWithKeyFile(dbPath, password, *keyFile); err != nil {
			fmt.Fprintf(os.Stderr, "Error creating database: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("Database created: %s\n", dbPath)
	} else {
		if _, err := os.Stat(dbPath); os.IsNotExist(err) {
			fmt.Fprintf(os.Stderr, "Error: file %q does not exist\n", dbPath)
			os.Exit(1)
		}
	}

	if *dryRun && *mergeDB == "" {
		fmt.Fprintln(os.Stderr, "Error: --dry-run requires --merge")
		os.Exit(1)
	}

	if *mergeKeyFile != "" {
		if *mergeDB == "" {
			fmt.Fprintln(os.Stderr, "Error: --merge-keyfile requires --merge")
			os.Exit(1)
		}
		if _, err := os.Stat(*mergeKeyFile); os.IsNotExist(err) {
			fmt.Fprintf(os.Stderr, "Error: key file %q does not exist\n", *mergeKeyFile)
			os.Exit(1)
		}
	}

	// Block mutating batch operations in read-only mode
	if *readOnly && (*importCSV != "" || *importXML != "" || (*mergeDB != "" && !*dryRun) || *upgradeKDBX4) {
		fmt.Fprintln(os.Stderr, "Error: import, merge, and upgrade are not allowed in read-only mode")
		os.Exit(1)
	}

	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: %s\n", err)
	}

	// Handle non-interactive commands (export, import, merge, upgrade)
	isBatchOp := *exportCSV != "" || *exportXML != "" || *importCSV != "" || *importXML != "" || *mergeDB != "" || *upgradeKDBX4
	if isBatchOp {
		db := openDBForBatch(dbPath, *keyFile)
		db.BackupOnSave = cfg.BackupEnabled()

		// Refuse to modify a database whose protected values decoded as
		// garbage (stream cipher misalignment, e.g. an incompatible writer):
		// saving would persist the corruption.
		if db.SuspectCorruption() && (*importCSV != "" || *importXML != "" || (*mergeDB != "" && !*dryRun) || *upgradeKDBX4) {
			fmt.Fprintln(os.Stderr, "Error: protected values in this database look corrupted (incompatible writer?); refusing to modify it. See README.")
			os.Exit(1)
		}

		if *exportCSV != "" {
			if err := db.ExportCSVFile(*exportCSV); err != nil {
				fmt.Fprintf(os.Stderr, "Error exporting CSV: %v\n", err)
				os.Exit(1)
			}
			fmt.Printf("Exported to %s\n", *exportCSV)
		}

		if *exportXML != "" {
			if err := db.ExportXMLFile(*exportXML); err != nil {
				fmt.Fprintf(os.Stderr, "Error exporting XML: %v\n", err)
				os.Exit(1)
			}
			fmt.Printf("Exported to %s\n", *exportXML)
		}

		if *importCSV != "" {
			n, err := db.ImportCSVFile(*importCSV)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error importing CSV: %v\n", err)
				os.Exit(1)
			}
			if err := db.Save(); err != nil {
				fmt.Fprintf(os.Stderr, "Error saving after import: %v\n", err)
				os.Exit(1)
			}
			fmt.Printf("Imported %d entries from %s\n", n, *importCSV)
		}

		if *importXML != "" {
			n, err := db.ImportXMLFile(*importXML)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error importing XML: %v\n", err)
				os.Exit(1)
			}
			if err := db.Save(); err != nil {
				fmt.Fprintf(os.Stderr, "Error saving after import: %v\n", err)
				os.Exit(1)
			}
			fmt.Printf("Imported %d entries from %s\n", n, *importXML)
		}

		if *mergeDB != "" {
			other := openDBForBatch(*mergeDB, *mergeKeyFile)
			if other.SuspectCorruption() && !*dryRun {
				fmt.Fprintln(os.Stderr, "Error: protected values in the merge source look corrupted (incompatible writer?); refusing to merge from it. See README.")
				os.Exit(1)
			}
			if *dryRun {
				result, err := db.MergePreview(other)
				if err != nil {
					fmt.Fprintf(os.Stderr, "Error analyzing merge: %v\n", err)
					os.Exit(1)
				}
				printMergePreview(result)
			} else {
				result, err := db.Merge(other)
				if err != nil {
					fmt.Fprintf(os.Stderr, "Error merging: %v\n", err)
					os.Exit(1)
				}
				if err := db.Save(); err != nil {
					fmt.Fprintf(os.Stderr, "Error saving after merge: %v\n", err)
					os.Exit(1)
				}
				fmt.Printf("Merged: %d added, %d updated, %d unchanged, %d groups created\n",
					result.EntriesAdded, result.EntriesUpdated, result.EntriesSkipped, result.GroupsCreated)
			}
		}

		if *upgradeKDBX4 {
			if db.IsKdbx4() {
				fmt.Fprintln(os.Stderr, "Database is already KDBX 4 format")
				os.Exit(0)
			}
			if err := db.ConvertToKDBX4(); err != nil {
				fmt.Fprintf(os.Stderr, "Error upgrading to KDBX 4: %v\n", err)
				os.Exit(1)
			}
			if err := db.Save(); err != nil {
				fmt.Fprintf(os.Stderr, "Error saving after upgrade: %v\n", err)
				os.Exit(1)
			}
			fmt.Printf("Upgraded %s to KDBX 4 (Argon2) format\n", dbPath)
		}

		os.Exit(0)
	}

	app := ui.NewApp(dbPath, *keyFile, cfg, *readOnly)
	p := tea.NewProgram(app)
	// Bubble Tea does not handle SIGHUP (terminal closed, SSH dropped), which
	// would otherwise kill the process with a password on the clipboard.
	hup := make(chan os.Signal, 1)
	signal.Notify(hup, syscall.SIGHUP)
	go func() {
		if _, ok := <-hup; ok {
			p.Quit()
		}
	}()
	_, err = p.Run()
	signal.Stop(hup)
	close(hup)
	// SIGTERM, SIGINT and recovered panics end Run without the model's quit
	// handling, so release the clipboard here as well.
	ui.ReleaseClipboard()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

// openDBForBatch opens the database for batch operations, prompting for password.
func openDBForBatch(dbPath, keyFilePath string) *keepass.DB {
	password, err := readPassword(fmt.Sprintf("Enter master password for %s: ", dbPath))
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error reading password: %v\n", err)
		os.Exit(1)
	}

	var db *keepass.DB
	if keyFilePath != "" {
		db, err = keepass.OpenWithKeyFile(dbPath, password, keyFilePath)
	} else {
		db, err = keepass.Open(dbPath, password)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error opening database: %v\n", err)
		os.Exit(1)
	}
	return db
}

// readPassword prompts on stderr (visible even with stdout redirected) and
// reads a password without echo. If the process is interrupted mid-read,
// the terminal state is restored before exiting — otherwise Ctrl+C would
// leave the shell with echo disabled.
func readPassword(prompt string) (string, error) {
	fmt.Fprint(os.Stderr, prompt)
	fd := int(syscall.Stdin)
	if state, err := term.GetState(fd); err == nil {
		sig := make(chan os.Signal, 1)
		signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
		done := make(chan struct{})
		go func() {
			select {
			case <-sig:
				_ = term.Restore(fd, state)
				fmt.Fprintln(os.Stderr)
				os.Exit(130)
			case <-done:
			}
		}()
		defer func() {
			signal.Stop(sig)
			close(done)
		}()
	}
	pwBytes, err := term.ReadPassword(fd)
	fmt.Fprintln(os.Stderr)
	if err != nil {
		return "", err
	}
	password := string(pwBytes)
	zeroBytes(pwBytes)
	return password, nil
}

// zeroBytes overwrites a byte slice with zeros to reduce sensitive data exposure in memory.
func zeroBytes(b []byte) {
	for i := range b {
		b[i] = 0
	}
}

func printMergePreview(result keepass.MergeResult) {
	fmt.Println("Merge preview (dry run - no changes will be made):")
	fmt.Println()

	if len(result.GroupActions) > 0 {
		fmt.Println("Groups:")
		for _, g := range result.GroupActions {
			switch g.Action {
			case "create":
				fmt.Printf("  + CREATE  %s (in %s)\n", g.Name, g.ParentPath)
			case "exists":
				fmt.Printf("  = EXISTS  %s (in %s)\n", g.Name, g.ParentPath)
			}
		}
		fmt.Println()
	}

	if len(result.EntryActions) > 0 {
		fmt.Println("Entries:")
		for _, e := range result.EntryActions {
			switch e.Action {
			case "add":
				fmt.Printf("  + ADD     %s (%s) [%s]\n", e.Title, e.UserName, e.GroupPath)
			case "update":
				fmt.Printf("  ~ UPDATE  %s (%s) [%s]\n", e.Title, e.UserName, e.GroupPath)
			case "skip":
				fmt.Printf("  = SKIP    %s (%s) [%s]\n", e.Title, e.UserName, e.GroupPath)
			}
		}
		fmt.Println()
	}

	fmt.Printf("Summary: %d to add, %d to update, %d unchanged, %d groups to create\n",
		result.EntriesAdded, result.EntriesUpdated, result.EntriesSkipped, result.GroupsCreated)
}

// selfHash returns the SHA256 hex digest of the running binary.
func selfHash() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	f, err := os.Open(exe)
	if err != nil {
		return "", err
	}
	defer func() { _ = f.Close() }()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return fmt.Sprintf("%x", h.Sum(nil)), nil
}
