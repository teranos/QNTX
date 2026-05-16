package server

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/teranos/QNTX/am"
	"github.com/teranos/QNTX/ats/parser"
	"github.com/teranos/QNTX/ats/storage"
	"github.com/teranos/QNTX/errors"
	"github.com/teranos/QNTX/plugin/grpc/protocol"
	"github.com/teranos/QNTX/internal/logger"
	"github.com/teranos/QNTX/server/syscap"
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
	maxMessageSize = 2 * 1024 * 1024

	// Semantic search defaults for unified search
	// TODO(#486): Make configurable via am.toml and UI
	semanticSearchLimit     = 20
	semanticSearchThreshold = float32(0.3)
)

// Client represents a WebSocket client connection
type Client struct {
	server    *QNTXServer
	conn      *websocket.Conn
	sendMsg   chan interface{} // Generic message channel for all WebSocket messages
	id        string
	closeOnce sync.Once       // Defensive: Prevents double-close panics
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
		c.server.logger.Warnw("WebSocket read error",
			"error", err,
			"client_id", c.id,
		)
	}
}

// routeMessage dispatches incoming WebSocket messages to appropriate handlers.
// This separation from readPump reduces complexity and improves testability.
func (c *Client) routeMessage(msg *QueryMessage) {
	switch msg.Type {
	case "set_verbosity":
		c.handleSetVerbosity(msg.Verbosity)
	case "upload":
		c.handleUpload(msg.Filename, msg.FileType, msg.Data)
	case "daemon_control":
		c.handleDaemonControl(*msg)
	case "pulse_config_update":
		c.handlePulseConfigUpdate(*msg)
	case "job_control":
		c.handleJobControl(*msg)
	case "rich_search":
		c.handleRichSearch(msg.Query)
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

// writePump writes messages to the WebSocket connection
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

		case msg, ok := <-c.sendMsg:
			c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if !ok {
				return
			}

			if err := c.conn.WriteJSON(msg); err != nil {
				c.server.logger.Debugw("Message write error",
					"error", err.Error(),
					"client_id", c.id,
				)
			}

		case <-ticker.C:
			c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

// handleSetVerbosity updates the server verbosity level
func (c *Client) handleSetVerbosity(verbosity int) {
	oldVerbosity := int(c.server.verbosity.Load())
	c.server.verbosity.Store(int32(verbosity))

	c.server.logger.Infow("Verbosity level changed",
		"client_id", c.id,
		"old_verbosity", oldVerbosity,
		"new_verbosity", verbosity,
		"level_name", logger.LevelName(verbosity),
	)
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

// handleGetDatabaseStats sends cached database statistics to the client.
// Stats are refreshed every 30s in the background — glyph opens are instant.
func (c *Client) handleGetDatabaseStats() {
	cached := c.server.dbStatsCache.Load()
	if cached == nil {
		c.sendJSON(map[string]interface{}{
			"type":  "database_stats",
			"error": "Database stats not yet available (first refresh pending)",
		})
		return
	}
	c.sendJSON(cached.response)
}

// richSearchResponse is the typed WS response for rich_search_results.
// Mirrors proto: protocol.RichSearchResultsMessage (server.proto)
type richSearchResponse struct {
	Type    string                    `json:"type"`
	Query   string                    `json:"query"`
	Matches []storage.RichSearchMatch `json:"matches"`
	Total   int                       `json:"total"`
}

// handleRichSearch performs unified search: text search + semantic search
func (c *Client) handleRichSearch(query string) {
	// Trim and validate query
	query = strings.TrimSpace(query)
	if query == "" {
		c.sendJSON(richSearchResponse{
			Type:    "rich_search_results",
			Query:   query,
			Matches: []storage.RichSearchMatch{},
			Total:   0,
		})
		return
	}

	ctx := c.server.ctx

	// Text search: MeiliSearch if available, SQL substring fallback
	var matches []storage.RichSearchMatch
	var searchStrategy string

	if router := c.server.servicesManager.GetSearchRouter(); router != nil && router.HasProvider() {
		searchStrategy = "meilisearch"
		meiliMatches, err := c.searchMeili(ctx, query, 50)
		if err != nil {
			c.server.logger.Warnw("MeiliSearch failed, falling back to substring",
				"query", query,
				"error", err,
			)
			searchStrategy = "substring (meili fallback)"
			meiliMatches = nil
		}
		matches = meiliMatches
	}

	// SQL substring fallback (no MeiliSearch provider, or MeiliSearch failed)
	if matches == nil {
		if searchStrategy == "" {
			searchStrategy = "substring"
		}
		boundedStore := storage.NewBoundedStore(c.server.db, nil, c.server.logger.Named("search"))
		var err error
		matches, err = boundedStore.SearchRichStringFields(ctx, query, 50)
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
	}

	c.server.logger.Infow("Unified search",
		"query", query,
		"strategy", searchStrategy,
		"client_id", c.id,
	)

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

	c.sendJSON(richSearchResponse{
		Type:    "rich_search_results",
		Query:   query,
		Matches: matches,
		Total:   len(matches),
	})

	c.server.logger.Infow("Unified search results sent",
		"query", query,
		"text_matches", len(matches),
		"semantic_available", c.server.embeddingService != nil,
		"client_id", c.id,
	)
}

// searchMeili queries the MeiliSearch provider via the SearchService gRPC.
// Returns matches converted to RichSearchMatch format, or error on failure.
// The index name is "attestations" — the standard index for attestation rich fields.
func (c *Client) searchMeili(ctx context.Context, query string, limit int) ([]storage.RichSearchMatch, error) {
	router := c.server.servicesManager.GetSearchRouter()
	if router == nil {
		return nil, errors.New("no search router available")
	}

	resp, err := router.Search(ctx, &protocol.SearchRequest{
		Query: query,
		Index: "attestations",
		TopK:  int32(limit),
	})
	if err != nil {
		return nil, errors.Wrapf(err, "MeiliSearch query failed for %q", query)
	}

	matches := make([]storage.RichSearchMatch, 0, len(resp.Hits))
	for _, hit := range resp.Hits {
		// Parse the document JSON to extract fields
		var doc map[string]interface{}
		if err := json.Unmarshal(hit.Document, &doc); err != nil {
			continue
		}

		nodeID, _ := doc["node_id"].(string)
		typeName, _ := doc["type_name"].(string)
		typeLabel, _ := doc["type_label"].(string)
		fieldName, _ := doc["field_name"].(string)
		fieldValue, _ := doc["field_value"].(string)
		displayLabel, _ := doc["display_label"].(string)

		// Use highlighted text as excerpt if available
		excerpt := fieldValue
		if len(hit.Highlighted) > 0 {
			var highlighted map[string]interface{}
			if err := json.Unmarshal(hit.Highlighted, &highlighted); err == nil {
				if hl, ok := highlighted["field_value"].(string); ok {
					excerpt = hl
				}
			}
		}

		matches = append(matches, storage.RichSearchMatch{
			NodeID:       nodeID,
			TypeName:     typeName,
			TypeLabel:    typeLabel,
			FieldName:    fieldName,
			FieldValue:   fieldValue,
			Excerpt:      excerpt,
			Score:        float64(hit.Score),
			Strategy:     storage.StrategyMeiliSearch,
			DisplayLabel: displayLabel,
			Attributes:   doc,
		})
	}

	return matches, nil
}

// searchSemantic generates an embedding for the query and searches the vector store.
// Returns empty slice on any failure — semantic search is best-effort.
func (c *Client) searchSemantic(query string) []storage.RichSearchMatch {
	queryResult, err := c.server.embeddingService.GenerateEmbedding(query, "")
	if err != nil {
		c.server.logger.Debugw("Semantic embedding failed", "query", query, "error", err)
		return nil
	}

	queryBlob, err := c.server.embeddingService.SerializeEmbedding(queryResult.Embedding)
	if err != nil {
		c.server.logger.Debugw("Semantic serialization failed", "query", query, "error", err)
		return nil
	}

	searchResults, err := c.server.embeddingStore.SemanticSearch(queryBlob, semanticSearchLimit, semanticSearchThreshold, nil, "")
	if err != nil {
		c.server.logger.Debugw("Semantic search failed", "query", query, "error", err)
		return nil
	}

	var matches []storage.RichSearchMatch
	for _, result := range searchResults {
		if result.SourceType != "attestation" {
			continue
		}

		attestation, err := c.server.getAttestationByID(result.SourceID)
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

	// Create watcher struct — detect SE glyph (semantic query) vs AX glyph (structured query)
	watcher := &storage.Watcher{
		ID:                watcherID,
		Name:              msg.WatcherName,
		MaxFiresPerSecond: am.GetInt("watcher.max_fires_per_second"),
		Enabled:           msg.Enabled,
	}

	if msg.SemanticQuery != "" {
		watcher.ActionType = storage.ActionTypeSemanticMatch
		watcher.SemanticQuery = msg.SemanticQuery
		watcher.SemanticThreshold = msg.SemanticThreshold
		watcher.SemanticClusterID = msg.SemanticClusterID
	} else {
		watcher.AxQuery = msg.WatcherQuery
		watcher.ActionType = storage.ActionTypePython // Default to Python action
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

	// Defer reload + post-reload behind coalescing window to avoid O(N²) FFI
	// calls when N glyphs reconnect simultaneously
	c.server.reloadCoalescer.schedule(pendingUpsert{
		watcherID:     watcherID,
		semanticQuery: msg.SemanticQuery,
		watcherQuery:  msg.WatcherQuery,
		threshold:     msg.SemanticThreshold,
		clusterID:     msg.SemanticClusterID,
	})
}

// sendSystemCapabilitiesToClient sends system capability information to a newly connected client.
// This informs the frontend about available optimizations (storage backend, parser backend).
// Sends are routed through broadcast worker (thread-safe).
func (s *QNTXServer) sendSystemCapabilitiesToClient(client *Client) {
	msg := syscap.Get()

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
