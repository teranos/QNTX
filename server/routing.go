package server

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

// setupHTTPRoutes configures all HTTP handlers
func (s *QNTXServer) setupHTTPRoutes() {
	// wrap applies CORS + rate limit + auth middleware for API routes.
	// When auth is disabled, rate limiting still applies.
	// Chain: cors → rateLimit(read/write) → auth → handler
	wrap := func(handler http.HandlerFunc) http.HandlerFunc {
		return s.corsMiddleware(s.rateLimitMiddleware(handler))
	}
	if s.authEnabled {
		inner := s.authHandler.Middleware
		wrap = func(handler http.HandlerFunc) http.HandlerFunc {
			return s.corsMiddleware(s.rateLimitMiddleware(inner(handler)))
		}
		// Register auth routes (rate limited via authCorsWrap composed in init.go)
		s.authHandler.RegisterRoutes()
	}

	// wrapWS applies CORS + WS rate limit + auth for WebSocket upgrades.
	wrapWS := func(handler http.HandlerFunc) http.HandlerFunc {
		return s.corsMiddleware(s.rateLimitWSMiddleware(handler))
	}
	if s.authEnabled {
		inner := s.authHandler.Middleware
		wrapWS = func(handler http.HandlerFunc) http.HandlerFunc {
			return s.corsMiddleware(s.rateLimitWSMiddleware(inner(handler)))
		}
	}

	// wrapPublic applies public rate limit + CORS (no auth).
	wrapPublic := func(handler http.HandlerFunc) http.HandlerFunc {
		return s.rateLimitPublicMiddleware(s.corsMiddleware(handler))
	}

	// Node DID document (public, no auth)
	http.HandleFunc("/.well-known/did.json", wrapPublic(s.nodeDID.HandleDIDDocument))

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

	// Register WebSocket routes for plugins (same lazy pattern as HTTP routes above).
	// Plugins load asynchronously, so we register /ws/<name> from pre-registered names
	// and resolve the actual handler when the connection arrives.
	if s.pluginRegistry != nil {
		wsHandler := wrapWS(s.handlePluginWebSocket)
		for _, name := range s.pluginRegistry.ListEnabled() {
			pattern := "/ws/" + name
			http.HandleFunc(pattern, wsHandler)
			s.logger.Infow("Registered WebSocket route", "plugin", name, "path", pattern)
		}
	}

	// Core QNTX handlers
	http.HandleFunc("/ws", wrapWS(s.HandleWebSocket))      // Custom WebSocket protocol (graph updates, logs, etc.)
	http.HandleFunc("/lsp", wrapWS(s.HandleGLSPWebSocket)) // ATS LSP protocol (completions, hover, semantic tokens)
	http.HandleFunc("/health", wrapPublic(s.HandleHealth)) // Health check always public
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
	http.HandleFunc("/api/plugins/{name}/logs", wrap(s.HandlePluginLogs))                           // Plugin log stream (SSE)
	http.HandleFunc("/api/plugins/{name}/config", wrap(s.HandlePluginConfig))                       // Plugin configuration (GET/PUT)
	http.HandleFunc("/api/plugins/glyphs", wrap(s.HandlePluginGlyphs))                              // List custom plugin glyphs (GET)
	http.HandleFunc("/api/plugins/", wrap(s.HandlePluginAction))                                    // Plugin actions: pause/resume (POST)
	http.HandleFunc("/api/plugins", wrap(s.HandlePlugins))                                          // List installed plugins (GET)
	http.HandleFunc("/api/types/", wrap(s.HandleTypes))                                             // Get specific type (GET /api/types/{typename})
	http.HandleFunc("/api/types", wrap(s.HandleTypes))                                              // List/create types (GET/POST)
	http.HandleFunc("/api/watchers/queue/stats", wrap(s.HandleWatcherQueueStats))                   // Watcher execution queue stats (GET)
	http.HandleFunc("/api/watchers/", wrap(s.HandleWatchers))                                       // Watcher CRUD (GET/PUT/DELETE /api/watchers/{id})
	http.HandleFunc("/api/watchers", wrap(s.HandleWatchers))                                        // List/create watchers (GET/POST)
	http.HandleFunc("/api/attestations", wrap(s.HandleCreateAttestation))                           // Sync browser-created attestations (POST)
	http.HandleFunc("/api/glyph-config", wrap(s.HandleGlyphConfig))                                 // Plugin glyph config via attestations (GET/POST)
	http.HandleFunc("/api/canvas/glyphs/", wrap(s.canvasHandler.HandleGlyphs))                      // Glyph CRUD (GET/POST/DELETE /api/canvas/glyphs/{id})
	http.HandleFunc("/api/canvas/glyphs", wrap(s.canvasHandler.HandleGlyphs))                       // List/create glyphs (GET/POST)
	http.HandleFunc("/api/canvas/compositions/", wrap(s.canvasHandler.HandleCompositions))          // Composition CRUD (GET/POST/DELETE /api/canvas/compositions/{id})
	http.HandleFunc("/api/canvas/compositions", wrap(s.canvasHandler.HandleCompositions))           // List/create compositions (GET/POST)
	http.HandleFunc("/api/canvas/minimized-windows/", wrap(s.canvasHandler.HandleMinimizedWindows)) // Minimized window CRUD (DELETE /api/canvas/minimized-windows/{id})
	http.HandleFunc("/api/canvas/minimized-windows", wrap(s.canvasHandler.HandleMinimizedWindows))  // List/add minimized windows (GET/POST)
	http.HandleFunc("/api/canvas/export-dom", wrap(s.canvasHandler.HandleExportDOM))                // Export rendered DOM (POST /api/canvas/export-dom, demo mode only)
	http.HandleFunc("/api/canvas/export", wrap(s.canvasHandler.HandleExportStatic))                 // Export canvas via server-side rendering (GET /api/canvas/export?canvas_id={id})
	http.HandleFunc("/api/files/", wrap(s.HandleFiles))                                             // Serve stored file (GET /api/files/{id})
	http.HandleFunc("/api/files", wrap(s.HandleFiles))                                              // Upload file (POST)
	http.HandleFunc("/api/search/semantic", wrap(s.HandleSemanticSearch))                           // Semantic search (GET)
	http.HandleFunc("/api/embeddings/generate", wrap(s.HandleEmbeddingGenerate))                    // Generate embedding (POST)
	http.HandleFunc("/api/embeddings/batch", wrap(s.HandleEmbeddingBatch))                          // Batch generate embeddings (POST)
	http.HandleFunc("/api/embeddings/clusters", wrap(s.HandleEmbeddingClusters))                    // List stable clusters (GET)
	http.HandleFunc("/api/embeddings/cluster-timeline", wrap(s.HandleClusterTimeline))              // Cluster evolution timeline (GET)
	http.HandleFunc("/api/embeddings/cluster", wrap(s.HandleEmbeddingCluster))                      // HDBSCAN clustering (POST)
	http.HandleFunc("/api/embeddings/by-source", wrap(s.HandleEmbeddingsBySource))                  // Embeddings by attestation source IDs (POST)
	http.HandleFunc("/api/embeddings/info", wrap(s.HandleEmbeddingInfo))                            // Embedding service status (GET)
	http.HandleFunc("/api/embeddings/project", wrap(s.HandleEmbeddingProject))                      // UMAP projection (POST)
	http.HandleFunc("/api/embeddings/projections", wrap(s.HandleEmbeddingProjections))              // Get 2D projections (GET)
	// Sync routes are only registered when sync is enabled (loopback bind only).
	// See https://github.com/teranos/QNTX/issues/643
	if s.syncTree != nil {
		http.HandleFunc("/ws/sync", wrapWS(s.HandleSyncWebSocket))    // Sync peer WebSocket (incoming reconciliation)
		http.HandleFunc("/api/sync/status", wrap(s.HandleSyncStatus)) // Sync tree status (GET)
		http.HandleFunc("/api/sync", wrap(s.HandleSync))              // Initiate sync with peer (POST)
	}
	http.HandleFunc("/", wrapPublic(s.HandleStatic))
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

	// Read body once — Clone() does not preserve it, and the fallback path also needs it.
	var bodyBytes []byte
	if r.Body != nil {
		var err error
		bodyBytes, err = io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, fmt.Sprintf("Failed to read request body: %v", err), http.StatusBadRequest)
			return
		}
		r.Body.Close()
	}

	// Try stripped path first (modern approach)
	recorder := &responseRecorder{ResponseWriter: w, statusCode: http.StatusOK}
	newReq := r.Clone(r.Context())
	newReq.URL.Path = strippedPath
	newReq.RequestURI = strippedPath
	newReq.Body = io.NopCloser(bytes.NewReader(bodyBytes))
	newReq.ContentLength = int64(len(bodyBytes))
	mux.ServeHTTP(recorder, newReq)

	// If 404, try full path (backward compat for plugins that include prefix)
	if recorder.statusCode == http.StatusNotFound {
		s.logger.Debugw("Stripped path 404, trying full path",
			"plugin", pluginName,
			"stripped", strippedPath,
			"full", path)
		r.Body = io.NopCloser(bytes.NewReader(bodyBytes))
		r.ContentLength = int64(len(bodyBytes))
		mux.ServeHTTP(w, r)
		return
	}

	// Write buffered response
	recorder.flush()
}

// handlePluginWebSocket proxies WebSocket connections to the plugin's handler.
// Like handlePluginRequest, it waits for the plugin to be ready (async loading).
func (s *QNTXServer) handlePluginWebSocket(w http.ResponseWriter, r *http.Request) {
	// Extract plugin name from /ws/<name>
	pluginName := strings.TrimPrefix(r.URL.Path, "/ws/")

	// Wait for plugin to be ready (polls briefly since plugins load async)
	if s.pluginRegistry == nil {
		http.Error(w, "Plugin registry not available", http.StatusServiceUnavailable)
		return
	}
	if !s.pluginRegistry.IsReady(pluginName) {
		// Give async loading a moment to finish
		deadline := time.Now().Add(5 * time.Second)
		for time.Now().Before(deadline) && !s.pluginRegistry.IsReady(pluginName) {
			time.Sleep(100 * time.Millisecond)
		}
		if !s.pluginRegistry.IsReady(pluginName) {
			http.Error(w, fmt.Sprintf("Plugin '%s' is still loading", pluginName), http.StatusServiceUnavailable)
			return
		}
	}

	p, ok := s.pluginRegistry.Get(pluginName)
	if !ok {
		http.Error(w, fmt.Sprintf("Plugin '%s' not found", pluginName), http.StatusNotFound)
		return
	}

	wsHandlers, err := p.RegisterWebSocket()
	if err != nil {
		s.logger.Errorw("Failed to get WebSocket handlers", "plugin", pluginName, "error", err)
		http.Error(w, fmt.Sprintf("Plugin '%s' WebSocket error: %v", pluginName, err), http.StatusInternalServerError)
		return
	}

	handler, ok := wsHandlers["/ws/"+pluginName]
	if !ok {
		http.Error(w, fmt.Sprintf("Plugin '%s' has no WebSocket handler at /ws/%s", pluginName, pluginName), http.StatusNotFound)
		return
	}

	handler.ServeWS(w, r)
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
