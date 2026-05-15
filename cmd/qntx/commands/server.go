package commands

import (
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"syscall"
	"time"

	"github.com/pterm/pterm"
	"github.com/spf13/cobra"
	"github.com/teranos/QNTX/am"
	"github.com/teranos/QNTX/errors"
	"github.com/teranos/QNTX/server"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
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

// DeferredPluginInit is set by main's init() to hold the plugin initialization
// function. The server fires this via onReady after it's fully started.
var DeferredPluginInit func()

func init() {
	// Server command flags
	ServerCmd.Flags().BoolVar(&serverTestMode, "test-mode", false, "Run with test database")
	ServerCmd.Flags().StringVar(&serverAtsQuery, "ats", "", "Pre-load graph with an Ax query (e.g., --ats 'role:developer')")
	ServerCmd.Flags().BoolVar(&serverNoBrowser, "no-browser", true, "Disable automatic browser opening")
	ServerCmd.Flags().BoolVar(&serverDevMode, "dev", false, "Enable development mode")
	ServerCmd.Flags().StringVar(&serverDBPath, "db-path", "", "Custom database path (overrides config)")
}

func runServer(cmd *cobra.Command, args []string) error {
	// Bootstrap logger for pre-server startup logging
	zapCfg := zap.NewDevelopmentConfig()
	zapCfg.EncoderConfig.EncodeLevel = zapcore.CapitalColorLevelEncoder
	zapLog, _ := zapCfg.Build()
	bootLog := zapLog.Sugar()
	defer bootLog.Sync()

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

	// Set dev mode early — openDatabase skips integrity check in dev mode
	if serverDevMode {
		am.SetDevMode()
	}

	// Open and migrate database
	dbStart := time.Now()
	database, atsStore, dbPath, rustStore, err := openDatabase(dbPath)
	if err != nil {
		return errors.Wrap(err, "failed to open database")
	}
	defer database.Close()
	bootLog.Infow("openDatabase complete", "took", time.Since(dbStart))

	// Resolve log path from config
	cfg, err := am.Load()
	if err != nil {
		cfg = &am.Config{}
	}
	logPath := cfg.GetLogPath(serverPort)

	// Print startup banner
	printStartupBanner(verbosity, dbPath, logPath, cfg.Plugin.Enabled)

	// Create server with pre-created attestation store
	srvStart := time.Now()
	srv, err := server.NewQNTXServer(database, atsStore, dbPath, verbosity, serverAtsQuery)
	if err != nil {
		return errors.Wrap(err, "failed to create server")
	}
	bootLog.Infow("NewQNTXServer complete", "took", time.Since(srvStart))

	// Wire Rust-side WAL checkpointer (closes read conns, checkpoints, reopens)
	if cp, ok := rustStore.(server.WALCheckpointer); ok {
		srv.SetWALCheckpointer(cp)
	}

	// Wire Rust-side age distiller (fold old attestations into sigmas)
	if ad, ok := rustStore.(server.AgeDistiller); ok {
		srv.SetAgeDistiller(ad)
	}

	// Wire write lock inspector (diagnostics for UI)
	if wl, ok := rustStore.(server.WriteLockInspector); ok {
		srv.SetWriteLockInspector(wl)
	}

	// Wire deferred plugin initialization — fires when server is fully ready
	// (migrations done, HTTP listening), not before.
	if DeferredPluginInit != nil {
		srv.SetOnReady(DeferredPluginInit)
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
		// Server crashed — write deferred news to Ground so the user
		// gets notified at the next stop hook, even if the crash was silent.
		server.WriteDeferredNews(cfg.GroundDBPath, "qntx", "crash",
			"qntx-server", fmt.Sprintf("QNTX crashed: %v", err), nil, bootLog)
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
				return errors.Wrap(err, "shutdown error")
			}
			pterm.Success.Println("Server stopped cleanly")
			return nil
		case <-sigChan:
			// Second Ctrl+C - force immediate exit
			pterm.Warning.Println("\nForce shutdown - exiting immediately")
			os.Exit(1)
		}
	}
	return nil // unreachable but required by compiler
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
