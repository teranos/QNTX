package server

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"go.uber.org/zap"
)

// StorageEventsPoller polls the storage_events table and broadcasts to WebSocket clients
type StorageEventsPoller struct {
	db       *sql.DB
	server   *QNTXServer
	logger   *zap.SugaredLogger
	interval time.Duration
	lastID   int64 // Track last processed event ID
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
		SELECT id, event_type, actor, context, entity, deletions_count, timestamp
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
			timestamp       string
		)

		if err := rows.Scan(&id, &eventType, &actor, &context, &entity, &deletionsCount, &timestamp); err != nil {
			p.logger.Warnw("Failed to scan storage event", "error", err)
			continue
		}

		// Broadcast notification based on event type
		if eventType == "storage_warning" {
			p.broadcastWarning(actor.String, context.String, deletionsCount)
		} else {
			p.broadcastEviction(eventType, actor.String, context.String, entity.String, deletionsCount)
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
func (p *StorageEventsPoller) broadcastEviction(eventType, actor, context, entity string, deletionsCount int) {
	// Format message based on event type
	var message string
	switch eventType {
	case "actor_context_limit":
		message = fmt.Sprintf("Evicted %d old attestations for %s/%s (limit: 16)", deletionsCount, actor, context)
	case "actor_contexts_limit":
		message = fmt.Sprintf("Evicted %d attestations for actor %s (contexts limit: 64)", deletionsCount, actor)
	case "entity_actors_limit":
		message = fmt.Sprintf("Evicted %d attestations for entity %s (actors limit: 64)", deletionsCount, entity)
	default:
		message = fmt.Sprintf("Evicted %d attestations (%s)", deletionsCount, eventType)
	}

	p.logger.Infow(fmt.Sprintf("⊔ Storage eviction: %s - %s/%s (%d deleted)",
		eventType, actor, context, deletionsCount))

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

	p.server.broadcastMessage(msg)
}

// broadcastWarning sends a storage warning notification to WebSocket clients
func (p *StorageEventsPoller) broadcastWarning(actor, context string, current int) {
	// For warnings, deletionsCount field contains current attestation count
	limit := 16 // Default ActorContextLimit
	fillPercent := float64(current) / float64(limit)

	p.logger.Infow(fmt.Sprintf("⊔ Storage warning: %s/%s at %d%% (%d/%d)",
		actor, context, int(fillPercent*100), current, limit))

	// Broadcast using existing storage_warning message type
	msg := map[string]interface{}{
		"type":           "storage_warning",
		"actor":          actor,
		"context":        context,
		"current":        current,
		"limit":          limit,
		"fill_percent":   fillPercent,
		"time_until_full": "unknown", // Could be calculated if needed
		"timestamp":      time.Now().Unix(),
	}

	p.server.broadcastMessage(msg)
}
