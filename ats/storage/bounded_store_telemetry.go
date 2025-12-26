package storage

import "time"

// logStorageEvent records a bounded storage enforcement event for observability
func (bs *BoundedStore) logStorageEvent(eventType, actor, context, entity string, deletionsCount int) {
	timestamp := time.Now().Format(time.RFC3339)

	// Log to database for historical tracking
	_, err := bs.db.Exec(`
		INSERT INTO storage_events (event_type, actor, context, entity, deletions_count, timestamp)
		VALUES (?, ?, ?, ?, ?, ?)`,
		eventType,
		nullIfEmpty(actor),
		nullIfEmpty(context),
		nullIfEmpty(entity),
		deletionsCount,
		timestamp,
	)
	if err != nil {
		// Don't fail the operation if logging fails, but warn
		if bs.logger != nil {
			bs.logger.Warnw("Failed to log storage event",
				"event_type", eventType,
				"error", err,
			)
		}
	}

	// Also log to structured logger for real-time visibility
	if bs.logger != nil {
		bs.logger.Infow("Bounded storage limit enforced",
			"event_type", eventType,
			"actor", actor,
			"context", context,
			"entity", entity,
			"deletions", deletionsCount,
		)
	}
}

// nullIfEmpty returns nil for empty strings (for nullable SQL columns)
func nullIfEmpty(s string) interface{} {
	if s == "" {
		return nil
	}
	return s
}
