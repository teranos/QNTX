package server

import (
	"context"
	"database/sql"
	"sync"
	"sync/atomic"
	"time"

	"github.com/teranos/QNTX/am"
	"github.com/teranos/QNTX/server/wslogs"
	"github.com/teranos/QNTX/version"
	"github.com/teranos/QNTX/graph"
	"github.com/teranos/QNTX/pulse/async"
	"github.com/teranos/QNTX/pulse/budget"
	"github.com/teranos/QNTX/pulse/schedule"
	"go.uber.org/zap"
)

// webFiles is defined in embed_prod.go (production) or embed_stub.go (testing)

// QNTXServer provides live-updating graph visualization for Ax queries
type QNTXServer struct {
	db            *sql.DB
	dbPath        string // Database file path (for display in banner)
	builder       *graph.AxGraphBuilder
	// langService   *lsp.Service          // TODO: Extract ats/lsp - Language service for ATS LSP features
	// usageTracker  *tracker.UsageTracker // TODO: Extract ai/tracker - Cached usage tracker (eliminates 172k+ allocations/day)
	budgetTracker *budget.Tracker    // Budget tracking for Pulse daemon
	daemon        *async.WorkerPool  // Background job processor (daemon)
	ticker        *schedule.Ticker   // Pulse ticker for scheduled jobs
	configWatcher *am.ConfigWatcher  // Config watcher for auto-reload on config changes
	clients       map[*Client]bool
	broadcast     chan *graph.Graph
	register      chan *Client
	unregister    chan *Client
	mu            sync.RWMutex
	lastGraph     *graph.Graph        // Cache last broadcast graph for reconnecting clients
	lastStatus    *cachedDaemonStatus // Cache last daemon status for change detection
	lastUsage     *cachedUsageStats   // Cache last usage stats for change detection
	verbosity     atomic.Int32        // Thread-safe verbosity level (fixes Issue #64)
	graphLimit    atomic.Int32        // Thread-safe graph node limit (default 1000)
	logger        *zap.SugaredLogger
	logTransport  *wslogs.Transport
	wsCore        *wslogs.WebSocketCore
	initialQuery  string // Pre-loaded Ax query to execute on client connection

	// Lifecycle management (defensive programming)
	ctx            context.Context    // Cancellation context for graceful shutdown
	cancel         context.CancelFunc // Cancels all goroutines
	wg             sync.WaitGroup     // Tracks active goroutines for clean shutdown
	broadcastDrops atomic.Int64       // Tracks dropped broadcasts for monitoring
	state          atomic.Int32       // GRACE Phase 4: Server state (Running/Draining/Stopped)
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
		select {
		case client.send <- cachedGraph:
			s.logger.Debugw("Cached graph sent successfully", "client_id", client.id)
		default:
			s.logger.Warnw("Failed to send cached graph to client", "client_id", client.id)
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

		client.close()
		s.logTransport.UnregisterClient(client.id)

		s.logger.Infow("Client disconnected",
			"client_id", client.id,
			"total_clients", totalClients,
		)
	} else {
		s.mu.Unlock()
	}
}

// removeSlowClient safely removes a client that can't keep up with broadcasts
func (s *QNTXServer) removeSlowClient(client *Client) {
	s.mu.Lock()
	if _, ok := s.clients[client]; ok {
		delete(s.clients, client)
		s.mu.Unlock()
		client.close()
		s.logger.Warnw("Client send channel full, removing client",
			"client_id", client.id,
			"total_drops", s.broadcastDrops.Load(),
		)
	} else {
		s.mu.Unlock()
	}
}

// handleBroadcast sends a graph update to all connected clients
func (s *QNTXServer) handleBroadcast(g *graph.Graph) {
	// Cache graph and snapshot clients atomically
	s.mu.Lock()
	s.lastGraph = g
	clients := make([]*Client, 0, len(s.clients))
	for client := range s.clients {
		clients = append(clients, client)
	}
	clientCount := len(clients)
	s.mu.Unlock()

	// Broadcast to all clients (without holding lock to avoid deadlock)
	dropped := 0
	for _, client := range clients {
		select {
		case client.send <- g:
			// Success
		default:
			// Track drops and remove slow client
			dropped++
			s.broadcastDrops.Add(1)
			s.removeSlowClient(client)
		}
	}

	if dropped > 0 {
		s.logger.Warnw("Broadcast had drops",
			"clients", clientCount,
			"dropped", dropped,
			"total_drops", s.broadcastDrops.Load(),
		)
	}
}

// Run starts the server hub event loop
func (s *QNTXServer) Run() {
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
