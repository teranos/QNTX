package server

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/teranos/QNTX/errors"
	"github.com/teranos/QNTX/graph"
	grapherr "github.com/teranos/QNTX/graph/error"
	"github.com/teranos/QNTX/logger"
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

	// Maximum message size allowed from peer (1MB for graph data)
	maxMessageSize = 1024 * 1024
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

		if logger.ShouldOutput(int(c.server.verbosity.Load()), logger.OutputDataDump) {
			c.server.logger.Debugw("Received WebSocket message",
				"client_id", c.id,
				"size_bytes", len(messageBytes),
			)
		}

		var msg QueryMessage
		if err := json.Unmarshal(messageBytes, &msg); err != nil {
			c.server.logger.Warnw("JSON unmarshal error",
				"error", err.Error(),
				"client_id", c.id,
			)
			continue
		}

		c.routeMessage(&msg)
	}
}

// handleReadError logs unexpected WebSocket read errors.
// Expected closure codes (going away, abnormal, no status) are silently ignored.
func (c *Client) handleReadError(err error) {
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
	// TODO(#54): Extract ats/lsp - LSP features deferred
	return
}

// handleHoverRequest processes hover requests for entity information
func (c *Client) handleHoverRequest(msg QueryMessage) {
	// TODO(#54): Extract ats/lsp - LSP features deferred
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

// close safely closes the client's channels using sync.Once to prevent double-close panics
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
