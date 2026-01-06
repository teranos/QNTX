package server

import (
	"fmt"
	"net/http"
	"time"

	appcfg "github.com/teranos/QNTX/am"
	"github.com/teranos/QNTX/errors"
	"github.com/teranos/QNTX/sym"
)

// GRACE Phase 4: Server state management

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
			s.logger.Warnw("Failed to read daemon state, defaulting to enabled", "error", err)
			enabled = true
		}

		if enabled {
			s.daemon.Start()
			if s.ticker != nil {
				s.ticker.Start()
				s.logger.Infow(fmt.Sprintf("%s Pulse ticker started (from saved state)", sym.Pulse))
			}
			s.logger.Infow("Daemon started (from saved state)", "workers", s.daemon.Workers())
		} else {
			s.logger.Infow("Daemon not started (disabled in saved state)")
		}
	}

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
		return errors.Wrap(err, "failed to find available port")
	}

	if actualPort != port {
		s.logger.Infow("Port in use, using alternative",
			"requested_port", port,
			"actual_port", actualPort,
		)
	}

	// Set up HTTP routes
	s.setupHTTPRoutes()

	// Check if TLS is enabled
	cfg, _ := appcfg.Load()
	useTLS := cfg != nil && cfg.Auth.TLS.Enabled && cfg.Auth.TLS.CertFile != "" && cfg.Auth.TLS.KeyFile != ""

	protocol := "http"
	if useTLS {
		protocol = "https"
	}

	url := fmt.Sprintf("%s://localhost:%d", protocol, actualPort)
	s.logger.Infow("Server ready",
		"url", url,
		"port", actualPort,
		"tls", useTLS,
	)

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

	addr := fmt.Sprintf(":%d", actualPort)

	if useTLS {
		s.logger.Infow(fmt.Sprintf("HTTPS server listening on port %d (TLS enabled)", actualPort),
			"cert_file", cfg.Auth.TLS.CertFile,
		)
		return http.ListenAndServeTLS(addr, cfg.Auth.TLS.CertFile, cfg.Auth.TLS.KeyFile, nil)
	}

	s.logger.Infow(fmt.Sprintf("HTTP server listening on port %d", actualPort))
	return http.ListenAndServe(addr, nil)
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
			s.logger.Warnw("Browser slow to connect",
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

	// GRACE Phase 4: Transition to draining state
	s.setState(ServerStateDraining)

	// Stop daemon FIRST before stopping server goroutines
	if s.daemon != nil {
		s.logger.Infow("Stopping daemon workers")
		s.daemon.Stop()
		s.logger.Infow("Daemon stopped")
	}

	// Shutdown plugins before closing clients
	if s.pluginRegistry != nil {
		s.logger.Infow("Shutting down domain plugins")
		if err := s.pluginRegistry.ShutdownAll(s.ctx); err != nil {
			s.logger.Warnw("Plugin shutdown errors", "error", err)
		} else {
			s.logger.Infow("Domain plugins shut down")
		}
	}

	// Shutdown gRPC services for plugins (Issue #138)
	if s.servicesManager != nil {
		s.logger.Infow("Shutting down plugin services")
		s.servicesManager.Shutdown()
		s.logger.Infow("Plugin services shut down")
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

	// GRACE Phase 4: Mark shutdown complete
	s.setState(ServerStateStopped)

	s.logger.Infow("Server shutdown complete",
		"broadcast_drops", s.broadcastDrops.Load(),
	)

	return nil
}
