package storage

import "time"

// logStorageEvent records a bounded storage enforcement event for observability
func (bs *BoundedStore) logStorageEvent(eventType, actor, context, entity string, deletionsCount, limitValue int) {
	timestamp := time.Now().Format(time.RFC3339)

	// Log to database for historical tracking
	_, err := bs.db.Exec(`
		INSERT INTO storage_events (event_type, actor, context, entity, deletions_count, limit_value, timestamp)
		VALUES (?, ?, ?, ?, ?, ?, ?)`,
		eventType,
		nullIfEmpty(actor),
		nullIfEmpty(context),
		nullIfEmpty(entity),
		deletionsCount,
		limitValue,
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
		// Build human-readable message with key details
		msg := "Bounded storage limit enforced"

		// Add limit type for clarity
		switch eventType {
		case "actor_context_limit":
			msg += " (attestations per actor-context)"
		case "actor_contexts_limit":
			msg += " (contexts per actor)"
		case "entity_actors_limit":
			msg += " (actors per entity)"
		}

		// Always show actor/context/entity (use <all> for empty)
		if actor != "" {
			msg += " actor=" + actor
		} else {
			msg += " actor=<all>"
		}

		if context != "" {
			msg += " context=" + context
		} else {
			msg += " context=<all>"
		}

		if entity != "" {
			msg += " entity=" + entity
		} else {
			msg += " entity=<all>"
		}

		if deletionsCount > 0 {
			msg += " (deleted oldest)"
		}

		bs.logger.Infow(msg,
			"event_type", eventType,
			"actor", actor,
			"context", context,
			"entity", entity,
			"deletions", deletionsCount,
			"limit", limitValue,
		)
	}
}

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
