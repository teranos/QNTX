package server

import (
	"context"
	"fmt"
	"net/http"
	_ "net/http/pprof"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/teranos/QNTX/errors"
	"github.com/teranos/QNTX/internal/logger"
	"github.com/teranos/QNTX/internal/version"
)

func init() {
	runtime.SetMutexProfileFraction(5)
	runtime.SetBlockProfileRate(1000)
}

// Opening/Closing Phase 4: Server state management

// getState returns the current server state
func (s *QNTXServer) getState() ServerState {
	return ServerState(s.state.Load())
}

// setState atomically updates the server state
func (s *QNTXServer) setState(newState ServerState) {
	s.state.Store(int32(newState))
	s.logger.Infow("Server state changed", "new_state", stateString(newState))
}

// stateString returns human-readable state name
func stateString(state ServerState) string {
	switch state {
	case ServerStateRunning:
		return "running"
	case ServerStateDraining:
		return "draining"
	case ServerStateStopped:
		return "stopped"
	default:
		return "unknown"
	}
}

// startBackgroundServices starts all background service goroutines
func (s *QNTXServer) startBackgroundServices() {
	// Start daemon based on saved state
	if s.daemon != nil {
		enabled, err := s.getDaemonState()
		if err != nil {
			s.logger.Warnw("Failed to read daemon state, defaulting to disabled", "error", err)
			enabled = false
		}

		if enabled {
			s.daemon.Start()
			if s.ticker != nil {
				s.ticker.Start()
				logger.AddPulseSymbol(s.logger).Debugw("Pulse ticker started (from saved state)")
			}
			s.logger.Debugw("Daemon started (from saved state)", "workers", s.daemon.Workers())
		} else {
			s.logger.Infow("Daemon not started (disabled in saved state)")
		}
	}

	// Start auth session sweep (if auth is enabled)
	if s.authHandler != nil {
		s.wg.Add(1)
		s.authHandler.StartSessionSweep(s.wg.Done, s.ctx.Done())
	}

	// Start rate limiter sweep goroutine
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		s.sweepRateLimiters(s.ctx)
	}()

	// Broadcast worker is started in Run() method
	// Start usage update broadcaster
	s.startUsageUpdateTicker()

	// Start job update broadcaster (if daemon is available)
	if s.daemon != nil {
		s.startJobUpdateBroadcaster()
	}

	// Start daemon status broadcaster (if daemon is available)
	if s.daemon != nil {
		s.startDaemonStatusBroadcaster()
	}

	// Start Pulse execution completion poller (if ticker is available)
	if s.ticker != nil {
		s.startPulseExecutionPoller()
	}

	// Start watcher queue status broadcaster
	if s.watcherEngine != nil {
		s.startWatcherQueueBroadcaster()
	}

	// Start database stats cache refresher
	s.startDBStatsRefresher()
}

// Start starts the server on the specified port
func (s *QNTXServer) Start(port int, openBrowserFunc func(url string)) error {
	// Start the hub in a goroutine
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		s.Run()
	}()

	// Start all background services
	s.startBackgroundServices()

	// Find an available port
	actualPort, err := findAvailablePort(port)
	if err != nil {
		return errors.Wrapf(err, "failed to find available port starting from %d", port)
	}

	if actualPort != port {
		s.logger.Infow("Port in use, using alternative",
			"requested_port", port,
			"actual_port", actualPort,
		)
	}

	// Set up HTTP routes
	s.setupHTTPRoutes()

	url := fmt.Sprintf("http://localhost:%d", actualPort)
	s.logger.Infow("Server ready",
		"url", url,
		"port", actualPort,
	)

	// Attest startup via ground delivery
	s.emitLifecycleNews("started", actualPort)

	// Open browser if callback provided
	if openBrowserFunc != nil {
		s.logger.Infow("Opening browser", "url", url)
		openBrowserFunc(url)
		s.logger.Infow("Browser launch triggered (async)")

		// Detect slow browser connection
		s.wg.Add(1)
		go func() {
			defer s.wg.Done()
			s.monitorBrowserConnection()
		}()
	}

	// Signal that the server is fully ready — plugins can now initialize.
	if s.onReady != nil {
		go s.onReady()
	}

	s.httpServer = &http.Server{
		Addr:              fmt.Sprintf("%s:%d", s.bindAddress, actualPort),
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       120 * time.Second,
		// ReadTimeout and WriteTimeout must be 0 — non-zero values kill
		// long-lived WebSocket connections (graph, sync).
	}
	s.logger.Infow(fmt.Sprintf("HTTP server listening on %s:%d", s.bindAddress, actualPort))
	return s.httpServer.ListenAndServe()
}

// monitorBrowserConnection warns if no clients connect within 5 seconds
func (s *QNTXServer) monitorBrowserConnection() {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	select {
	case <-ticker.C:
		s.mu.Lock()
		clientCount := len(s.clients)
		s.mu.Unlock()

		if clientCount == 0 {
			s.logger.Warnw("No browser connected after 5 seconds",
				"elapsed_seconds", 5,
				"hint", "Browser may be delayed by extensions, previous pages, or system settings",
			)
		}
	case <-s.ctx.Done():
		return
	}
}

// Stop gracefully shuts down the server and cleans up resources
func (s *QNTXServer) Stop() error {
	s.logger.Infow("Initiating server shutdown")

	// Attest shutdown immediately — before any teardown that might get interrupted
	s.emitLifecycleNews("stopped", 0)

	// Opening/Closing Phase 4: Transition to draining state
	s.setState(ServerStateDraining)

	// Stop daemon FIRST before stopping server goroutines
	if s.daemon != nil {
		s.daemon.Stop()
	}

	// Stop watcher engine — drain loop stops, in-flight entries re-queued for next startup
	if s.watcherEngine != nil {
		s.watcherEngine.Stop()
	}
	if s.watcherDB != nil {
		s.watcherDB.Close()
	}
	if s.embeddingsHandler != nil && s.embeddingsHandler.ReadDB != nil {
		s.embeddingsHandler.ReadDB.Close()
	}

	// Clear service providers before killing plugins — observers check HasProvider()
	// and will skip routing once providers are cleared.
	if s.servicesManager != nil {
		if searchRouter := s.servicesManager.GetSearchRouter(); searchRouter != nil {
			searchRouter.ClearProviders()
		}
		if llmRouter := s.servicesManager.GetLLMRouter(); llmRouter != nil {
			llmRouter.ClearProviders()
		}
	}

	// Shutdown plugins and gRPC services
	if s.pluginRegistry != nil {
		if err := s.pluginRegistry.ShutdownAll(s.ctx); err != nil {
			s.logger.Warnw("Plugin shutdown errors", "error", err)
		}
	}
	if s.servicesManager != nil {
		s.servicesManager.Shutdown()
	}
	s.logger.Debugw("Plugins shut down")

	// Gracefully shut down the HTTP server — stops accepting new connections
	// while allowing in-flight requests to complete.
	if s.httpServer != nil {
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer shutdownCancel()
		if err := s.httpServer.Shutdown(shutdownCtx); err != nil {
			s.logger.Warnw("HTTP server shutdown error", "error", err)
		}
	}

	// Close all client connections BEFORE cancelling context
	// This ensures readPump/writePump exit cleanly before context cancellation
	s.mu.Lock()
	clientsToClose := make([]*Client, 0, len(s.clients))
	for client := range s.clients {
		clientsToClose = append(clientsToClose, client)
		delete(s.clients, client)
	}
	s.mu.Unlock()

	if len(clientsToClose) > 0 {
		s.logger.Infow("Closing client connections", "count", len(clientsToClose))
		for _, client := range clientsToClose {
			client.conn.Close() // Close connection to unblock readPump
		}
	}

	// Cancel context to signal all server goroutines to stop
	if s.cancel != nil {
		s.cancel()
	}

	// Wait for goroutines with timeout
	// Goroutines should exit quickly now that:
	// 1. WebSocket connections are closed (unblocking readPump)
	// 2. Context is cancelled (stopping writePump and broadcasters)
	done := make(chan struct{})
	go func() {
		s.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		s.logger.Infow("All goroutines stopped cleanly")
	case <-time.After(ShutdownTimeout):
		s.logger.Warnw("Goroutine shutdown timed out, forcing exit",
			"timeout", ShutdownTimeout,
		)
	}

	// Stop config watcher
	if s.configWatcher != nil {
		if err := s.configWatcher.Stop(); err != nil {
			s.logger.Warnw("Failed to stop config watcher", "error", err)
		} else {
			s.logger.Infow("Config watcher stopped")
		}
	}

	// Opening/Closing Phase 4: Mark shutdown complete
	s.setState(ServerStateStopped)

	s.logger.Infow("Server shutdown complete",
		"broadcast_drops", s.broadcastDrops.Load(),
	)

	return nil
}

// emitLifecycleNews writes an immediate news attestation to ground for startup/shutdown.
func (s *QNTXServer) emitLifecycleNews(event string, port int) {
	v := version.Get()

	// Collect plugin names — at startup these are enabled (not yet initialized),
	// at shutdown these are the plugins that were registered during the session.
	var plugins []string
	if s.pluginRegistry != nil {
		plugins = s.pluginRegistry.List()
	}

	ts := time.Now().Format("15:04:05")
	detail := fmt.Sprintf("QNTX %s (%s) %s at %s", v.Version, v.Short(), event, ts)
	if port > 0 {
		detail += fmt.Sprintf(" on port %d", port)
	}
	if len(plugins) > 0 {
		if event == "started" {
			detail += fmt.Sprintf(" plugins enabled: %s", strings.Join(plugins, ", "))
		} else {
			detail += fmt.Sprintf(" with %s", strings.Join(plugins, ", "))
		}
	}

	attrs := map[string]interface{}{
		"event":      event,
		"version":    v.Version,
		"commit":     v.CommitHash,
		"build_time": v.BuildTime,
		"log_path":   s.logPath,
		"db_path":    s.dbPath,
	}
	if port > 0 {
		attrs["port"] = port
		attrs["url"] = fmt.Sprintf("http://localhost:%d", port)
	}
	if len(plugins) > 0 {
		attrs["plugins"] = plugins
		logDir := filepath.Dir(s.logPath)
		pluginLogs := make(map[string]string, len(plugins))
		for _, name := range plugins {
			pluginLogs[name] = filepath.Join(logDir, name+".log")
		}
		attrs["plugin_logs"] = pluginLogs
	}

	WriteImmediateNews(s.groundDBPath, "qntx", "lifecycle", "qntx-server", detail, attrs, s.logger)
}
