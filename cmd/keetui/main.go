package main

import (
	"flag"
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/rmarchei/keetui/internal/config"
	"github.com/rmarchei/keetui/internal/keepass"
	"github.com/rmarchei/keetui/internal/ui"
)

var version = "dev"

func main() {
	keyFile := flag.String("keyfile", "", "path to key file")
	showVersion := flag.Bool("version", false, "show version")
	newDB := flag.Bool("new", false, "create a new database")
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "keetui - a TUI for KeePass databases\n\n")
		fmt.Fprintf(os.Stderr, "Usage: keetui [options] <database.kdbx>\n\n")
		fmt.Fprintf(os.Stderr, "Options:\n")
		flag.PrintDefaults()
	}
	flag.Parse()

	if *showVersion {
		fmt.Printf("keetui %s\n", version)
		os.Exit(0)
	}

	if flag.NArg() < 1 {
		flag.Usage()
		os.Exit(1)
	}

	dbPath := flag.Arg(0)

	if *newDB {
		if _, err := os.Stat(dbPath); err == nil {
			fmt.Fprintf(os.Stderr, "Error: file %q already exists\n", dbPath)
			os.Exit(1)
		}
		// Prompt for password
		fmt.Printf("Creating new database: %s\n", dbPath)
		fmt.Print("Enter master password: ")
		var password string
		fmt.Scanln(&password)
		if password == "" {
			fmt.Fprintln(os.Stderr, "Error: password cannot be empty")
			os.Exit(1)
		}
		fmt.Print("Confirm master password: ")
		var confirm string
		fmt.Scanln(&confirm)
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

	cfg := config.Load()
	app := ui.NewApp(dbPath, *keyFile, cfg)
	p := tea.NewProgram(app, tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
