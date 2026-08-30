package commands

import (
	"fmt"
	"github.com/teranos/QNTX/internal/sqlclose"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"syscall"
	"time"

	"github.com/pterm/pterm"
	"github.com/spf13/cobra"
	"github.com/teranos/QNTX/ats"
	"github.com/teranos/QNTX/ats/storage"
	"github.com/teranos/QNTX/internal/config"
	"github.com/teranos/QNTX/internal/logger"
	"github.com/teranos/QNTX/server"
	"github.com/teranos/errors"
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
	ServerCmd.Flags().BoolVar(&serverNoBrowser, "no-browser", true, "Disable automatic browser opening")
	ServerCmd.Flags().BoolVar(&serverDevMode, "dev", false, "Enable development mode")
	ServerCmd.Flags().StringVar(&serverDBPath, "db-path", "", "Custom database path (overrides config)")
}

func runServer(cmd *cobra.Command, args []string) (err error) {
	// Bootstrap logger for pre-server startup logging. A server that cannot
	// build its logger would run mute; refusing to start says so instead.
	zapCfg := zap.NewDevelopmentConfig()
	zapCfg.EncoderConfig.EncodeLevel = zapcore.CapitalColorLevelEncoder
	zapLog, err := zapCfg.Build()
	if err != nil {
		return errors.Wrap(err, "failed to build the bootstrap logger")
	}
	bootLog := zapLog.Sugar()
	// No Sync defer: the sink is stderr, which needs no flush — Sync on a
	// terminal answers ENOTTY, an error that means nothing here.

	// GetCount fails only for a flag that does not exist — a broken registration.
	verbosity, err := cmd.Flags().GetCount("verbose")
	if err != nil {
		return errors.Wrap(err, "the verbose flag is not registered as a count")
	}
	if verbosity == 0 {
		verbosity = 1
	}

	// Get server port from config system (env > project > user > system > default)
	serverPort := config.GetServerPort()

	// Determine database path - priority: --db-path flag > --test-mode > DB_PATH env > config
	var dbPath string
	if serverDBPath != "" {
		dbPath = serverDBPath
	} else if serverTestMode {
		dbPath = "tmp/test-qntx.db"
	}
	// If dbPath still empty, openDatabase will use config.GetDatabasePath()

	// Set dev mode early — openDatabase skips integrity check in dev mode
	if serverDevMode {
		config.SetDevMode()
	}

	// Open and migrate database
	dbStart := time.Now()
	database, atsStore, dbPath, rustStore, err := openDatabase(dbPath)
	if err != nil {
		return errors.Wrap(err, "failed to open database")
	}
	defer func() { err = sqlclose.With(err, database.Close(), "the server database") }()
	bootLog.Infow("openDatabase complete", "took", time.Since(dbStart))

	// Resolve log path from config
	cfg, err := config.Load()
	if err != nil {
		cfg = &config.Config{}
	}
	logPath := cfg.GetLogPath(serverPort)

	// Print startup banner
	printStartupBanner(verbosity, dbPath, logPath, cfg.Plugin.EnabledNames())

	// Create server with pre-created attestation store
	srvStart := time.Now()
	srv, err := server.NewQNTXServer(database, atsStore, dbPath, verbosity)
	if err != nil {
		return errors.Wrap(err, "failed to create server")
	}
	bootLog.Infow("NewQNTXServer complete", "took", time.Since(srvStart))

	// A backend that keeps watchers itself says so here; otherwise the engine
	// keeps them in the operational SQLite, which is what sqlite nodes want.
	if wp, ok := rustStore.(interface{ Watchers() storage.Watchers }); ok {
		srv.SetWatcherStore(wp.Watchers())
	}

	// system is a node itself. A backend that keeps a store for it says so
	// here, and a node's own records land in that store.
	if ss, ok := rustStore.(interface{ SystemStore() ats.AttestationStore }); ok {
		srv.SetSystemStore(ss.SystemStore())
	}

	// Namespaces are the top-level prefix at a storage location, which only a
	// backend that has prefixes can keep (ADR-026).
	if ns, ok := rustStore.(interface{ Namespaces() storage.Namespaces }); ok {
		srv.SetNamespaces(ns.Namespaces())
	}

	// A namespace created after boot is reached by opening its store on the
	// first request that names it, rather than by restarting the node.
	if no, ok := rustStore.(server.NamespaceOpener); ok {
		srv.SetNamespaceOpener(no)
	}

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

	// QNTX cannot run without the store holding passkeys, jobs, schedules and
	// canvas. Losing it ends the process.
	go srv.WatchOperationalStore(func(reason error) {
		select {
		case errChan <- reason:
		default:
		}
	})

	// GRACE: Wait for shutdown signal (Ctrl+C)
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	select {
	case err := <-errChan:
		// Either it never started or it stopped being able to run. Both end the
		// process, and "failed to start" would have lied about the second.
		server.WriteDeferredNews(cfg.GroundDBPath, "qntx", "crash",
			"qntx-server", fmt.Sprintf("QNTX stopped: %v", err), nil, bootLog)
		return errors.Wrap(err, "server stopped")
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
			// os.Exit runs no defers and main never gets its turn. Whatever the
			// node said on the way down is worth more than the moment it costs.
			logger.FlushSentry()
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
	// The operator is sitting at this terminal waiting for a browser. If none
	// is coming, that is the moment to say so and give them the URL.
	if err != nil {
		pterm.Warning.Printfln("Could not open a browser: %v", err)
		pterm.Warning.Printfln("Open %s yourself.", url)
	}
}
