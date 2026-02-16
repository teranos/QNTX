package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"sync/atomic"
	"time"

	"github.com/teranos/QNTX/logger"
	"go.uber.org/zap"
)

// StorageEventsPoller polls the storage_events table and broadcasts to WebSocket clients
type StorageEventsPoller struct {
	db       *sql.DB
	server   *QNTXServer
	logger   *zap.SugaredLogger
	interval time.Duration
	lastID   int64 // Track last processed event ID

	// Accumulated eviction counters, drained by the Pulse ticker for periodic summaries
	evictionEvents       atomic.Int64
	evictedAttestations  atomic.Int64
}

// NewStorageEventsPoller creates a new storage events poller
func NewStorageEventsPoller(db *sql.DB, server *QNTXServer, logger *zap.SugaredLogger) *StorageEventsPoller {
	// Initialize lastID to current max to avoid broadcasting historical events
	var lastID int64
	err := db.QueryRow("SELECT COALESCE(MAX(id), 0) FROM storage_events").Scan(&lastID)
	if err != nil {
		logger.Warnw("Failed to get last storage event ID, starting from 0", "error", err)
		lastID = 0
	}

	return &StorageEventsPoller{
		db:       db,
		server:   server,
		logger:   logger,
		interval: 2 * time.Second, // Poll every 2 seconds
		lastID:   lastID,
	}
}

// Start begins polling for storage events
func (p *StorageEventsPoller) Start(ctx context.Context) {
	ticker := time.NewTicker(p.interval)
	defer ticker.Stop()

	p.logger.Debugw("Storage events poller started", "interval", p.interval)

	for {
		select {
		case <-ctx.Done():
			p.logger.Debugw("Storage events poller stopped")
			return
		case <-ticker.C:
			p.pollEvents()
		}
	}
}

// pollEvents checks for new storage events and broadcasts them
func (p *StorageEventsPoller) pollEvents() {
	// Query for new events since last poll
	rows, err := p.db.Query(`
		SELECT id, event_type, actor, context, entity, deletions_count, limit_value, timestamp, eviction_details
		FROM storage_events
		WHERE id > ?
		ORDER BY id ASC
	`, p.lastID)
	if err != nil {
		p.logger.Warnw("Failed to query storage events", "error", err)
		return
	}
	defer rows.Close()

	eventsProcessed := 0
	for rows.Next() {
		var (
			id              int64
			eventType       string
			actor           sql.NullString
			context         sql.NullString
			entity          sql.NullString
			deletionsCount  int
			limitValue      sql.NullInt64
			timestamp       string
			evictionDetails sql.NullString
		)

		if err := rows.Scan(&id, &eventType, &actor, &context, &entity, &deletionsCount, &limitValue, &timestamp, &evictionDetails); err != nil {
			p.logger.Warnw("Failed to scan storage event", "error", err)
			continue
		}

		// Broadcast notification based on event type
		if eventType == "storage_warning" {
			limit := int(limitValue.Int64)
			if !limitValue.Valid {
				limit = 16 // Fallback to default for old events
			}
			p.broadcastWarning(actor.String, context.String, deletionsCount, limit)
		} else {
			limit := int(limitValue.Int64)
			if !limitValue.Valid {
				// Fallback to defaults for old events
				limit = getDefaultLimit(eventType)
			}
			p.broadcastEviction(eventType, actor.String, context.String, entity.String, deletionsCount, limit, evictionDetails.String)
		}

		// Update last processed ID
		p.lastID = id
		eventsProcessed++
	}

	if eventsProcessed > 0 {
		p.logger.Debugw("Processed storage events", "count", eventsProcessed, "last_id", p.lastID)
	}
}

// broadcastEviction sends an eviction notification to WebSocket clients
func (p *StorageEventsPoller) broadcastEviction(eventType, actor, context, entity string, deletionsCount, limit int, evictionDetailsJSON string) {
	// Parse eviction details if available
	var detailsMap map[string]interface{}
	if evictionDetailsJSON != "" {
		if err := json.Unmarshal([]byte(evictionDetailsJSON), &detailsMap); err != nil {
			p.logger.Debugw("Failed to parse eviction details JSON",
				"error", err,
				"event_type", eventType,
				"raw_json", evictionDetailsJSON)
			// Continue with nil detailsMap - the rest of the code handles this gracefully
		}
	}

	// Format message based on event type
	var message string
	switch eventType {
	case "actor_context_limit":
		message = fmt.Sprintf("Evicted %d old attestations for %s/%s (limit: %d)", deletionsCount, actor, context, limit)
	case "actor_contexts_limit":
		message = fmt.Sprintf("Evicted %d attestations for actor %s (contexts limit: %d)", deletionsCount, actor, limit)
	case "entity_actors_limit":
		message = fmt.Sprintf("Evicted %d attestations for entity %s (actors limit: %d)", deletionsCount, entity, limit)
	default:
		message = fmt.Sprintf("Evicted %d attestations (%s)", deletionsCount, eventType)
	}

	// Build log fields with eviction details
	logFields := []interface{}{
		"event_type", eventType,
		"actor", actor,
		"context", context,
		"entity", entity,
		"deletions_count", deletionsCount,
	}
	if detailsMap != nil {
		logFields = append(logFields, "eviction_details", detailsMap)
	}

	logger.AddDBSymbol(p.logger).Debugw("Storage eviction", logFields...)

	p.evictionEvents.Add(1)
	p.evictedAttestations.Add(int64(deletionsCount))

	// Broadcast as storage_eviction message
	msg := map[string]interface{}{
		"type":            "storage_eviction",
		"event_type":      eventType,
		"actor":           actor,
		"context":         context,
		"entity":          entity,
		"deletions_count": deletionsCount,
		"message":         message,
	}
	if detailsMap != nil {
		msg["eviction_details"] = detailsMap
	}

	p.server.broadcastMessage(msg)
}

// broadcastWarning sends a storage warning notification to WebSocket clients
func (p *StorageEventsPoller) broadcastWarning(actor, context string, current, limit int) {
	// For warnings, deletionsCount field contains current attestation count
	fillPercent := float64(current) / float64(limit)

	logger.AddDBSymbol(p.logger).Infow("Storage warning",
		"actor", actor,
		"context", context,
		"fill_percent", int(fillPercent*100),
		"current", current,
		"limit", limit,
	)

	// Broadcast using existing storage_warning message type
	msg := map[string]interface{}{
		"type":            "storage_warning",
		"actor":           actor,
		"context":         context,
		"current":         current,
		"limit":           limit,
		"fill_percent":    fillPercent,
		"time_until_full": "unknown", // Could be calculated if needed
		"timestamp":       time.Now().Unix(),
	}

	p.server.broadcastMessage(msg)
}

// DrainEvictionCounts atomically reads and resets the accumulated eviction counters.
// Returns (eviction events, attestations evicted). Both zero means no evictions since last drain.
func (p *StorageEventsPoller) DrainEvictionCounts() (events int, attestations int) {
	return int(p.evictionEvents.Swap(0)), int(p.evictedAttestations.Swap(0))
}

// getDefaultLimit returns the default limit for a given event type
// Used as fallback for old events that don't have limit_value stored
func getDefaultLimit(eventType string) int {
	switch eventType {
	case "actor_context_limit":
		return 16 // DefaultActorContextLimit
	case "actor_contexts_limit":
		return 64 // DefaultActorContextsLimit
	case "entity_actors_limit":
		return 64 // DefaultEntityActorsLimit
	default:
		return 0
	}
}
