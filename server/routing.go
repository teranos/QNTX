package server

import (
	"fmt"
	"net/http"
	"strings"
)

// setupHTTPRoutes configures all HTTP handlers
func (s *QNTXServer) setupHTTPRoutes() {
	// Create a custom ServeMux for plugins
	mux := http.NewServeMux()

	// Register domain plugin handlers
	if s.pluginRegistry != nil {
		for _, name := range s.pluginRegistry.List() {
			plugin, ok := s.pluginRegistry.Get(name)
			if !ok {
				continue
			}

			// Register HTTP handlers
			if err := plugin.RegisterHTTP(mux); err != nil {
				s.logger.Errorw("Failed to register HTTP handlers for plugin",
					"plugin", name,
					"error", err)
			}

			// Register WebSocket handlers
			wsHandlers, err := plugin.RegisterWebSocket()
			if err != nil {
				s.logger.Errorw("Failed to register WebSocket handlers for plugin",
					"plugin", name,
					"error", err)
			} else {
				// Register each WebSocket handler
				for path, handler := range wsHandlers {
					// Capture handler in local variable for closure
					wsHandler := handler
					http.HandleFunc(path, s.corsMiddleware(wsHandler.ServeWS))
					s.logger.Infow("Registered WebSocket handler", "plugin", name, "path", path)
				}
			}
		}
	}

	// Register plugin handlers with CORS and readiness middleware
	// Register routes for ALL enabled plugins (loading async), not just loaded ones
	corsPluginHandler := s.corsMiddleware(mux.ServeHTTP)
	readyPluginHandler := s.pluginReadinessMiddleware(corsPluginHandler)
	if s.pluginRegistry != nil {
		for _, name := range s.pluginRegistry.ListEnabled() {
			// Register exact match for /api/{plugin} (e.g., /api/code)
			exactPattern := "/api/" + name
			http.HandleFunc(exactPattern, readyPluginHandler)

			// Register wildcard for /api/{plugin}/* (e.g., /api/code/file.go)
			wildcardPattern := "/api/" + name + "/{path...}"
			http.HandleFunc(wildcardPattern, readyPluginHandler)

			s.logger.Infow("Registered HTTP routes", "plugin", name,
				"exact", exactPattern,
				"wildcard", wildcardPattern)
		}
	}

	// Core QNTX handlers
	http.HandleFunc("/ws", s.corsMiddleware(s.HandleWebSocket))      // Custom WebSocket protocol (graph updates, logs, etc.)
	http.HandleFunc("/lsp", s.corsMiddleware(s.HandleGLSPWebSocket)) // ATS LSP protocol (completions, hover, semantic tokens)
	http.HandleFunc("/health", s.corsMiddleware(s.HandleHealth))
	http.HandleFunc("/logs/download", s.corsMiddleware(s.HandleLogDownload))
	http.HandleFunc("/api/timeseries/usage", s.corsMiddleware(s.HandleUsageTimeSeries))
	http.HandleFunc("/api/config", s.corsMiddleware(s.HandleConfig))
	http.HandleFunc("/api/dev", s.corsMiddleware(s.HandleDevMode))                        // Dev mode status
	http.HandleFunc("/api/debug", s.corsMiddleware(s.HandleDebug))                        // Browser console debugging (dev mode only)
	http.HandleFunc("/api/prose", s.corsMiddleware(s.HandleProse))                        // Prose content tree
	http.HandleFunc("/api/prose/", s.corsMiddleware(s.HandleProseContent))                // Individual prose files
	http.HandleFunc("/api/pulse/executions/", s.corsMiddleware(s.HandlePulseExecution))   // Individual execution (GET) and logs (GET /logs)
	http.HandleFunc("/api/pulse/schedules/", s.corsMiddleware(s.HandlePulseSchedule))     // Individual schedule (GET/PATCH/DELETE)
	http.HandleFunc("/api/pulse/schedules", s.corsMiddleware(s.HandlePulseSchedules))     // List/create schedules (GET/POST)
	http.HandleFunc("/api/pulse/jobs/", s.corsMiddleware(s.HandlePulseJob))               // Individual async job and sub-resources (GET)
	http.HandleFunc("/api/pulse/jobs", s.corsMiddleware(s.HandlePulseJobs))               // List async jobs (GET)
	http.HandleFunc("/api/plugins/{name}/config", s.corsMiddleware(s.HandlePluginConfig)) // Plugin configuration (GET/PUT)
	http.HandleFunc("/api/plugins/", s.corsMiddleware(s.HandlePluginAction))              // Plugin actions: pause/resume (POST)
	http.HandleFunc("/api/plugins", s.corsMiddleware(s.HandlePlugins))                    // List installed plugins (GET)
	http.HandleFunc("/api/prompt/", s.corsMiddleware(s.HandlePrompt))                     // Prompt editor endpoints (preview/execute)
	http.HandleFunc("/", s.corsMiddleware(s.HandleStatic))
}

// corsMiddleware adds CORS headers to HTTP responses using configured allowed origins
// Uses the same origin validation as WebSocket connections (server.allowed_origins config)
func (s *QNTXServer) corsMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")

		// If origin is present and allowed by config, set CORS headers
		if origin != "" && checkOrigin(r) {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Access-Control-Allow-Credentials", "true")
		}

		// Configure allowed methods based on environment
		if s.isDevMode() {
			// Dev mode: Allow all methods for rapid development
			w.Header().Set("Access-Control-Allow-Methods", "*")
			w.Header().Set("Access-Control-Allow-Headers", "*")
		} else {
			// Production: Restrict to explicitly required methods
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PATCH, DELETE, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
		}

		// Handle preflight requests
		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}

		next(w, r)
	}
}

// pluginReadinessMiddleware checks if plugin is ready before forwarding request
// Returns 503 Service Unavailable if plugin is still loading
func (s *QNTXServer) pluginReadinessMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Extract plugin name from /api/{plugin}/* path
		path := r.URL.Path
		if len(path) < 6 || path[:5] != "/api/" {
			// Not a plugin route, pass through
			next(w, r)
			return
		}

		// Extract plugin name (between /api/ and next /)
		remaining := path[5:]
		pluginName := remaining
		if idx := strings.Index(remaining, "/"); idx != -1 {
			pluginName = remaining[:idx]
		}

		// Check if plugin is ready
		if s.pluginRegistry != nil && !s.pluginRegistry.IsReady(pluginName) {
			w.Header().Set("Retry-After", "5")
			http.Error(w, fmt.Sprintf("Plugin '%s' is still loading, please retry", pluginName), http.StatusServiceUnavailable)
			return
		}

		next(w, r)
	}
}
