package server

import (
	"fmt"
	"net/http"
	"strings"
	"sync"
)

// setupHTTPRoutes configures all HTTP handlers
func (s *QNTXServer) setupHTTPRoutes() {
	// wrap applies CORS + auth middleware. When auth is disabled, identical to corsMiddleware.
	// Resolved at registration time — zero per-request branching.
	wrap := s.corsMiddleware
	if s.authEnabled {
		inner := s.authHandler.Middleware
		wrap = func(handler http.HandlerFunc) http.HandlerFunc {
			return s.corsMiddleware(inner(handler))
		}
		// Register auth routes (CORS only, no auth middleware)
		s.authHandler.RegisterRoutes()
	}

	// Node DID document (public, no auth)
	http.HandleFunc("/.well-known/did.json", s.corsMiddleware(s.nodeDID.HandleDIDDocument))

	// Register plugin routes with dynamic handler that waits for plugins to load
	// This allows routes to be registered immediately while plugins load asynchronously
	if s.pluginRegistry != nil {
		pluginHandler := wrap(s.handlePluginRequest)
		for _, name := range s.pluginRegistry.ListEnabled() {
			// Register exact match for /api/{plugin} (e.g., /api/code)
			exactPattern := "/api/" + name
			http.HandleFunc(exactPattern, pluginHandler)

			// Register wildcard for /api/{plugin}/* (e.g., /api/code/file.go)
			wildcardPattern := "/api/" + name + "/{path...}"
			http.HandleFunc(wildcardPattern, pluginHandler)

			s.logger.Infow("Registered HTTP routes", "plugin", name,
				"exact", exactPattern,
				"wildcard", wildcardPattern)
		}
	}

	// Register WebSocket handlers for loaded plugins
	// These are registered separately as they don't go through the same routing
	if s.pluginRegistry != nil {
		for _, name := range s.pluginRegistry.List() {
			plugin, ok := s.pluginRegistry.Get(name)
			if !ok {
				continue
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
					http.HandleFunc(path, wrap(wsHandler.ServeWS))
					s.logger.Infow("Registered WebSocket handler", "plugin", name, "path", path)
				}
			}
		}
	}

	// Core QNTX handlers
	http.HandleFunc("/ws", wrap(s.HandleWebSocket))              // Custom WebSocket protocol (graph updates, logs, etc.)
	http.HandleFunc("/lsp", wrap(s.HandleGLSPWebSocket))         // ATS LSP protocol (completions, hover, semantic tokens)
	http.HandleFunc("/health", s.corsMiddleware(s.HandleHealth)) // Health check always public
	http.HandleFunc("/logs/download", wrap(s.HandleLogDownload))
	http.HandleFunc("/api/timeseries/usage", wrap(s.HandleUsageTimeSeries))
	http.HandleFunc("/api/config", wrap(s.HandleConfig))
	http.HandleFunc("/api/dev", wrap(s.HandleDevMode))                                              // Dev mode status
	http.HandleFunc("/api/debug", wrap(s.HandleDebug))                                              // Browser console debugging (dev mode only)
	http.HandleFunc("/api/prose", wrap(s.HandleProse))                                              // Prose content tree
	http.HandleFunc("/api/prose/", wrap(s.HandleProseContent))                                      // Individual prose files
	http.HandleFunc("/api/pulse/executions/", wrap(s.HandlePulseExecution))                         // Individual execution (GET) and logs (GET /logs)
	http.HandleFunc("/api/pulse/schedules/", wrap(s.HandlePulseSchedule))                           // Individual schedule (GET/PATCH/DELETE)
	http.HandleFunc("/api/pulse/schedules", wrap(s.HandlePulseSchedules))                           // List/create schedules (GET/POST)
	http.HandleFunc("/api/pulse/jobs/", wrap(s.HandlePulseJob))                                     // Individual async job and sub-resources (GET)
	http.HandleFunc("/api/pulse/jobs", wrap(s.HandlePulseJobs))                                     // List async jobs (GET)
	http.HandleFunc("/api/prompt/", wrap(s.HandlePrompt))                                           // Prompt operations (preview/execute/list/save/get/versions)
	http.HandleFunc("/api/plugins/{name}/config", wrap(s.HandlePluginConfig))                       // Plugin configuration (GET/PUT)
	http.HandleFunc("/api/plugins/", wrap(s.HandlePluginAction))                                    // Plugin actions: pause/resume (POST)
	http.HandleFunc("/api/plugins", wrap(s.HandlePlugins))                                          // List installed plugins (GET)
	http.HandleFunc("/api/types/", wrap(s.HandleTypes))                                             // Get specific type (GET /api/types/{typename})
	http.HandleFunc("/api/types", wrap(s.HandleTypes))                                              // List/create types (GET/POST)
	http.HandleFunc("/api/watchers/", wrap(s.HandleWatchers))                                       // Watcher CRUD (GET/PUT/DELETE /api/watchers/{id})
	http.HandleFunc("/api/watchers", wrap(s.HandleWatchers))                                        // List/create watchers (GET/POST)
	http.HandleFunc("/api/attestations", wrap(s.HandleCreateAttestation))                           // Sync browser-created attestations (POST)
	http.HandleFunc("/api/canvas/glyphs/", wrap(s.canvasHandler.HandleGlyphs))                      // Glyph CRUD (GET/POST/DELETE /api/canvas/glyphs/{id})
	http.HandleFunc("/api/canvas/glyphs", wrap(s.canvasHandler.HandleGlyphs))                       // List/create glyphs (GET/POST)
	http.HandleFunc("/api/canvas/compositions/", wrap(s.canvasHandler.HandleCompositions))          // Composition CRUD (GET/POST/DELETE /api/canvas/compositions/{id})
	http.HandleFunc("/api/canvas/compositions", wrap(s.canvasHandler.HandleCompositions))           // List/create compositions (GET/POST)
	http.HandleFunc("/api/canvas/minimized-windows/", wrap(s.canvasHandler.HandleMinimizedWindows)) // Minimized window CRUD (DELETE /api/canvas/minimized-windows/{id})
	http.HandleFunc("/api/canvas/minimized-windows", wrap(s.canvasHandler.HandleMinimizedWindows))  // List/add minimized windows (GET/POST)
	http.HandleFunc("/api/files/", wrap(s.HandleFiles))                                             // Serve stored file (GET /api/files/{id})
	http.HandleFunc("/api/files", wrap(s.HandleFiles))                                              // Upload file (POST)
	http.HandleFunc("/api/search/semantic", wrap(s.HandleSemanticSearch))                           // Semantic search (GET)
	http.HandleFunc("/api/embeddings/generate", wrap(s.HandleEmbeddingGenerate))                    // Generate embedding (POST)
	http.HandleFunc("/api/embeddings/batch", wrap(s.HandleEmbeddingBatch))                          // Batch generate embeddings (POST)
	http.HandleFunc("/api/embeddings/clusters", wrap(s.HandleEmbeddingClusters))                    // List stable clusters (GET)
	http.HandleFunc("/api/embeddings/cluster-timeline", wrap(s.HandleClusterTimeline))              // Cluster evolution timeline (GET)
	http.HandleFunc("/api/embeddings/cluster", wrap(s.HandleEmbeddingCluster))                      // HDBSCAN clustering (POST)
	http.HandleFunc("/api/embeddings/info", wrap(s.HandleEmbeddingInfo))                            // Embedding service status (GET)
	http.HandleFunc("/api/embeddings/project", wrap(s.HandleEmbeddingProject))                      // UMAP projection (POST)
	http.HandleFunc("/api/embeddings/projections", wrap(s.HandleEmbeddingProjections))              // Get 2D projections (GET)
	http.HandleFunc("/ws/sync", wrap(s.HandleSyncWebSocket))                                        // Sync peer WebSocket (incoming reconciliation)
	http.HandleFunc("/api/sync/status", wrap(s.HandleSyncStatus))                                   // Sync tree status (GET)
	http.HandleFunc("/api/sync", wrap(s.HandleSync))                                                // Initiate sync with peer (POST)
	http.HandleFunc("/", wrap(s.HandleStatic))
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

		// Explicit methods and headers required — wildcard (*) is forbidden
		// when credentials: 'include' is used (cross-origin with cookies).
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PATCH, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")

		// Handle preflight requests
		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}

		next(w, r)
	}
}

// handlePluginRequest dynamically routes requests to plugin handlers
// This enables async plugin loading - routes are registered immediately,
// but plugin muxes are initialized lazily when plugins finish loading
func (s *QNTXServer) handlePluginRequest(w http.ResponseWriter, r *http.Request) {
	// Extract plugin name from /api/{plugin}/* path
	path := r.URL.Path
	if len(path) < 6 || path[:5] != "/api/" {
		http.Error(w, "Invalid plugin route", http.StatusBadRequest)
		return
	}

	remaining := path[5:]
	pluginName := remaining
	if idx := strings.Index(remaining, "/"); idx != -1 {
		pluginName = remaining[:idx]
	}

	// Check if plugin is ready
	if s.pluginRegistry == nil || !s.pluginRegistry.IsReady(pluginName) {
		w.Header().Set("Retry-After", "5")
		http.Error(w, fmt.Sprintf("Plugin '%s' is still loading, please retry", pluginName), http.StatusServiceUnavailable)
		return
	}

	// Lazy-initialize plugin mux on first request (after plugin loads)
	// Use sync.Once per plugin to ensure thread-safe one-time initialization
	onceVal, _ := s.pluginMuxInit.LoadOrStore(pluginName, &sync.Once{})
	once := onceVal.(*sync.Once)

	// All concurrent requests will block here until initialization completes
	var initErr error
	once.Do(func() {
		plugin, ok := s.pluginRegistry.Get(pluginName)
		if !ok {
			initErr = fmt.Errorf("plugin '%s' not found", pluginName)
			return
		}

		// Initialize plugin with services (calls gRPC Init which populates plugin's httpMux)
		if err := plugin.Initialize(r.Context(), s.services); err != nil {
			s.logger.Errorw("Failed to initialize plugin",
				"plugin", pluginName,
				"error", err)
			initErr = err
			return
		}

		mux := http.NewServeMux()
		if err := plugin.RegisterHTTP(mux); err != nil {
			s.logger.Errorw("Failed to register HTTP handlers for plugin",
				"plugin", pluginName,
				"error", err)
			initErr = err
			return
		}

		s.pluginMuxes.Store(pluginName, mux)
		s.logger.Infow("Initialized HTTP handlers for plugin", "plugin", pluginName)
	})

	// Check if initialization failed
	if initErr != nil {
		http.Error(w, fmt.Sprintf("Plugin '%s' initialization failed: %v", pluginName, initErr), http.StatusInternalServerError)
		return
	}

	// Load the initialized mux
	muxVal, muxExists := s.pluginMuxes.Load(pluginName)
	if !muxExists {
		// Should never happen after sync.Once completes successfully
		http.Error(w, fmt.Sprintf("Plugin '%s' mux not found after initialization", pluginName), http.StatusInternalServerError)
		return
	}

	// Serve request through plugin's mux
	// Try with stripped prefix first (e.g., /api/code/health -> /health)
	// If that 404s, try with full path (backward compatibility)
	// This allows plugins to register routes either way (Issue #277)
	mux := muxVal.(*http.ServeMux)

	// Strip /api/{plugin} prefix
	strippedPath := strings.TrimPrefix(path, "/api/"+pluginName)
	if strippedPath == "" {
		strippedPath = "/"
	}

	// Try stripped path first (modern approach)
	recorder := &responseRecorder{ResponseWriter: w, statusCode: http.StatusOK}
	newReq := r.Clone(r.Context())
	newReq.URL.Path = strippedPath
	newReq.RequestURI = strippedPath
	mux.ServeHTTP(recorder, newReq)

	// If 404, try full path (backward compat for plugins that include prefix)
	if recorder.statusCode == http.StatusNotFound {
		s.logger.Debugw("Stripped path 404, trying full path",
			"plugin", pluginName,
			"stripped", strippedPath,
			"full", path)
		mux.ServeHTTP(w, r)
		return
	}

	// Write buffered response
	recorder.flush()
}

// responseRecorder captures response to detect 404s
type responseRecorder struct {
	http.ResponseWriter
	statusCode  int
	body        []byte
	wroteHeader bool
}

func (rr *responseRecorder) Header() http.Header {
	return rr.ResponseWriter.Header()
}

func (rr *responseRecorder) WriteHeader(code int) {
	if !rr.wroteHeader {
		rr.statusCode = code
		rr.wroteHeader = true
	}
}

func (rr *responseRecorder) Write(b []byte) (int, error) {
	if !rr.wroteHeader {
		rr.WriteHeader(http.StatusOK)
	}
	rr.body = append(rr.body, b...)
	return len(b), nil
}

func (rr *responseRecorder) flush() {
	if rr.wroteHeader {
		rr.ResponseWriter.WriteHeader(rr.statusCode)
	}
	if len(rr.body) > 0 {
		rr.ResponseWriter.Write(rr.body)
	}
}
