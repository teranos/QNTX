package server

import (
	"bytes"
	"fmt"
	"github.com/teranos/QNTX/internal/sqlclose"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/teranos/QNTX/plugin/grpc"
	"github.com/teranos/QNTX/server/reach"
	"github.com/teranos/errors"
	"go.uber.org/zap"
)

// setupHTTPRoutes says what this node can answer and where.

// None of it is served yet. server/reach reads its table and serves what the
// lines grant; a path named here that no line grants is unreachable.
func (s *QNTXServer) setupHTTPRoutes() {
	s.answering = map[string]reach.Answering{}

	// The ceremony admits strangers, which is what logging in is. That is a
	// line in the table saying ANYONE, the same as everything else.

	// Asked of a nil handler on a node with auth.enabled = false, which answers
	// the same paths saying it has no login.
	for path, handler := range s.authHandler.Routes() {
		s.answer(path, handler)
	}

	s.answer("/.well-known/did.json", s.nodeDID.HandleDIDDocument)

	// Register plugin routes with dynamic handler that waits for plugins to load
	// This allows routes to be registered immediately while plugins load asynchronously
	if s.pluginRegistry != nil {
		// A plugin registers its own routes over gRPC and the host never sees
		// them, so no row here can describe one. Every plugin route is ROOT's.
		for _, name := range s.pluginRegistry.ListEnabled() {
			// Register exact match for /api/{plugin} (e.g., /api/code)
			exactPattern := "/api/" + name
			s.answer(exactPattern, s.handlePluginRequest)

			// Register wildcard for /api/{plugin}/* (e.g., /api/code/file.go)
			wildcardPattern := "/api/" + name + "/{path...}"
			s.answer(wildcardPattern, s.handlePluginRequest)

			s.pluginRoutes.Store(name, true)
			s.logger.Debugw("Registered HTTP routes", "plugin", name,
				"exact", exactPattern,
				"wildcard", wildcardPattern)
		}
	}

	// Register WebSocket routes for plugins (same lazy pattern as HTTP routes above).
	// Plugins load asynchronously, so we register /ws/<name> from pre-registered names
	// and resolve the actual handler when the connection arrives.
	if s.pluginRegistry != nil {
		for _, name := range s.pluginRegistry.ListEnabled() {
			pattern := "/ws/" + name
			s.answerSocket(pattern, s.handlePluginWebSocket)
			s.logger.Debugw("Registered WebSocket route", "plugin", name, "path", pattern)
		}
	}

	// Generic /ws/llm resolves the configured LLM provider and proxies to it.
	// UI connects here instead of hardcoding a plugin name.
	s.answerSocket("/ws/llm", s.handleLLMWebSocket)

	// Core QNTX handlers
	s.answerSocket("/ws", s.HandleWebSocket) // Custom WebSocket protocol (graph updates, logs, etc.)
	s.answer("/health", s.HandleHealth)
	s.answer("/api/version", s.HandleVersion) // Which build is running (GET)
	s.answer("/logs/download", s.HandleLogDownload)
	s.answer("/api/timeseries/usage", s.HandleUsageTimeSeries)
	s.answer("/api/config", s.HandleConfig)
	s.answer("/api/dev", s.HandleDevMode)                                              // Dev mode status
	s.answer("/api/debug", s.HandleDebug)                                              // Browser console debugging (dev mode only)
	s.answer("/api/crash-test", s.HandleCrashTest)                                     // Flight recorder crash test (dev mode only)
	s.answer("/api/prose", s.HandleProse)                                              // Prose content tree
	s.answer("/api/prose/", s.HandleProseContent)                                      // Individual prose files
	s.answer("/api/pulse/executions/", s.HandlePulseExecution)                         // Individual execution (GET) and logs (GET /logs)
	s.answer("/api/pulse/schedules/", s.HandlePulseSchedule)                           // Individual schedule (GET/PATCH/DELETE)
	s.answer("/api/pulse/schedules", s.HandlePulseSchedules)                           // List/create schedules (GET/POST)
	s.answer("/api/pulse/jobs/", s.HandlePulseJob)                                     // Individual async job and sub-resources (GET)
	s.answer("/api/pulse/jobs", s.HandlePulseJobs)                                     // List async jobs (GET)
	s.answer("/api/prompt/", s.HandlePrompt)                                           // Prompt operations (preview/execute/list/save/get/versions)
	s.answer("/api/plugins/{name}/logs", s.HandlePluginLogs)                           // Plugin log stream (SSE)
	s.answer("/api/plugins/{name}/config", s.HandlePluginConfig)                       // Plugin configuration (GET/PUT)
	s.answer("/api/plugins/glyphs", s.pluginHandler.HandlePluginGlyphs)                // List custom plugin glyphs (GET)
	s.answer("/api/plugins/routes", s.pluginHandler.HandlePluginRoutes)                // List plugin routes and capabilities (GET)
	s.answer("/api/plugins/", s.HandlePluginAction)                                    // Plugin actions: pause/resume (POST)
	s.answer("/api/plugins", s.pluginHandler.HandlePlugins)                            // List installed plugins (GET)
	s.answer("/statusline", s.statusLineHandler.HandleStatusLine)                      // What a status line draws (GET)
	s.answer("/statusline/", s.statusLineHandler.HandleStatusLineItem)                 // What one item on it is doing (GET)
	s.answer("/api/types/", s.HandleTypes)                                             // Get specific type (GET /api/types/{typename})
	s.answer("/api/types", s.HandleTypes)                                              // List/create types (GET/POST)
	s.answer("/api/watchers/queue/stats", s.watcherHandler.HandleWatcherQueueStats)    // Watcher execution queue stats (GET)
	s.answer("/api/watchers/", s.watcherHandler.HandleWatchers)                        // Watcher CRUD (GET/PUT/DELETE /api/watchers/{id})
	s.answer("/api/watchers", s.watcherHandler.HandleWatchers)                         // List/create watchers (GET/POST)
	s.answer("/api/namespaces", s.HandleNamespaces)                                    // List/create namespaces (GET/POST)
	s.answer("/api/doors/draft", s.HandleDoorDraft)                                    // What the door onto a namespace would be (POST). Says the block; writes nothing
	s.answer("/api/attestations", s.HandleAttestations)                                // Query (GET) / create (POST) attestations
	s.answer("/api/glyph-config", s.HandleGlyphConfig)                                 // Plugin glyph config via attestations (GET/POST)
	s.answer("/api/canvas/glyphs/", s.canvasHandler.HandleGlyphs)                      // Glyph CRUD (GET/POST/DELETE /api/canvas/glyphs/{id})
	s.answer("/api/canvas/glyphs", s.canvasHandler.HandleGlyphs)                       // List/create glyphs (GET/POST)
	s.answer("/api/canvas/compositions/", s.canvasHandler.HandleCompositions)          // Composition CRUD (GET/POST/DELETE /api/canvas/compositions/{id})
	s.answer("/api/canvas/compositions", s.canvasHandler.HandleCompositions)           // List/create compositions (GET/POST)
	s.answer("/api/canvas/minimized-windows/", s.canvasHandler.HandleMinimizedWindows) // Minimized window CRUD (DELETE /api/canvas/minimized-windows/{id})
	s.answer("/api/canvas/minimized-windows", s.canvasHandler.HandleMinimizedWindows)  // List/add minimized windows (GET/POST)
	s.answer("/api/canvas/export-dom", s.canvasHandler.HandleExportDOM)                // Export rendered DOM (POST /api/canvas/export-dom, demo mode only)
	s.answer("/api/canvas/export", s.canvasHandler.HandleExportStatic)                 // Export canvas via server-side rendering (GET /api/canvas/export?canvas_id={id})
	s.answer("/api/files/", s.HandleFiles)                                             // Serve stored file (GET /api/files/{id})
	s.answer("/api/files", s.HandleFiles)                                              // Upload file (POST)
	// Python capability endpoint — delegates to whichever plugin declared python_provider=true.
	// TODO: generalize to capability-based routing for all provider types.
	s.answer("/api/python/execute", s.HandlePythonExecute)

	s.answer("/api/search/semantic", s.embeddingsHandler.HandleSemanticSearch)                     // Semantic search (GET)
	s.answer("/api/embeddings/generate", s.embeddingsHandler.HandleEmbeddingGenerate)              // Generate embedding (POST)
	s.answer("/api/embeddings/batch", s.embeddingsHandler.HandleEmbeddingBatch)                    // Batch generate embeddings (POST)
	s.answer("/api/embeddings/clusters", s.embeddingsHandler.HandleEmbeddingClusters)              // List stable clusters (GET)
	s.answer("/api/embeddings/clusters/samples", s.embeddingsHandler.HandleClusterSamples)         // Sample texts from a cluster (GET)
	s.answer("/api/embeddings/clusters/members", s.embeddingsHandler.HandleClusterMembers)         // Recent attestations in a cluster (GET)
	s.answer("/api/embeddings/clusters/memberships", s.embeddingsHandler.HandleClusterMemberships) // Cluster assignments for attestation IDs (GET)
	s.answer("/api/embeddings/cluster-timeline", s.embeddingsHandler.HandleClusterTimeline)        // Cluster evolution timeline (GET)
	s.answer("/api/embeddings/cluster", s.embeddingsHandler.HandleCluster)                         // HDBSCAN clustering (POST)
	s.answer("/api/embeddings/by-source", s.embeddingsHandler.HandleEmbeddingsBySource)            // Embeddings by attestation source IDs (POST)
	s.answer("/api/embeddings/info", s.embeddingsHandler.HandleEmbeddingInfo)                      // Embedding service status (GET)
	s.answer("/api/embeddings/unembedded", s.embeddingsHandler.HandleUnembeddedPage)               // Paginated unembedded IDs (GET)
	s.answer("/api/embeddings/project", s.embeddingsHandler.HandleProject)                         // UMAP projection (POST)
	s.answer("/api/embeddings/projections", s.embeddingsHandler.HandleEmbeddingProjections)        // Get 2D projections (GET)
	s.answer("/", s.HandleStatic)

}

// open builds what the node serves. A line granting reach to a path nothing
// answers stops the node, and a handler no line names is not served.
func (s *QNTXServer) open() error {
	served, unreachable, err := reach.Open(s.answering, s.wrapping())
	if err != nil {
		return err
	}
	s.served, s.unreachable = served, unreachable
	if len(unreachable) > 0 {
		s.logger.Infow("Compiled and unreachable; no line in server/reach grants them",
			"paths", unreachable)
	}
	return nil
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
		// A method the API serves and this list omits reaches the browser as a
		// refused preflight, which arrives as a network error naming nothing.
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
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

	// Check if mux was pre-registered (e.g. by plugin restart callback).
	// This avoids the slow lazy-init path that calls Initialize over gRPC.
	muxVal, muxExists := s.pluginMuxes.Load(pluginName)
	if !muxExists {
		// Lazy-initialize plugin mux on first request (after plugin loads)
		// Use sync.Once per plugin to ensure thread-safe one-time initialization
		onceVal, _ := s.pluginMuxInit.LoadOrStore(pluginName, &sync.Once{})
		once, isOnce := onceVal.(*sync.Once)
		if !isOnce {
			s.logger.Errorw("Plugin mux init guard held the wrong type; refusing the request", "plugin", pluginName)
			http.Error(w, fmt.Sprintf("Plugin '%s' routing state is corrupt", pluginName), http.StatusInternalServerError)
			return
		}

		// All concurrent requests will block here until initialization completes
		var initErr error
		once.Do(func() {
			plugin, ok := s.pluginRegistry.Get(pluginName)
			if !ok {
				initErr = errors.Newf("plugin '%s' not found", pluginName)
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
			if ep, ok := plugin.(*grpc.ExternalDomainProxy); ok {
				s.logger.Infow("Initialized HTTP handlers for plugin", "plugin", pluginName, "addr", ep.Addr())
			} else {
				s.logger.Infow("Initialized HTTP handlers for plugin", "plugin", pluginName)
			}
		})

		// Check if initialization failed
		if initErr != nil {
			http.Error(w, fmt.Sprintf("Plugin '%s' initialization failed: %v", pluginName, initErr), http.StatusInternalServerError)
			return
		}

		muxVal, muxExists = s.pluginMuxes.Load(pluginName)
		if !muxExists {
			http.Error(w, fmt.Sprintf("Plugin '%s' mux not found after initialization", pluginName), http.StatusInternalServerError)
			return
		}
	}

	// Serve request through plugin's mux
	// Try with stripped prefix first (e.g., /api/code/health -> /health)
	// If that 404s, try with full path (backward compatibility)
	// This allows plugins to register routes either way (Issue #277)
	mux, isMux := muxVal.(*http.ServeMux)
	if !isMux {
		s.logger.Errorw("Plugin mux entry held the wrong type; refusing the request", "plugin", pluginName)
		http.Error(w, fmt.Sprintf("Plugin '%s' routing state is corrupt", pluginName), http.StatusInternalServerError)
		return
	}

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
		sqlclose.Log(r.Body.Close(), s.logger, "the plugin request body")
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
	recorder.flush(s.logger)
}

// handleLLMWebSocket resolves the active LLM provider and proxies the WebSocket
// connection to it. The UI connects to /ws/llm without knowing the plugin name.
func (s *QNTXServer) handleLLMWebSocket(w http.ResponseWriter, r *http.Request) {
	provider := resolveProvider("")
	r.URL.Path = "/ws/" + provider
	s.handlePluginWebSocket(w, r)
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

func (rr *responseRecorder) flush(logger *zap.SugaredLogger) {
	if rr.wroteHeader {
		rr.ResponseWriter.WriteHeader(rr.statusCode)
	}
	if len(rr.body) > 0 {
		if _, err := rr.ResponseWriter.Write(rr.body); err != nil && logger != nil {
			logger.Warnw("Buffered plugin response not delivered",
				"status", rr.statusCode, "bytes", len(rr.body), "error", err)
		}
	}
}
