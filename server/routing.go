package server

import "net/http"

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

	// Register plugin handlers with CORS middleware
	// Register handlers for all loaded plugins - mux handles internal routing
	corsPluginHandler := s.corsMiddleware(mux.ServeHTTP)
	if s.pluginRegistry != nil {
		for _, name := range s.pluginRegistry.List() {
			// Use wildcard pattern for Go 1.22+ ServeMux
			pattern := "/api/" + name + "/{path...}"
			http.HandleFunc(pattern, corsPluginHandler)
			s.logger.Infow("Registered HTTP route", "plugin", name, "pattern", pattern)
		}
	}

	// Auth endpoints (public, no auth required)
	if s.authHandlers != nil {
		http.HandleFunc("/auth/providers", s.corsMiddleware(s.authHandlers.HandleProviders))
		http.HandleFunc("/auth/oauth/", s.corsMiddleware(s.handleAuthOAuth))  // /auth/oauth/{provider}/url
		http.HandleFunc("/auth/callback", s.corsMiddleware(s.authHandlers.HandleCallback))
		http.HandleFunc("/auth/refresh", s.corsMiddleware(s.authHandlers.HandleRefresh))
		// Protected auth endpoints (require authentication)
		http.HandleFunc("/auth/logout", s.corsMiddleware(s.authMiddleware.RequireAuth(s.authHandlers.HandleLogout)))
		http.HandleFunc("/auth/sessions", s.corsMiddleware(s.authMiddleware.RequireAuth(s.handleAuthSessions)))
		http.HandleFunc("/auth/me", s.corsMiddleware(s.authMiddleware.RequireAuth(s.authHandlers.HandleMe)))
		s.logger.Infow("Auth routes registered")
	}

	// Core QNTX handlers
	// WebSocket endpoints - auth optional, validated at upgrade time
	http.HandleFunc("/ws", s.corsMiddleware(s.HandleWebSocket))      // Custom WebSocket protocol (graph updates, logs, etc.)
	http.HandleFunc("/lsp", s.corsMiddleware(s.HandleGLSPWebSocket)) // ATS LSP protocol (completions, hover, semantic tokens)

	// Public endpoints
	http.HandleFunc("/health", s.corsMiddleware(s.HandleHealth))
	http.HandleFunc("/logs/download", s.corsMiddleware(s.HandleLogDownload))
	http.HandleFunc("/api/timeseries/usage", s.corsMiddleware(s.HandleUsageTimeSeries))
	http.HandleFunc("/api/config", s.corsMiddleware(s.HandleConfig))
	http.HandleFunc("/api/dev", s.corsMiddleware(s.HandleDevMode))                       // Dev mode status
	http.HandleFunc("/api/debug", s.corsMiddleware(s.HandleDebug))                       // Browser console debugging (dev mode only)
	http.HandleFunc("/api/prose", s.corsMiddleware(s.HandleProse))                       // Prose content tree
	http.HandleFunc("/api/prose/", s.corsMiddleware(s.HandleProseContent))               // Individual prose files
	http.HandleFunc("/api/pulse/executions/", s.corsMiddleware(s.HandlePulseExecution))  // Individual execution (GET) and logs (GET /logs)
	http.HandleFunc("/api/pulse/schedules/", s.corsMiddleware(s.HandlePulseSchedule))    // Individual schedule (GET/PATCH/DELETE)
	http.HandleFunc("/api/pulse/schedules", s.corsMiddleware(s.HandlePulseSchedules))    // List/create schedules (GET/POST)
	http.HandleFunc("/api/pulse/jobs/", s.corsMiddleware(s.HandlePulseJob))                 // Individual async job and sub-resources (GET)
	http.HandleFunc("/api/pulse/jobs", s.corsMiddleware(s.HandlePulseJobs))                 // List async jobs (GET)
	http.HandleFunc("/api/plugins/{name}/config", s.corsMiddleware(s.HandlePluginConfig))   // Plugin configuration (GET/PUT)
	http.HandleFunc("/api/plugins/", s.corsMiddleware(s.HandlePluginAction))                // Plugin actions: pause/resume (POST)
	http.HandleFunc("/api/plugins", s.corsMiddleware(s.HandlePlugins))                      // List installed plugins (GET)
	http.HandleFunc("/", s.corsMiddleware(s.HandleStatic))
}

// handleAuthOAuth routes OAuth provider requests
func (s *QNTXServer) handleAuthOAuth(w http.ResponseWriter, r *http.Request) {
	// Route to auth URL handler for /auth/oauth/{provider}/url
	s.authHandlers.HandleAuthURL(w, r)
}

// handleAuthSessions routes session list and revoke requests
func (s *QNTXServer) handleAuthSessions(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		s.authHandlers.HandleSessions(w, r)
	} else if r.Method == http.MethodDelete {
		s.authHandlers.HandleRevokeSession(w, r)
	} else {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
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
