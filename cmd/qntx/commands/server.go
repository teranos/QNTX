package commands

import (
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"syscall"

	"github.com/pterm/pterm"
	"github.com/spf13/cobra"
	"github.com/teranos/QNTX/am"
	"github.com/teranos/QNTX/errors"
	"github.com/teranos/QNTX/server"
)

// ServerCmd starts the QNTX web server
var ServerCmd = &cobra.Command{
	Use:     "server",
	Aliases: []string{"serve"},
	Short:   "Start the QNTX server for graph visualization and attestation exploration",
	Long:    `Launch the QNTX server with graph visualization interface. Type Ax queries to visualize relationships, explore attestations, and navigate the continuous intelligence substrate.`,
	RunE:    runServer,
}

var (
	serverTestMode  bool
	serverAtsQuery  string
	serverNoBrowser bool
	serverDevMode   bool
	serverDBPath    string
)

func init() {
	// Server command flags
	ServerCmd.Flags().BoolVar(&serverTestMode, "test-mode", false, "Run with test database")
	ServerCmd.Flags().StringVar(&serverAtsQuery, "ats", "", "Pre-load graph with an Ax query (e.g., --ats 'role:developer')")
	ServerCmd.Flags().BoolVar(&serverNoBrowser, "no-browser", true, "Disable automatic browser opening")
	ServerCmd.Flags().BoolVar(&serverDevMode, "dev", false, "Enable development mode")
	ServerCmd.Flags().StringVar(&serverDBPath, "db-path", "", "Custom database path (overrides config)")
}

func runServer(cmd *cobra.Command, args []string) error {
	// Get verbosity flag - default to 1 (Info) for server
	verbosity, _ := cmd.Flags().GetCount("verbose")
	if verbosity == 0 {
		verbosity = 1
	}

	// Get server port from config system (env > project > user > system > default)
	serverPort := am.GetServerPort()

	// Determine database path - priority: --db-path flag > --test-mode > DB_PATH env > config
	var dbPath string
	if serverDBPath != "" {
		dbPath = serverDBPath
	} else if serverTestMode {
		dbPath = "tmp/test-qntx.db"
	}
	// If dbPath still empty, openDatabase will use am.GetDatabasePath()

	// Open and migrate database
	database, err := openDatabase(dbPath)
	if err != nil {
		return errors.Wrap(err, "failed to open database")
	}
	defer database.Close()

	// Get actual path for banner (openDatabase resolved it)
	if serverDBPath != "" {
		dbPath = serverDBPath
	} else if serverTestMode {
		dbPath = "tmp/test-qntx.db"
	} else {
		// Resolve the actual path used by openDatabase
		resolvedPath, err := am.GetDatabasePath()
		if err != nil {
			dbPath = "qntx.db" // Default fallback, same as openDatabase
		} else if resolvedPath != "" {
			dbPath = resolvedPath
		} else {
			dbPath = "qntx.db"
		}
	}

	// Print startup banner
	printStartupBanner(verbosity, dbPath)

	if serverAtsQuery != "" {
		pterm.Info.Printf("Pre-loaded query: %s\n", serverAtsQuery)
	}

	// Set dev mode environment variable if flag is set
	if serverDevMode {
		os.Setenv("DEV", "true")
	}

	// Create server
	srv, err := server.NewQNTXServer(database, dbPath, verbosity, serverAtsQuery)
	if err != nil {
		return fmt.Errorf("failed to create server: %w", err)
	}

	// Start server in goroutine
	// The server will call openBrowser with the actual port (unless --no-browser is set)
	var browserFunc func(string)
	if !serverNoBrowser {
		browserFunc = openBrowser
	}

	// Start server in background
	errChan := make(chan error, 1)
	go func() {
		errChan <- srv.Start(serverPort, browserFunc)
	}()

	// GRACE: Wait for shutdown signal (Ctrl+C)
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	select {
	case err := <-errChan:
		// Server failed to start or stopped unexpectedly
		return errors.Wrap(err, "server failed to start")
	case <-sigChan:
		// First Ctrl+C - graceful shutdown
		pterm.Info.Println("\nShutting down gracefully (press Ctrl+C again to force)...")

		// Start graceful shutdown in background
		shutdownDone := make(chan error, 1)
		go func() {
			shutdownDone <- srv.Stop()
		}()

		// Wait for either shutdown completion or second Ctrl+C
		select {
		case err := <-shutdownDone:
			// Graceful shutdown completed
			if err != nil {
				return fmt.Errorf("shutdown error: %w", err)
			}
			pterm.Success.Println("Server stopped cleanly")
			return nil
		case <-sigChan:
			// Second Ctrl+C - force immediate exit
			pterm.Warning.Println("\nForce shutdown - exiting immediately")
			os.Exit(1)
			return nil // unreachable
		}
	}
}

// openBrowser attempts to open the URL in the default browser
func openBrowser(url string) {
	var err error
	switch runtime.GOOS {
	case "darwin":
		// Try to open with Chrome directly with performance flags
		chromeApp := "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome"
		if _, statErr := os.Stat(chromeApp); statErr == nil {
			// Chrome found - launch with flags to reduce GC pauses and disable extensions
			err = exec.Command(chromeApp,
				"--disable-extensions",            // Disable all extensions
				"--disable-background-networking", // Disable background tasks
				"--disable-sync",                  // Disable sync
				url,
			).Start()
		} else {
			// Fallback to default browser
			err = exec.Command("open", url).Start()
		}
	case "linux":
		err = exec.Command("xdg-open", url).Start()
	case "windows":
		err = exec.Command("cmd", "/c", "start", url).Start()
	}
	// Silently ignore errors - user can manually open the URL
	_ = err
}
