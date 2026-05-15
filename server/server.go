package server

import (
	"context"
	"database/sql"
	"net/http"
	"sync"
	"sync/atomic"

	"github.com/teranos/QNTX/ai/tracker"
	"github.com/teranos/QNTX/am"
	"github.com/teranos/QNTX/ats"
	"github.com/teranos/QNTX/ats/storage"
	"github.com/teranos/QNTX/ats/types"

	"github.com/teranos/QNTX/ats/watcher"
	"github.com/teranos/QNTX/glyph/handlers"
	"github.com/teranos/QNTX/graph"
	"github.com/teranos/QNTX/internal/version"
	"github.com/teranos/QNTX/plugin"
	grpcplugin "github.com/teranos/QNTX/plugin/grpc"
	"github.com/teranos/QNTX/pulse/async"
	"github.com/teranos/QNTX/pulse/budget"
	"github.com/teranos/QNTX/pulse/schedule"
	"github.com/teranos/QNTX/server/auth"
	serverembeddings "github.com/teranos/QNTX/server/embeddings"
	"github.com/teranos/QNTX/server/nodedid"
	"go.uber.org/zap"
)

// webFiles is defined in embed_prod.go (production) or embed_stub.go (testing)

// QNTXServer provides live-updating graph visualization for Ax queries
type QNTXServer struct {
	db                  *sql.DB
	dbPath              string               // Database file path (for display in banner)
	logPath             string               // File log path (for download endpoint and banner)
	atsStore            ats.AttestationStore // Attestation store (Rust FFI or Go SQLite)
	bindAddress         string               // Network interface (e.g., "127.0.0.1" or "0.0.0.0")
	authHandler         *auth.Handler        // nil when auth.enabled = false
	authEnabled         bool                 // resolved at init, never changes
	nodeDID             *nodedid.Handler     // node's decentralized identity
	builder             *graph.AxGraphBuilder
	usageTracker        *tracker.UsageTracker // Cached usage tracker (eliminates 172k+ allocations/day)
	budgetTracker       *budget.Tracker       // Budget tracking for Pulse daemon
	daemon              *async.WorkerPool     // Background job processor (daemon)
	ticker              *schedule.Ticker      // Pulse ticker for scheduled jobs
	configWatcher       *am.ConfigWatcher     // Config watcher for auto-reload on config changes
	storageEventsPoller *StorageEventsPoller  // Poller for storage events (warnings/evictions)
	clients             map[*Client]bool
	broadcast           chan *graph.Graph
	broadcastReq        chan *broadcastRequest // Requests to broadcast worker (thread-safe sends)
	register            chan *Client
	unregister          chan *Client
	mu                  sync.RWMutex
	lastGraph           *graph.Graph        // Cache last broadcast graph for reconnecting clients
	lastStatus          *cachedDaemonStatus // Cache last daemon status for change detection
	lastUsage           *cachedUsageStats   // Cache last usage stats for change detection
	verbosity           atomic.Int32        // Thread-safe verbosity level (fixes Issue #64)
	graphLimit          atomic.Int32        // Thread-safe graph node limit (default 1000)
	logger              *zap.SugaredLogger
	consoleBuffer       *ConsoleBuffer              // Browser console log buffer for debugging (dev mode only)
	initialQuery        string                      // Pre-loaded Ax query to execute on client connection
	pluginRegistry      *plugin.Registry            // Domain plugin registry
	pluginManager       *grpcplugin.PluginManager   // Plugin process manager
	services            plugin.ServiceRegistry      // Service registry for plugins
	servicesManager     *grpcplugin.ServicesManager // gRPC services for plugin callbacks (Issue #138)

	// Plugin HTTP routing (lazy initialization for async plugin loading)
	pluginMuxes   sync.Map // map[string]*http.ServeMux - plugin name -> dedicated mux
	pluginMuxInit sync.Map // map[string]*sync.Once - ensures thread-safe one-time initialization per plugin

	// HTTP server with timeouts
	httpServer *http.Server

	// Lifecycle management (defensive programming)
	ctx            context.Context    // Cancellation context for graceful shutdown
	cancel         context.CancelFunc // Cancels all goroutines
	wg             sync.WaitGroup     // Tracks active goroutines for clean shutdown
	broadcastDrops atomic.Int64       // Tracks dropped broadcasts for monitoring
	state          atomic.Int32       // Opening/Closing Phase 4: Server state (Running/Draining/Stopped)

	// Per-IP rate limiting groups
	rlAuth   *rateLimitGroup
	rlWS     *rateLimitGroup
	rlWrite  *rateLimitGroup
	rlRead   *rateLimitGroup
	rlPublic *rateLimitGroup

	// Watcher engine for reactive attestation triggers
	watcherEngine   *watcher.Engine
	reloadCoalescer *watcherReloadCoalescer

	// Canvas state handlers
	canvasHandler *handlers.CanvasHandler

	// Embedding handlers (semantic search, clustering, projection)
	embeddingsHandler *serverembeddings.Handler

	conversationAssembler *ConversationAssembler

	// Embedding service for semantic search (provided by embedding_provider plugin)
	embeddingService serverembeddings.Service
	embeddingStore              *storage.EmbeddingStore
	embeddingClusterInvalidator func()                  // called after re-cluster to invalidate centroid cache
	embeddingStats              schedule.EmbeddingStats // drained by ticker for periodic summary
	groundDBPath                string
	watcherDB                   *sql.DB                // Separate DB connection for watcher engine (avoids RustStore contention)
	walCheckpointer             WALCheckpointer        // Rust-side WAL checkpoint (closes read conns, checkpoints, reopens)
	ageDistiller                AgeDistiller           // Rust-side age distillation (fold old attestations into sigmas)
	onReady                     func()                 // Called once when server is fully ready (routes, DB, listeners)

	// Cached database stats — refreshed every 30s in the background.
	// Glyph opens return instantly from cache instead of blocking on 4+ queries.
	dbStatsCache atomic.Pointer[cachedDBStats]
}

// SetWALCheckpointer sets the Rust-side WAL checkpointer (closes read conns, checkpoints, reopens).
func (s *QNTXServer) SetWALCheckpointer(c WALCheckpointer) {
	s.walCheckpointer = c
}

// SetAgeDistiller sets the Rust-side age distiller (fold old attestations into sigmas).
func (s *QNTXServer) SetAgeDistiller(d AgeDistiller) {
	s.ageDistiller = d
}


// handleClientRegister handles a new client connection
func (s *QNTXServer) handleClientRegister(client *Client) {
	s.mu.Lock()

	// Defensive: Check client limit
	if len(s.clients) >= MaxClients {
		s.mu.Unlock()
		s.logger.Warnw("Max clients reached, rejecting connection",
			"client_id", client.id,
			"max_clients", MaxClients,
		)
		client.close()
		return
	}

	s.clients[client] = true
	totalClients := len(s.clients)
	cachedGraph := s.lastGraph
	s.mu.Unlock()

	s.logger.Infow("Client connected",
		"client_id", client.id,
		"total_clients", totalClients,
	)

	// Send connection message to UI logs panel
	versionInfo := version.Get()
	s.logger.Infow("WebSocket connection established",
		"client_id", client.id,
		"version", versionInfo.Short(),
	)

	// Send cached graph to newly connected client
	if cachedGraph != nil {
		s.logger.Infow("Sending cached graph to reconnected client",
			"client_id", client.id,
			"nodes", len(cachedGraph.Nodes),
			"links", len(cachedGraph.Links),
		)

		// Send via broadcast worker (thread-safe)
		req := &broadcastRequest{
			reqType:  "graph",
			graph:    cachedGraph,
			clientID: client.id, // Send to specific client only
		}

		select {
		case s.broadcastReq <- req:
			s.logger.Debugw("Queued cached graph for client", "client_id", client.id)
		case <-s.ctx.Done():
			return
		default:
			s.logger.Warnw("Broadcast request queue full, skipping cached graph", "client_id", client.id)
		}
	}
}

// handleClientUnregister handles a client disconnection
func (s *QNTXServer) handleClientUnregister(client *Client) {
	s.mu.Lock()
	if _, ok := s.clients[client]; ok {
		delete(s.clients, client)
		totalClients := len(s.clients)
		s.mu.Unlock()

		// Signal broadcast worker to close channels (thread-safe)
		req := &broadcastRequest{
			reqType: "close",
			client:  client,
		}
		select {
		case s.broadcastReq <- req:
			// Request queued
		case <-s.ctx.Done():
			// Server shutting down, close directly
			client.close()
		}

		s.logger.Infow("Client disconnected",
			"client_id", client.id,
			"total_clients", totalClients,
		)
	} else {
		s.mu.Unlock()
	}
}

// removeSlowClient safely removes a client that can't keep up with broadcasts.
// IMPORTANT: Only called from broadcast worker, so safe to close channels directly.
func (s *QNTXServer) removeSlowClient(client *Client) {
	s.mu.Lock()
	if _, ok := s.clients[client]; ok {
		delete(s.clients, client)
		s.mu.Unlock()
	} else {
		s.mu.Unlock()
		return // Already removed
	}

	// Close channels directly (we're in broadcast worker context, single-writer invariant maintained)
	client.close()

	s.logger.Warnw("Client send channel full, removing client",
		"client_id", client.id,
		"total_drops", s.broadcastDrops.Load(),
	)
}

// handleBroadcast sends a graph update to all connected clients via the broadcast worker
func (s *QNTXServer) handleBroadcast(g *graph.Graph) {
	// Cache graph for reconnecting clients
	s.mu.Lock()
	s.lastGraph = g
	s.mu.Unlock()

	// Send to broadcast worker (thread-safe)
	req := &broadcastRequest{
		reqType: "graph",
		graph:   g,
	}

	select {
	case s.broadcastReq <- req:
		// Request queued successfully
	case <-s.ctx.Done():
		// Server shutting down
	default:
		// Broadcast queue full (should never happen with proper sizing)
		s.logger.Warnw("Broadcast request queue full, dropping graph update")
	}
}

// Run starts the server hub event loop
func (s *QNTXServer) Run() {
	// Start dedicated broadcast worker (MUST start before processing any messages)
	// This worker owns all client channel sends to prevent race conditions
	go s.runBroadcastWorker()

	for {
		select {
		case <-s.ctx.Done():
			s.logger.Debugw("Server hub stopping due to context cancellation")
			return
		case client := <-s.register:
			s.handleClientRegister(client)
		case client := <-s.unregister:
			s.handleClientUnregister(client)
		case g := <-s.broadcast:
			s.handleBroadcast(g)
		}
	}
}

// Global server instance for async plugin initialization
var (
	defaultServer   *QNTXServer
	defaultServerMu sync.RWMutex
)

// SetDefaultServer sets the global server instance
func SetDefaultServer(s *QNTXServer) {
	defaultServerMu.Lock()
	defer defaultServerMu.Unlock()
	defaultServer = s
}

// GetDefaultServer returns the global server instance
func GetDefaultServer() *QNTXServer {
	defaultServerMu.RLock()
	defer defaultServerMu.RUnlock()
	return defaultServer
}

// getPluginManager returns the plugin manager, falling back to the global default
// if the server's field is nil (happens when plugins load asynchronously after server creation).
// When falling back, lazily wires servicesManager so LLM provider re-registration works on restart.
func (s *QNTXServer) getPluginManager() *grpcplugin.PluginManager {
	if s.pluginManager != nil {
		return s.pluginManager
	}
	pm := grpcplugin.GetDefaultPluginManager()
	if pm != nil && s.servicesManager != nil {
		pm.SetServicesManager(s.servicesManager)
	}
	return pm
}

// GetServices returns the service registry for plugins
func (s *QNTXServer) GetServices() plugin.ServiceRegistry {
	return s.services
}

// GetDaemon returns the Pulse worker pool for dynamic handler registration
func (s *QNTXServer) GetDaemon() *async.WorkerPool {
	return s.daemon
}

// GetDB returns the database connection for schedule setup
func (s *QNTXServer) GetDB() *sql.DB {
	return s.db
}

// GetServicesManager returns the gRPC services manager for plugin service access
func (s *QNTXServer) GetServicesManager() *grpcplugin.ServicesManager {
	return s.servicesManager
}

// SetOnReady sets a callback invoked once the server is fully ready
// (migrations complete, routes set up, HTTP listening). Plugins should
// initialize after this fires, not before.
func (s *QNTXServer) SetOnReady(fn func()) {
	s.onReady = fn
}

// ReloadWatchers reloads the watcher engine's in-memory map from the database.
func (s *QNTXServer) ReloadWatchers() error {
	if s.watcherEngine == nil {
		return nil
	}
	return s.watcherEngine.ReloadWatchers()
}

// InvalidatePluginMux clears cached HTTP mux state for a plugin so the next
// request re-initializes it. Called after plugin auto-restart to avoid stale
// sync.Once that was poisoned by a previous failed init.
func (s *QNTXServer) InvalidatePluginMux(name string) {
	s.pluginMuxes.Delete(name)
	s.pluginMuxInit.Delete(name)
}

// RegisterPluginMux proactively registers HTTP proxy handlers for a plugin.
// Called after plugin restart so HTTP routes work immediately without waiting
// for a slow lazy-init gRPC Initialize call.
func (s *QNTXServer) RegisterPluginMux(name string) {
	p, ok := s.pluginRegistry.Get(name)
	if !ok {
		return
	}
	mux := http.NewServeMux()
	if err := p.RegisterHTTP(mux); err != nil {
		s.logger.Errorw("Failed to pre-register HTTP handlers for plugin", "plugin", name, "error", err)
		return
	}
	s.pluginMuxes.Store(name, mux)
	if ep, ok := p.(*grpcplugin.ExternalDomainProxy); ok {
		s.logger.Infow("Registered HTTP proxy handlers", "plugin", name, "addr", ep.Addr())
	} else {
		s.logger.Infow("Registered HTTP proxy handlers", "plugin", name)
	}
}

// getAttestationByID retrieves a single attestation through the attestation store (Rust FFI).
// Falls back to Go's *sql.DB if the store doesn't support direct get.
func (s *QNTXServer) getAttestationByID(id string) (*types.As, error) {
	type singleGetter interface {
		GetAttestation(id string) (*types.As, error)
	}
	if sg, ok := s.atsStore.(singleGetter); ok {
		return sg.GetAttestation(id)
	}
	// Fallback for non-Rust stores (tests)
	return storage.GetAttestationByID(s.db, id)
}

// queryAttestationsRaw executes a raw SQL query through the attestation store (Rust FFI).
// Falls back to Go's *sql.DB if the store doesn't support raw queries.
func (s *QNTXServer) queryAttestationsRaw(sql string, params []interface{}) ([]*types.As, error) {
	type rawQuerier interface {
		QueryAttestationsRaw(sql string, params []interface{}) ([]*types.As, error)
	}
	if rq, ok := s.atsStore.(rawQuerier); ok {
		return rq.QueryAttestationsRaw(sql, params)
	}
	// Fallback for non-Rust stores (tests)
	return storage.GetAttestationsRaw(s.db, sql, params)
}
