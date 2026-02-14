package server

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/teranos/QNTX/ats/parser"
	"github.com/teranos/QNTX/ats/storage"
	"github.com/teranos/QNTX/errors"
	"github.com/teranos/QNTX/graph"
	grapherr "github.com/teranos/QNTX/graph/error"
	"github.com/teranos/QNTX/logger"
	"github.com/teranos/QNTX/server/syscap"
	"github.com/teranos/QNTX/server/wslogs"
)

// WebSocket timeout constants following Gorilla best practices
// See: https://github.com/gorilla/websocket/blob/master/examples/chat/client.go
const (
	// Time allowed to write a message to the peer
	writeWait = 10 * time.Second

	// Time allowed to read the next pong message from the peer
	pongWait = 60 * time.Second

	// Send pings to peer with this period (must be less than pongWait)
	pingPeriod = 54 * time.Second

	// Maximum message size allowed from peer
	// Currently 10MB to support VidStream frames (640x480 RGBA as JSON ≈ 4.4MB)
	// TODO: Switch to binary WebSocket frames to reduce payload from 4.4MB → 1.2MB
	//       Binary format: 12-byte header (width:u32, height:u32, format:u32) + raw bytes
	//       This would allow reducing maxMessageSize back to 2MB
	maxMessageSize = 10 * 1024 * 1024

	// Semantic search defaults for unified search
	// TODO(#486): Make configurable via am.toml and UI
	semanticSearchLimit     = 20
	semanticSearchThreshold = float32(0.3)
)

// createErrorGraph creates an empty graph with error metadata.
// It handles both structured GraphError types and generic errors,
// providing appropriate metadata for UI display.
func createErrorGraph(err error) *graph.Graph {
	meta := graph.Meta{
		GeneratedAt: time.Now(),
		Stats: graph.Stats{
			TotalNodes: 0,
			TotalEdges: 0,
		},
	}

	// Use structured error metadata if available
	if graphErr, ok := err.(*grapherr.GraphError); ok {
		meta.Config = graphErr.ToGraphMeta()
	} else {
		meta.Config = map[string]string{
			"error": err.Error(),
		}
	}

	return &graph.Graph{
		Nodes: []graph.Node{},
		Links: []graph.Link{},
		Meta:  meta,
	}
}

// applyVisibilityFilters applies client-specific visibility preferences to a graph.
// Phase 2: Server-side visibility control - backend sets Visible field based on client prefs.
// Frontend just renders nodes where visible === true.
func (c *Client) applyVisibilityFilters(g *graph.Graph) {
	c.graphView.mu.RLock()
	defer c.graphView.mu.RUnlock()

	// Build connection count map to identify isolated nodes
	connectionCount := make(map[string]int)
	for _, link := range g.Links {
		connectionCount[link.Source]++
		connectionCount[link.Target]++
	}

	// Apply visibility rules to nodes
	for i := range g.Nodes {
		node := &g.Nodes[i]

		// Rule 1: Hide nodes if their type is in hiddenNodeTypes
		if c.graphView.hiddenNodeTypes[node.Type] {
			node.Visible = false
			continue
		}

		// Rule 2: Hide isolated nodes if hideIsolatedNodes is true
		if c.graphView.hideIsolatedNodes && connectionCount[node.ID] == 0 {
			node.Visible = false
			continue
		}

		// Default: visible (already set to true by graph builder)
	}

	// Build set of visible node IDs for link filtering
	visibleNodes := make(map[string]bool)
	for _, node := range g.Nodes {
		if node.Visible {
			visibleNodes[node.ID] = true
		}
	}

	// Apply visibility to links: hide link if either endpoint is hidden
	for i := range g.Links {
		link := &g.Links[i]
		// Link is only visible if both endpoints are visible
		link.Hidden = !visibleNodes[link.Source] || !visibleNodes[link.Target]
	}
}

// GraphViewState encapsulates client-specific graph visibility preferences (Phase 2)
type GraphViewState struct {
	hiddenNodeTypes   map[string]bool // Node types to hide (e.g., "contact" -> true)
	hideIsolatedNodes bool            // Whether to hide nodes with no connections
	mu                sync.RWMutex    // Protects state updates
}

// Client represents a WebSocket client connection
type Client struct {
	server    *QNTXServer
	conn      *websocket.Conn
	send      chan *graph.Graph
	sendLog   chan *wslogs.Batch
	sendMsg   chan interface{} // Generic message channel for ix progress/errors
	id        string
	closeOnce sync.Once       // Defensive: Prevents double-close panics
	graphView *GraphViewState // Phase 2: Client's graph visibility preferences
	lastQuery string          // Phase 2: Last executed query for re-rendering with new visibility
}

// readPump handles reading messages from the WebSocket connection
func (c *Client) readPump() {
	defer func() {
		c.server.unregister <- c
		c.conn.Close()
	}()

	// Configure connection limits and timeouts per Gorilla best practices
	c.conn.SetReadLimit(maxMessageSize)
	c.conn.SetReadDeadline(time.Now().Add(pongWait))
	c.conn.SetPongHandler(func(string) error {
		c.conn.SetReadDeadline(time.Now().Add(pongWait))
		return nil
	})

	c.server.logger.Debugw("Read pump started", "client_id", c.id)

	for {
		_, messageBytes, err := c.conn.ReadMessage()
		if err != nil {
			c.handleReadError(err)
			break
		}

		// Log message size for large messages (helps diagnose WebSocket issues)
		msgSize := len(messageBytes)
		if msgSize > 500000 { // Log if > 500KB
			c.server.logger.Infow("Large WebSocket message received",
				"client_id", c.id,
				"size_bytes", msgSize,
				"size_mb", float64(msgSize)/(1024*1024),
			)
		} else if logger.ShouldOutput(int(c.server.verbosity.Load()), logger.OutputDataDump) {
			c.server.logger.Debugw("Received WebSocket message",
				"client_id", c.id,
				"size_bytes", msgSize,
			)
		}

		var msg QueryMessage
		if err := json.Unmarshal(messageBytes, &msg); err != nil {
			c.server.logger.Warnw("JSON unmarshal error",
				"error", err.Error(),
				"client_id", c.id,
				"message_size", msgSize,
			)
			continue
		}

		c.routeMessage(&msg)
	}
}

// handleReadError logs unexpected WebSocket read errors.
// Expected closure codes (going away, abnormal, no status) are silently ignored.
func (c *Client) handleReadError(err error) {
	// Always log close errors with full details for debugging
	if closeErr, ok := err.(*websocket.CloseError); ok {
		c.server.logger.Infow("WebSocket closed",
			"client_id", c.id,
			"code", closeErr.Code,
			"text", closeErr.Text,
		)
	}

	if websocket.IsUnexpectedCloseError(err,
		websocket.CloseGoingAway,
		websocket.CloseAbnormalClosure,
		websocket.CloseNoStatusReceived,
	) {
		graphErr := grapherr.New(
			grapherr.CategoryWebSocket,
			err,
			"WebSocket connection closed unexpectedly",
		).WithSubcategory(grapherr.SubcategoryWSRead)

		c.server.logger.Warnw("WebSocket read error",
			graphErr.ToLogFields()...,
		)
	}
}

// routeMessage dispatches incoming WebSocket messages to appropriate handlers.
// This separation from readPump reduces complexity and improves testability.
func (c *Client) routeMessage(msg *QueryMessage) {
	switch msg.Type {
	case "query":
		c.handleQuery(msg.Query)
	case "clear":
		c.handleClear()
	case "set_verbosity":
		c.handleSetVerbosity(msg.Verbosity)
	case "set_graph_limit":
		c.handleSetGraphLimit(msg.GraphLimit)
	case "upload":
		c.handleUpload(msg.Filename, msg.FileType, msg.Data)
	case "parse_request":
		c.handleParseRequest(*msg)
	case "completion_request":
		c.handleCompletionRequest(*msg)
	case "hover_request":
		c.handleHoverRequest(*msg)
	case "daemon_control":
		c.handleDaemonControl(*msg)
	case "pulse_config_update":
		c.handlePulseConfigUpdate(*msg)
	case "job_control":
		c.handleJobControl(*msg)
	case "visibility": // Phase 2: Handle visibility preference updates
		c.handleVisibility(*msg)
	case "rich_search":
		c.handleRichSearch(msg.Query)
	case "vidstream_init":
		c.handleVidStreamInit(*msg)
	case "vidstream_frame":
		c.handleVidStreamFrame(*msg)
	case "get_database_stats":
		c.handleGetDatabaseStats()
	case "watcher_upsert":
		c.handleWatcherUpsert(*msg)
	case "ping":
		// Just update deadline, handled by pong handler
	default:
		c.server.logger.Debugw("Unknown message type",
			"type", msg.Type,
			"client_id", c.id,
		)
	}
}

// writePump writes graph updates and log batches to the WebSocket connection
func (c *Client) writePump() {
	ticker := time.NewTicker(pingPeriod)
	defer func() {
		ticker.Stop()
		c.conn.Close()
	}()

	c.server.logger.Debugw("Write pump started", "client_id", c.id)

	for {
		select {
		case <-c.server.ctx.Done():
			c.server.logger.Debugw("Write pump stopping due to server shutdown", "client_id", c.id)
			return
		case g, ok := <-c.send:
			c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if !ok {
				c.conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}

			// Send graph as JSON
			if err := c.conn.WriteJSON(g); err != nil {
				graphErr := grapherr.New(
					grapherr.CategoryWebSocket,
					err,
					"Failed to send graph to client",
				).WithSubcategory(grapherr.SubcategoryWSWrite)

				c.server.logger.Warnw("Graph write error",
					append(graphErr.ToLogFields(), "client_id", c.id)...,
				)
				return
			}

			if logger.ShouldOutput(int(c.server.verbosity.Load()), logger.OutputInternalFlow) {
				c.server.logger.Debugw("Sent graph to client",
					"client_id", c.id,
					"nodes", len(g.Nodes),
					"links", len(g.Links),
				)
			}

		case logBatch, ok := <-c.sendLog:
			c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if !ok {
				return
			}

			// Send log batch as JSON with type marker
			message := map[string]interface{}{
				"type": "logs",
				"data": logBatch,
			}

			if err := c.conn.WriteJSON(message); err != nil {
				c.server.logger.Debugw("Log batch write error",
					"error", err.Error(),
					"client_id", c.id,
				)
				// Don't return - log errors shouldn't kill connection
			}

		case msg, ok := <-c.sendMsg:
			c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if !ok {
				return
			}

			// Send generic message (ix progress, errors, etc.)
			if err := c.conn.WriteJSON(msg); err != nil {
				c.server.logger.Debugw("Message write error",
					"error", err.Error(),
					"client_id", c.id,
				)
				// Don't return - message errors shouldn't kill connection
			}

		case <-ticker.C:
			c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

// handleQuery processes an Ax query and sends the resulting graph
func (c *Client) handleQuery(query string) {
	ctx := c.server.ctx // Use server's cancellable context
	queryID := fmt.Sprintf("q_%d", time.Now().UnixNano())

	// TODO: Extract ix command handling - domain-specific ingestion commands deferred
	// For now, treat all input as Ax queries
	c.lastQuery = query

	// Handle as Ax query
	// Create log batcher for this query
	batcher := wslogs.NewBatcher(queryID, c.server.logTransport)

	// Set batcher on WebSocket core to start collecting logs
	c.server.wsCore.SetBatcher(batcher)
	defer func() {
		c.server.wsCore.ClearBatcher()
		batcher.Flush() // Send all collected logs
	}()

	// Log query start
	c.server.logger.Infow("Processing Ax query",
		"query_id", queryID,
		"client_id", c.id,
		"query_length", len(query),
	)

	if logger.ShouldOutput(int(c.server.verbosity.Load()), logger.OutputAxExecution) {
		c.server.logger.Debugw("Query details",
			"query_id", queryID,
			"query", query,
		)
	}

	// Build graph from query (this will generate logs that get batched)
	limit := int(c.server.graphLimit.Load())
	g, err := c.server.builder.BuildFromQuery(ctx, query, limit)
	if err != nil {
		// Error already logged by builder - create error graph for display
		g = createErrorGraph(err)
	} else {
		c.server.logger.Infow("Query completed",
			"query_id", queryID,
			"nodes", len(g.Nodes),
			"links", len(g.Links),
		)
	}

	// Phase 2: Apply client-specific visibility filters before sending
	c.applyVisibilityFilters(g)

	// Send graph to client
	select {
	case c.send <- g:
	default:
		c.server.logger.Warnw("Client send channel full, dropping graph update",
			"client_id", c.id,
			"query_id", queryID,
		)
	}
}

// handleClear sends an empty graph
func (c *Client) handleClear() {
	c.server.logger.Debugw("Clearing graph", "client_id", c.id)

	g := &graph.Graph{
		Nodes: []graph.Node{},
		Links: []graph.Link{},
		Meta: graph.Meta{
			GeneratedAt: time.Now(),
			Stats: graph.Stats{
				TotalNodes: 0,
				TotalEdges: 0,
			},
			Config: map[string]string{
				"description": "Type an Ax query to see the graph...",
			},
		},
	}

	select {
	case c.send <- g:
	default:
		c.server.logger.Warnw("Client send channel full, dropping clear",
			"client_id", c.id,
		)
	}
}

// handleSetVerbosity updates the server verbosity level
func (c *Client) handleSetVerbosity(verbosity int) {
	oldVerbosity := int(c.server.verbosity.Load())
	c.server.verbosity.Store(int32(verbosity))

	// Update the zap logger level
	level := logger.VerbosityToLevel(verbosity)
	c.server.wsCore.LevelEnabler = level

	c.server.logger.Infow("Verbosity level changed",
		"client_id", c.id,
		"old_verbosity", oldVerbosity,
		"new_verbosity", verbosity,
		"level_name", logger.LevelName(verbosity),
	)
}

// handleSetGraphLimit sets the graph node limit and triggers a refresh
func (c *Client) handleSetGraphLimit(limit int) {
	if limit <= 0 || limit > 100000 {
		c.server.logger.Warnw("Invalid graph limit, ignoring",
			"client_id", c.id,
			"requested_limit", limit,
		)
		return
	}

	oldLimit := int(c.server.graphLimit.Load())
	c.server.graphLimit.Store(int32(limit))

	c.server.logger.Infow("Graph limit changed",
		"client_id", c.id,
		"old_limit", oldLimit,
		"new_limit", limit,
	)

	// Trigger graph refresh with new limit
	c.server.refreshGraphFromDatabase()
}

// handleUpload processes file uploads from the client
func (c *Client) handleUpload(filename, fileType, data string) {
	c.server.logger.Infow("File upload received",
		"client_id", c.id,
		"filename", filename,
		"fileType", fileType,
		"data_length", len(data),
	)

	// Process upload in a goroutine to avoid blocking the WebSocket
	c.server.wg.Add(1)
	go func() {
		defer c.server.wg.Done()

		// Decode base64 data (but don't process it yet - handler not implemented)
		_, err := base64Decode(data)
		if err != nil {
			c.server.logger.Errorw("Failed to decode upload data",
				"client_id", c.id,
				"filename", filename,
				"error", err,
			)
			return
		}

		// TODO: Implement file upload handler
		// if err := c.server.handleFileUpload(filename, fileType, decodedData); err != nil {
		// 	c.server.logger.Errorw("File upload processing failed",
		// 		"client_id", c.id,
		// 		"filename", filename,
		// 		"error", err,
		// 	)
		// } else {
		// 	c.server.logger.Infow("File upload processed successfully",
		// 		"client_id", c.id,
		// 		"filename", filename,
		// 	)
		// }
		c.server.logger.Infow("File upload received but not processed (handler not implemented)",
			"client_id", c.id,
			"filename", filename,
		)
	}()
}

// handleParseRequest processes parse requests for semantic highlighting
func (c *Client) handleParseRequest(msg QueryMessage) {
	ctx := c.server.ctx
	// Use language service to parse with semantic tokens
	resp, err := c.server.langService.Parse(ctx, msg.Query, int(c.server.verbosity.Load()))
	if err != nil {
		c.sendJSON(map[string]interface{}{
			"type":  "error",
			"error": err.Error(),
		})
		return
	}
	// Send parse response to client
	c.sendJSON(map[string]interface{}{
		"type":        "parse_response",
		"timestamp":   msg.Line, // Reuse Line field for timestamp correlation
		"tokens":      resp.Tokens,
		"diagnostics": resp.Diagnostics,
		"parse_state": resp.ParseState,
	})
}

// handleCompletionRequest processes completion requests for autocomplete
func (c *Client) handleCompletionRequest(msg QueryMessage) {
	return
}

// handleHoverRequest processes hover requests for entity information
func (c *Client) handleHoverRequest(msg QueryMessage) {
	return
}

// sendJSON is a helper to send JSON messages to the client
func (c *Client) sendJSON(data interface{}) {
	select {
	case c.sendMsg <- data:
		// Message queued successfully
	default:
		c.server.logger.Warnw("Failed to queue message (channel full)",
			"client_id", c.id,
		)
	}
}

// handleDaemonControl handles daemon start/stop requests
func (c *Client) handleDaemonControl(msg QueryMessage) {
	c.server.logger.Infow("Daemon control request",
		"action", msg.Action,
		"client_id", c.id,
	)

	var err error
	switch msg.Action {
	case "start":
		err = c.server.startDaemon()
	case "stop":
		err = c.server.stopDaemon()
	default:
		c.server.logger.Warnw("Unknown daemon control action",
			"action", msg.Action,
			"client_id", c.id,
		)
		return
	}

	if err != nil {
		c.server.logger.Errorw("Daemon control failed",
			"action", msg.Action,
			"error", err,
			"client_id", c.id,
		)
	}
}

// handlePulseConfigUpdate updates Pulse configuration at runtime
func (c *Client) handlePulseConfigUpdate(msg QueryMessage) {
	c.server.logger.Infow("Pulse config update request",
		"daily_budget", msg.DailyBudget,
		"weekly_budget", msg.WeeklyBudget,
		"monthly_budget", msg.MonthlyBudget,
		"client_id", c.id,
	)

	// Validate budgets
	if msg.DailyBudget < 0 {
		c.server.logger.Warnw("Invalid daily budget",
			"daily_budget", msg.DailyBudget,
			"client_id", c.id,
		)
		return
	}

	if msg.WeeklyBudget < 0 {
		c.server.logger.Warnw("Invalid weekly budget",
			"weekly_budget", msg.WeeklyBudget,
			"client_id", c.id,
		)
		return
	}

	if msg.MonthlyBudget < 0 {
		c.server.logger.Warnw("Invalid monthly budget",
			"monthly_budget", msg.MonthlyBudget,
			"client_id", c.id,
		)
		return
	}

	// Update daily budget
	if msg.DailyBudget > 0 {
		err := c.server.budgetTracker.UpdateDailyBudget(msg.DailyBudget)
		if err != nil {
			c.server.logger.Errorw("Failed to update daily budget",
				"daily_budget", msg.DailyBudget,
				"error", err,
				"client_id", c.id,
			)
			return
		}
	}

	// Update weekly budget
	if msg.WeeklyBudget > 0 {
		err := c.server.budgetTracker.UpdateWeeklyBudget(msg.WeeklyBudget)
		if err != nil {
			c.server.logger.Errorw("Failed to update weekly budget",
				"weekly_budget", msg.WeeklyBudget,
				"error", err,
				"client_id", c.id,
			)
			return
		}
	}

	// Update monthly budget
	if msg.MonthlyBudget > 0 {
		err := c.server.budgetTracker.UpdateMonthlyBudget(msg.MonthlyBudget)
		if err != nil {
			c.server.logger.Errorw("Failed to update monthly budget",
				"monthly_budget", msg.MonthlyBudget,
				"error", err,
				"client_id", c.id,
			)
			return
		}
	}

	c.server.logger.Infow("Pulse budgets updated successfully",
		"daily_budget", msg.DailyBudget,
		"weekly_budget", msg.WeeklyBudget,
		"monthly_budget", msg.MonthlyBudget,
		"client_id", c.id,
	)
}

// handleJobControl handles job pause/resume/details requests
func (c *Client) handleJobControl(msg QueryMessage) {
	c.server.logger.Infow("Job control request",
		"action", msg.Action,
		"job_id", msg.JobID,
		"client_id", c.id,
	)

	// Validate job ID
	if msg.JobID == "" {
		c.server.logger.Warnw("Missing job ID",
			"action", msg.Action,
			"client_id", c.id,
		)
		return
	}

	queue := c.server.daemon.GetQueue()
	if queue == nil {
		c.server.logger.Warnw("Queue not available",
			"job_id", msg.JobID,
			"client_id", c.id,
		)
		return
	}

	var err error
	switch msg.Action {
	case "pause":
		err = queue.PauseJob(msg.JobID, "User requested via UI")
		if err == nil {
			c.server.logger.Infow("Job paused",
				"job_id", msg.JobID,
				"client_id", c.id,
			)
			// Broadcast job update
			if job, getErr := queue.GetJob(msg.JobID); getErr == nil {
				c.server.broadcastJobUpdate(job)
			}
		}

	case "resume":
		err = queue.ResumeJob(msg.JobID)
		if err == nil {
			c.server.logger.Infow("Job resumed",
				"job_id", msg.JobID,
				"client_id", c.id,
			)
			// Broadcast job update
			if job, getErr := queue.GetJob(msg.JobID); getErr == nil {
				c.server.broadcastJobUpdate(job)
			}
		}

	case "details":
		job, err := queue.GetJob(msg.JobID)
		if err == nil {
			c.server.logger.Infow("Job details retrieved",
				"job_id", msg.JobID,
				"client_id", c.id,
			)
			// Send job details to requesting client
			c.sendMsg <- JobUpdateMessage{
				Type: "job_details",
				Job:  job,
				Metadata: map[string]interface{}{
					"timestamp": time.Now().Unix(),
				},
			}
		}

	default:
		c.server.logger.Warnw("Unknown job control action",
			"action", msg.Action,
			"job_id", msg.JobID,
			"client_id", c.id,
		)
		return
	}

	if err != nil {
		c.server.logger.Errorw("Job control failed",
			"action", msg.Action,
			"job_id", msg.JobID,
			"error", err,
			"client_id", c.id,
		)
	}
}

// handleGetDatabaseStats retrieves database statistics and sends them to the client
func (c *Client) handleGetDatabaseStats() {
	c.server.logger.Infow("Database stats request",
		"client_id", c.id,
	)

	// Get basic storage statistics from database
	var totalAttestations, uniqueActors, uniqueSubjects, uniqueContexts int
	err := c.server.db.QueryRow(`
		SELECT
			COUNT(*) as total_attestations,
			COUNT(DISTINCT json_extract(actors, '$[0]')) as unique_actors,
			COUNT(DISTINCT json_extract(subjects, '$')) as unique_subjects,
			COUNT(DISTINCT json_extract(contexts, '$')) as unique_contexts
		FROM attestations
	`).Scan(&totalAttestations, &uniqueActors, &uniqueSubjects, &uniqueContexts)

	if err != nil {
		c.server.logger.Errorw("Failed to query database stats",
			"error", err,
			"client_id", c.id,
		)
		c.sendJSON(map[string]interface{}{
			"type":  "error",
			"error": fmt.Sprintf("Failed to retrieve database statistics: %v", err),
		})
		return
	}

	// Get discovered rich fields with statistics from a bounded store instance
	boundedStore := storage.NewBoundedStore(c.server.db, c.server.logger.Named("db-stats"))
	richFieldsWithStats, err := boundedStore.GetRichFieldsWithStats()
	if err != nil {
		c.server.logger.Errorw("Failed to get rich fields with stats",
			"error", err,
			"client_id", c.id,
		)
		// Fall back to simple field list
		richFields := boundedStore.GetDiscoveredRichFields()
		c.sendJSON(map[string]interface{}{
			"type":               "database_stats",
			"path":               c.server.dbPath,
			"total_attestations": totalAttestations,
			"unique_actors":      uniqueActors,
			"unique_subjects":    uniqueSubjects,
			"unique_contexts":    uniqueContexts,
			"rich_fields":        richFields,
		})
		return
	}

	// Get storage backend info
	storageBackend := "go"
	if syscap.IsStorageOptimized() {
		storageBackend = "rust"
	}

	// Send stats to client with enhanced field information
	c.sendJSON(map[string]interface{}{
		"type":               "database_stats",
		"path":               c.server.dbPath,
		"storage_backend":    storageBackend,
		"storage_optimized":  syscap.IsStorageOptimized(),
		"storage_version":    syscap.GetStorageVersion(),
		"total_attestations": totalAttestations,
		"unique_actors":      uniqueActors,
		"unique_subjects":    uniqueSubjects,
		"unique_contexts":    uniqueContexts,
		"rich_fields":        richFieldsWithStats,
	})

	c.server.logger.Infow("Database stats sent",
		"total_attestations", totalAttestations,
		"client_id", c.id,
	)
}

// handleRichSearch performs unified search: text search + semantic search
func (c *Client) handleRichSearch(query string) {
	// Trim and validate query
	query = strings.TrimSpace(query)
	if query == "" {
		c.sendJSON(map[string]interface{}{
			"type":    "rich_search_results",
			"query":   query,
			"matches": []interface{}{},
			"total":   0,
		})
		return
	}

	c.server.logger.Infow("Unified search",
		"query", query,
		"client_id", c.id,
	)

	// Text search (fuzzy/exact)
	boundedStore := storage.NewBoundedStore(c.server.db, c.server.logger.Named("search"))
	ctx := c.server.ctx
	matches, err := boundedStore.SearchRichStringFields(ctx, query, 50)
	if err != nil {
		err = errors.Wrapf(err, "text search failed for query %q", query)
		c.server.logger.Warnw("Text search failed",
			"query", query,
			"error", err,
			"client_id", c.id,
		)
		c.sendJSON(map[string]interface{}{
			"type":  "rich_search_error",
			"error": err.Error(),
		})
		return
	}

	// Semantic search (if embedding service available)
	if c.server.embeddingService != nil && c.server.embeddingStore != nil {
		semanticMatches := c.searchSemantic(query)
		if len(semanticMatches) > 0 {
			matches = mergeSearchResults(matches, semanticMatches)
		}
	}

	// Ensure matches is never nil (nil serializes as JSON null, breaking frontend)
	if matches == nil {
		matches = []storage.RichSearchMatch{}
	}

	c.sendJSON(map[string]interface{}{
		"type":    "rich_search_results",
		"query":   query,
		"matches": matches,
		"total":   len(matches),
	})

	c.server.logger.Infow("Unified search results sent",
		"query", query,
		"text_matches", len(matches),
		"semantic_available", c.server.embeddingService != nil,
		"client_id", c.id,
	)
}

// searchSemantic generates an embedding for the query and searches the vector store.
// Returns empty slice on any failure — semantic search is best-effort.
func (c *Client) searchSemantic(query string) []storage.RichSearchMatch {
	queryResult, err := c.server.embeddingService.GenerateEmbedding(query)
	if err != nil {
		c.server.logger.Debugw("Semantic embedding failed", "query", query, "error", err)
		return nil
	}

	queryBlob, err := c.server.embeddingService.SerializeEmbedding(queryResult.Embedding)
	if err != nil {
		c.server.logger.Debugw("Semantic serialization failed", "query", query, "error", err)
		return nil
	}

	searchResults, err := c.server.embeddingStore.SemanticSearch(queryBlob, semanticSearchLimit, semanticSearchThreshold)
	if err != nil {
		c.server.logger.Debugw("Semantic search failed", "query", query, "error", err)
		return nil
	}

	var matches []storage.RichSearchMatch
	for _, result := range searchResults {
		if result.SourceType != "attestation" {
			continue
		}

		attestation, err := storage.GetAttestationByID(c.server.db, result.SourceID)
		if err != nil || attestation == nil {
			continue
		}

		nodeID := result.SourceID
		if len(attestation.Subjects) > 0 {
			nodeID = attestation.Subjects[0]
		}

		displayLabel := nodeID
		typeName := "Document"
		var attributes map[string]interface{}
		if attestation.Attributes != nil {
			attributes = attestation.Attributes
			if label, ok := attributes["label"].(string); ok && label != "" {
				displayLabel = label
			} else if name, ok := attributes["name"].(string); ok && name != "" {
				displayLabel = name
			}
			if t, ok := attributes["type"].(string); ok {
				typeName = t
			}
		}

		excerpt := result.Text
		if len(excerpt) > 150 {
			excerpt = excerpt[:150] + "..."
		}

		matches = append(matches, storage.RichSearchMatch{
			NodeID:       nodeID,
			TypeName:     typeName,
			TypeLabel:    typeName,
			FieldName:    "content",
			FieldValue:   result.Text,
			Excerpt:      excerpt,
			Score:        float64(result.Similarity),
			Strategy:     storage.StrategySemantic,
			DisplayLabel: displayLabel,
			Attributes:   attributes,
		})
	}

	return matches
}

// mergeSearchResults combines text and semantic results, deduplicating by NodeID.
func mergeSearchResults(text, semantic []storage.RichSearchMatch) []storage.RichSearchMatch {
	seen := make(map[string]int) // NodeID -> index in result
	result := make([]storage.RichSearchMatch, len(text))
	copy(result, text)
	for i, m := range result {
		seen[m.NodeID] = i
	}

	for _, s := range semantic {
		if idx, exists := seen[s.NodeID]; exists {
			if s.Score > result[idx].Score {
				result[idx] = s
			}
		} else {
			seen[s.NodeID] = len(result)
			result = append(result, s)
		}
	}

	sort.Slice(result, func(i, j int) bool {
		return result[i].Score > result[j].Score
	})

	return result
}

// handleVisibility updates client visibility preferences and refreshes the graph
func (c *Client) handleVisibility(msg QueryMessage) {
	c.server.logger.Infow("Visibility preference update",
		"action", msg.Action,
		"client_id", c.id,
	)

	// Update client's visibility preferences (lock only for state mutation)
	c.graphView.mu.Lock()
	switch msg.Action {
	case "toggle_node_type":
		// Normalize node type to lowercase for consistent matching
		nodeType := strings.ToLower(strings.TrimSpace(msg.NodeType))

		if msg.Hidden {
			c.graphView.hiddenNodeTypes[nodeType] = true
			c.server.logger.Debugw("Node type hidden",
				"node_type", nodeType,
				"client_id", c.id,
			)
		} else {
			delete(c.graphView.hiddenNodeTypes, nodeType)
			c.server.logger.Debugw("Node type shown",
				"node_type", nodeType,
				"client_id", c.id,
			)
		}

	case "toggle_isolated":
		c.graphView.hideIsolatedNodes = msg.Hidden
		c.server.logger.Debugw("Isolated nodes visibility changed",
			"hidden", msg.Hidden,
			"client_id", c.id,
		)

	default:
		c.server.logger.Warnw("Unknown visibility action",
			"action", msg.Action,
			"client_id", c.id,
		)
		c.graphView.mu.Unlock()
		return
	}
	c.graphView.mu.Unlock()

	// Re-run last query to generate updated graph with new visibility
	// (This will trigger applyVisibilityFilters before sending)
	if c.lastQuery != "" {
		c.server.logger.Debugw("Re-running query with updated visibility",
			"client_id", c.id,
			"query", c.lastQuery,
		)
		c.handleQuery(c.lastQuery)
	} else {
		c.server.logger.Debugw("No query to re-run",
			"client_id", c.id,
		)
	}
}

// base64Decode decodes a base64 string (helper for file uploads)
func base64Decode(data string) (string, error) {
	const maxUploadSize = 512 * 1024 * 1024 // 512MB limit

	// Check size before decoding to prevent memory exhaustion
	// Base64 encoding adds ~33% overhead, so decoded size = len(data) * 3/4
	estimatedSize := (len(data) * 3) / 4
	if estimatedSize > maxUploadSize {
		return "", errors.Newf("upload too large: estimated %d bytes exceeds %d byte limit", estimatedSize, maxUploadSize)
	}

	// Decode from base64
	bytes, err := base64.StdEncoding.DecodeString(data)
	if err != nil {
		return "", errors.Wrap(err, "base64 decode failed")
	}

	// Verify actual decoded size doesn't exceed limit
	if len(bytes) > maxUploadSize {
		return "", errors.Newf("upload too large: %d bytes exceeds %d byte limit", len(bytes), maxUploadSize)
	}

	return string(bytes), nil
}

// close safely closes the client's channels using sync.Once to prevent double-close panics.
// Only called from the broadcast worker goroutine (single-writer model).
func (c *Client) close() {
	c.closeOnce.Do(func() {
		if c.send != nil {
			close(c.send)
		}
		if c.sendLog != nil {
			close(c.sendLog)
		}
		if c.sendMsg != nil {
			close(c.sendMsg)
		}
	})
}

// extractErrorSeverity determines the appropriate severity level for broadcasting
// a watcher error based on the error type. Uses parser error metadata when available.
func extractErrorSeverity(err error) string {
	// Check for ParseError with Severity field
	if parseErr, ok := err.(*parser.ParseError); ok {
		return string(parseErr.Severity) // "error", "warning", "info", "hint"
	}
	// Check for ParseWarning (best-effort parsing with warnings)
	if _, ok := err.(*parser.ParseWarning); ok {
		return "warning"
	}
	// Default to error for all other errors
	return "error"
}

// handleWatcherUpsert creates or updates a watcher based on AX glyph query
func (c *Client) handleWatcherUpsert(msg QueryMessage) {
	c.server.logger.Debugw("Watcher upsert request",
		"watcher_id", msg.WatcherID,
		"query", msg.WatcherQuery,
		"client_id", c.id,
	)

	// Validate watcher engine
	if c.server.watcherEngine == nil {
		c.server.logger.Warnw("Watcher engine not available",
			"client_id", c.id,
		)
		return
	}

	// Generate ID if not provided
	watcherID := msg.WatcherID
	if watcherID == "" {
		watcherID = fmt.Sprintf("watcher-%d", time.Now().UnixNano())
	}

	// Create watcher struct
	watcher := &storage.Watcher{
		ID:                watcherID,
		Name:              msg.WatcherName,
		AxQuery:           msg.WatcherQuery,
		ActionType:        storage.ActionTypePython, // Default to Python action
		ActionData:        "",                       // Empty for now
		MaxFiresPerMinute: 60,                       // Default rate limit
		Enabled:           msg.Enabled,
	}

	// Try to get existing watcher first
	existing, err := c.server.watcherEngine.GetStore().Get(c.server.ctx, watcherID)
	if err == nil {
		// Update existing watcher
		watcher.CreatedAt = existing.CreatedAt
		watcher.FireCount = existing.FireCount
		watcher.ErrorCount = existing.ErrorCount
		watcher.LastFiredAt = existing.LastFiredAt
		watcher.LastError = existing.LastError

		if err := c.server.watcherEngine.GetStore().Update(c.server.ctx, watcher); err != nil {
			c.server.logger.Errorw("Failed to update watcher",
				"watcher_id", watcherID,
				"error", err,
				"client_id", c.id,
			)
			return
		}
		c.server.logger.Infow("Updated watcher",
			"watcher_id", watcherID,
			"query", msg.WatcherQuery,
		)
	} else {
		// Create new watcher
		if err := c.server.watcherEngine.GetStore().Create(c.server.ctx, watcher); err != nil {
			c.server.logger.Errorw("Failed to create watcher",
				"watcher_id", watcherID,
				"error", err,
				"client_id", c.id,
			)
			return
		}
		c.server.logger.Infow("Created watcher",
			"watcher_id", watcherID,
			"query", msg.WatcherQuery,
		)
	}

	// Reload watchers in engine
	if err := c.server.watcherEngine.ReloadWatchers(); err != nil {
		c.server.logger.Errorw("Failed to reload watchers",
			"error", err,
			"client_id", c.id,
		)
		// Broadcast error to frontend with severity based on error type
		severity := extractErrorSeverity(err)
		c.server.broadcastWatcherError(watcherID, err.Error(), severity, errors.GetAllDetails(err)...)
		return
	}

	// Check if watcher was successfully loaded (parsing succeeded)
	// If parsing failed, the watcher won't be in the engine's in-memory map
	reloadedWatcher, exists := c.server.watcherEngine.GetWatcher(watcherID)
	if !exists || reloadedWatcher == nil {
		// Watcher exists in DB but failed to load (likely parse error)
		// Get the actual parse error from the engine
		parseErr := c.server.watcherEngine.GetParseError(watcherID)
		if parseErr != nil {
			// Broadcast with full error details
			c.server.logger.Warnw("Watcher parse failed",
				"watcher_id", watcherID,
				"query", msg.WatcherQuery,
				"error", parseErr,
			)
			severity := extractErrorSeverity(parseErr)
			c.server.broadcastWatcherError(watcherID, parseErr.Error(), severity, errors.GetAllDetails(parseErr)...)
		} else {
			// Fallback if no parse error was stored (shouldn't happen)
			errMsg := "Failed to parse AX query - watcher not activated"
			c.server.logger.Warnw("Watcher parse failed (no error details)",
				"watcher_id", watcherID,
				"query", msg.WatcherQuery,
			)
			c.server.broadcastWatcherError(watcherID, errMsg, "error",
				fmt.Sprintf("Query: %s", msg.WatcherQuery),
			)
		}
		return
	}

	// Query historical matches for the watcher (in goroutine to avoid blocking)
	c.server.wg.Add(1)
	go func() {
		defer c.server.wg.Done()
		if err := c.server.watcherEngine.QueryHistoricalMatches(watcherID); err != nil {
			c.server.logger.Errorw("Failed to query historical matches",
				"watcher_id", watcherID,
				"error", err,
			)
		}
	}()
}

// sendSystemCapabilitiesToClient sends system capability information to a newly connected client.
// This informs the frontend about available optimizations (e.g., Rust fuzzy matching, ONNX video).
// Sends are routed through broadcast worker (thread-safe).
func (s *QNTXServer) sendSystemCapabilitiesToClient(client *Client) {
	// Get system capabilities from syscap package
	fuzzyBackend := s.builder.FuzzyBackend()
	msg := syscap.Get(fuzzyBackend)

	// Send to broadcast worker (thread-safe)
	req := &broadcastRequest{
		reqType:  "message",
		msg:      msg,
		clientID: client.id, // Send to specific client only
	}

	select {
	case s.broadcastReq <- req:
		s.logger.Debugw("Queued system capabilities to client",
			"client_id", client.id,
			"fuzzy_backend", fuzzyBackend,
			"fuzzy_optimized", msg.FuzzyOptimized,
		)
	case <-s.ctx.Done():
		return
	default:
		// Broadcast queue full (should never happen with proper sizing)
		s.logger.Warnw("Broadcast request queue full, skipping system capabilities",
			"client_id", client.id,
		)
	}
}
