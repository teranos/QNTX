package storage

import (
	"time"
)

// logStorageWarning records a storage warning (approaching limit) for observability
func (bs *BoundedStore) logStorageWarning(actor, context string, current, limit int) {
	timestamp := time.Now().Format(time.RFC3339)

	// Log to database for historical tracking and UI notification
	_, err := bs.db.Exec(`
		INSERT INTO storage_events (event_type, actor, context, entity, deletions_count, limit_value, timestamp)
		VALUES (?, ?, ?, NULL, ?, ?, ?)`,
		"storage_warning",
		nullIfEmpty(actor),
		nullIfEmpty(context),
		current, // Store current count in deletions_count field (reusing existing column)
		limit,
		timestamp,
	)
	if err != nil {
		// Don't fail the operation if logging fails, but warn
		if bs.logger != nil {
			bs.logger.Warnw("Failed to log storage warning",
				"actor", actor,
				"context", context,
				"error", err,
			)
		}
	}
}

// nullIfEmpty returns nil for empty strings (for nullable SQL columns)
func nullIfEmpty(s string) interface{} {
	if s == "" {
		return nil
	}
	return s
}
