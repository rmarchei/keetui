package main

import (
	"crypto/sha256"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"syscall"

	tea "github.com/charmbracelet/bubbletea"
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
		fmt.Println(cfg)
		if err != nil {
			fmt.Fprintf(os.Stderr, "\nWarning: %s\n", err)
			fmt.Fprintln(os.Stderr, "Using default values. Fix the file or delete it to regenerate.")
			os.Exit(1)
		}
		fmt.Println("\nKeybindings:")
		fmt.Print(cfg.Keys())
		os.Exit(0)
	}

	if flag.NArg() < 1 {
		flag.Usage()
		os.Exit(1)
	}

	dbPath := flag.Arg(0)

	if strings.HasSuffix(strings.ToLower(dbPath), ".kdb") {
		fmt.Fprintln(os.Stderr, "Error: KeePass 1.x .kdb files are not supported. Please convert to .kdbx format using KeePass 2 or KeePassXC.")
		os.Exit(1)
	}

	if *newDB {
		if _, err := os.Stat(dbPath); err == nil {
			fmt.Fprintf(os.Stderr, "Error: file %q already exists\n", dbPath)
			os.Exit(1)
		}
		// Prompt for password
		fmt.Printf("Creating new database: %s\n", dbPath)
		fmt.Print("Enter master password: ")
		pwBytes, err := term.ReadPassword(int(syscall.Stdin))
		fmt.Println()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error reading password: %v\n", err)
			os.Exit(1)
		}
		password := string(pwBytes)
		zeroBytes(pwBytes)
		if password == "" {
			fmt.Fprintln(os.Stderr, "Error: password cannot be empty")
			os.Exit(1)
		}
		fmt.Print("Confirm master password: ")
		confirmBytes, err := term.ReadPassword(int(syscall.Stdin))
		fmt.Println()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error reading password: %v\n", err)
			os.Exit(1)
		}
		confirm := string(confirmBytes)
		zeroBytes(confirmBytes)
		if password != confirm {
			fmt.Fprintln(os.Stderr, "Error: passwords do not match")
			os.Exit(1)
		}
		if _, err := keepass.Create(dbPath, password); err != nil {
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

	if *keyFile != "" {
		if _, err := os.Stat(*keyFile); os.IsNotExist(err) {
			fmt.Fprintf(os.Stderr, "Error: key file %q does not exist\n", *keyFile)
			os.Exit(1)
		}
	}

	// Handle non-interactive commands (export, import, merge)
	isBatchOp := *exportCSV != "" || *exportXML != "" || *importCSV != "" || *importXML != "" || *mergeDB != ""
	if isBatchOp {
		db := openDBForBatch(dbPath, *keyFile)

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
			other := openDBForBatch(*mergeDB, "")
			result, err := db.Merge(other)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error merging: %v\n", err)
				os.Exit(1)
			}
			if err := db.Save(); err != nil {
				fmt.Fprintf(os.Stderr, "Error saving after merge: %v\n", err)
				os.Exit(1)
			}
			fmt.Printf("Merged: %d entries added, %d groups created\n", result.EntriesAdded, result.GroupsCreated)
		}

		os.Exit(0)
	}

	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: %s (using defaults)\n", err)
	}
	app := ui.NewApp(dbPath, *keyFile, cfg)
	p := tea.NewProgram(app, tea.WithAltScreen(), tea.WithMouseCellMotion())
	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

// openDBForBatch opens the database for batch operations, prompting for password.
func openDBForBatch(dbPath, keyFilePath string) *keepass.DB {
	fmt.Print("Enter master password: ")
	pwBytes, err := term.ReadPassword(int(syscall.Stdin))
	fmt.Println()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error reading password: %v\n", err)
		os.Exit(1)
	}
	password := string(pwBytes)
	zeroBytes(pwBytes)

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

// zeroBytes overwrites a byte slice with zeros to reduce sensitive data exposure in memory.
func zeroBytes(b []byte) {
	for i := range b {
		b[i] = 0
	}
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
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return fmt.Sprintf("%x", h.Sum(nil)), nil
}
