package server

import (
	"context"
	"database/sql"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/teranos/QNTX/ai/tracker"
	"github.com/teranos/QNTX/am"
	"github.com/teranos/QNTX/ats/embeddings/embeddings"
	"github.com/teranos/QNTX/ats/lsp"
	"github.com/teranos/QNTX/ats/storage"
	"github.com/teranos/QNTX/ats/vidstream/vidstream"
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
	"github.com/teranos/QNTX/server/nodedid"
	"github.com/teranos/QNTX/server/wslogs"
	syncPkg "github.com/teranos/QNTX/sync"
	"go.uber.org/zap"
)

// webFiles is defined in embed_prod.go (production) or embed_stub.go (testing)

// QNTXServer provides live-updating graph visualization for Ax queries
type QNTXServer struct {
	db                  *sql.DB
	dbPath              string           // Database file path (for display in banner)
	authHandler         *auth.Handler    // nil when auth.enabled = false
	authEnabled         bool             // resolved at init, never changes
	nodeDID             *nodedid.Handler // node's decentralized identity
	builder             *graph.AxGraphBuilder
	langService         *lsp.Service          // Language service for ATS LSP features
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
	logTransport        *wslogs.Transport
	wsCore              *wslogs.WebSocketCore
	consoleBuffer       *ConsoleBuffer              // Browser console log buffer for debugging (dev mode only)
	initialQuery        string                      // Pre-loaded Ax query to execute on client connection
	pluginRegistry      *plugin.Registry            // Domain plugin registry
	pluginManager       *grpcplugin.PluginManager   // Plugin process manager
	services            plugin.ServiceRegistry      // Service registry for plugins
	servicesManager     *grpcplugin.ServicesManager // gRPC services for plugin callbacks (Issue #138)

	// VidStream real-time video inference (browser → WS → ONNX)
	vidstreamEngine *vidstream.VideoEngine // Singleton video processing engine
	vidstreamMu     sync.Mutex             // Protects vidstream engine operations

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

	// Watcher engine for reactive attestation triggers
	watcherEngine   *watcher.Engine
	reloadCoalescer *watcherReloadCoalescer

	// Canvas state handlers
	canvasHandler *handlers.CanvasHandler

	// Sync: Merkle tree observer for content-addressed attestation sync
	syncTree           syncPkg.SyncTree      // nil if WASM unavailable
	syncObserver       *syncPkg.TreeObserver // nil if WASM unavailable
	syncPeerStatus     sync.Map              // map[string]string — peer name → "ok", "unreachable", or "self"
	syncPeerRemoteName sync.Map              // map[string]string — peer name → advertised name from hello

	// Embedding service for semantic search (optional, requires rustembeddings build tag)
	embeddingService interface {
		GenerateEmbedding(text string) (*embeddings.EmbeddingResult, error)
		GenerateBatchEmbeddings(texts []string) (*embeddings.BatchEmbeddingResult, error)
		GetModelInfo() (*embeddings.ModelInfo, error)
		SerializeEmbedding(embedding []float32) ([]byte, error)
		DeserializeEmbedding(data []byte) ([]float32, error)
		ComputeSimilarity(a, b []float32) (float32, error)
		Close() error
	}
	embeddingStore              *storage.EmbeddingStore
	embeddingClusterInvalidator func() // called after re-cluster to invalidate centroid cache
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

	// Register client for log batches
	s.logTransport.RegisterClient(client.id, client.sendLog)

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

	connectionMsg := wslogs.Message{
		Level:     "INFO",
		Timestamp: time.Now(),
		Logger:    "server",
		Message:   "WebSocket connection established",
		Fields: map[string]interface{}{
			"client_id": client.id,
			"version":   versionInfo.Short(),
		},
	}
	s.logTransport.SendBatch(&wslogs.Batch{
		Messages:  []wslogs.Message{connectionMsg},
		QueryID:   "connection",
		Timestamp: time.Now(),
	})

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

		s.logTransport.UnregisterClient(client.id)

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

	// Unregister from log transport
	s.logTransport.UnregisterClient(client.id)

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

// GetServices returns the service registry for plugins
func (s *QNTXServer) GetServices() plugin.ServiceRegistry {
	return s.services
}

// GetDaemon returns the Pulse worker pool for dynamic handler registration
func (s *QNTXServer) GetDaemon() *async.WorkerPool {
	return s.daemon
}
