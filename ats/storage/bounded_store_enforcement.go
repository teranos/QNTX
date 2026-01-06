package storage

import (
	"github.com/teranos/QNTX/ats/types"
	"github.com/teranos/QNTX/errors"
)

const (
	// SQL query to get actor contexts with usage counts for limit enforcement
	queryActorContexts = `
		SELECT DISTINCT json_extract(contexts, '$') as context_array, COUNT(*) as usage_count
		FROM attestations
		WHERE EXISTS (
			SELECT 1 FROM json_each(attestations.actors)
			WHERE value = ?
		)
		GROUP BY context_array
		ORDER BY usage_count ASC`

	// SQL query to get entity actors with most recent timestamps for limit enforcement
	queryEntityActors = `
		SELECT value as actor, MAX(timestamp) as last_seen
		FROM attestations, json_each(actors)
		WHERE EXISTS (
			SELECT 1 FROM json_each(attestations.subjects)
			WHERE value = ?
		)
		GROUP BY actor
		ORDER BY last_seen ASC`
)

// enforceLimits applies the configurable storage limits
func (bs *BoundedStore) enforceLimits(as *types.As) {
	if as == nil {
		if bs.logger != nil {
			bs.logger.Warn("enforceLimits called with nil attestation")
		}
		return
	}

	// 1. Enforce N attestations per (actor, context) - remove oldest
	for _, actor := range as.Actors {
		for _, context := range as.Contexts {
			if err := bs.enforceActorContextLimit(actor, context); err != nil {
				// Log error but don't fail the operation
				if bs.logger != nil {
					bs.logger.Warnw("Failed to enforce actor-context limit",
						"actor", actor,
						"context", context,
						"error", err,
					)
				}
			}
		}
	}

	// 2. Enforce N contexts per actor - remove least used
	for _, actor := range as.Actors {
		if err := bs.enforceActorContextsLimit(actor); err != nil {
			if bs.logger != nil {
				bs.logger.Warnw("Failed to enforce actor contexts limit",
					"actor", actor,
					"error", err,
				)
			}
		}
	}

	// 3. Enforce N actors per entity - remove least recent
	for _, subject := range as.Subjects {
		if err := bs.enforceEntityActorsLimit(subject); err != nil {
			if bs.logger != nil {
				bs.logger.Warnw("Failed to enforce entity actors limit",
					"subject", subject,
					"error", err,
				)
			}
		}
	}
}

// enforceActorContextLimit keeps only N most recent attestations for this actor+context
func (bs *BoundedStore) enforceActorContextLimit(actor, context string) error {
	limit := bs.config.ActorContextLimit

	// Count current attestations for this actor+context
	var count int
	err := bs.db.QueryRow(AttestationCountByActorContextQuery, actor, context).Scan(&count)
	if err != nil {
		return errors.Wrap(err, "failed to count attestations")
	}

	// If over limit, delete oldest ones
	if count > limit {
		deleteCount := count - limit
		_, err = bs.db.Exec(AttestationDeleteOldestByActorContextQuery, actor, context, deleteCount)
		if err != nil {
			return errors.Wrap(err, "failed to delete old attestations")
		}

		// Log enforcement event for observability
		bs.logStorageEvent("actor_context_limit", actor, context, "", deleteCount, limit)
	}

	return nil
}

// enforceActorContextsLimit keeps only N most used contexts for this actor
func (bs *BoundedStore) enforceActorContextsLimit(actor string) error {
	limit := bs.config.ActorContextsLimit

	// Get all contexts for this actor with usage counts
	rows, err := bs.db.Query(queryActorContexts, actor)
	if err != nil {
		return errors.Wrap(err, "failed to query actor contexts")
	}
	defer rows.Close()

	type contextUsage struct {
		contextArray string
		usageCount   int
	}

	var contexts []contextUsage
	for rows.Next() {
		var cu contextUsage
		err := rows.Scan(&cu.contextArray, &cu.usageCount)
		if err != nil {
			return errors.Wrap(err, "failed to scan context usage")
		}
		contexts = append(contexts, cu)
	}

	if err = rows.Err(); err != nil {
		return errors.Wrap(err, "error iterating over contexts")
	}

	// If over limit, delete attestations for least used contexts
	if len(contexts) > limit {
		contextsToDelete := contexts[:len(contexts)-limit] // Keep most used (at end)
		totalDeleted := 0

		for _, cu := range contextsToDelete {
			// Delete all attestations with this context array
			result, err := bs.db.Exec(
				`DELETE FROM attestations
				WHERE EXISTS (
					SELECT 1 FROM json_each(attestations.actors)
					WHERE value = ?
				) AND contexts = ?`,
				actor, cu.contextArray,
			)
			if err != nil {
				return errors.Wrapf(err, "failed to delete attestations for context %s", cu.contextArray)
			}

			// Track deletions for telemetry
			if rowsAffected, err := result.RowsAffected(); err == nil {
				totalDeleted += int(rowsAffected)
			}
		}

		// Log enforcement event for observability
		if totalDeleted > 0 {
			bs.logStorageEvent("actor_contexts_limit", actor, "", "", totalDeleted, limit)
		}
	}

	return nil
}

// enforceEntityActorsLimit keeps only N most recent actors for this entity
func (bs *BoundedStore) enforceEntityActorsLimit(entity string) error {
	limit := bs.config.EntityActorsLimit

	// Get all actors for this entity with most recent timestamps
	rows, err := bs.db.Query(queryEntityActors, entity)
	if err != nil {
		return errors.Wrap(err, "failed to query entity actors")
	}
	defer rows.Close()

	type actorInfo struct {
		actor    string
		lastSeen string
	}

	var actors []actorInfo
	for rows.Next() {
		var ai actorInfo
		err := rows.Scan(&ai.actor, &ai.lastSeen)
		if err != nil {
			return errors.Wrap(err, "failed to scan actor info")
		}
		actors = append(actors, ai)
	}

	if err = rows.Err(); err != nil {
		return errors.Wrap(err, "error iterating over actors")
	}

	// If over limit, delete attestations for least recent actors
	if len(actors) > limit {
		actorsToDelete := actors[:len(actors)-limit] // Keep most recent (at end)
		totalDeleted := 0

		for _, ai := range actorsToDelete {
			// Delete all attestations by this actor that mention this entity
			result, err := bs.db.Exec(
				`DELETE FROM attestations
				WHERE EXISTS (
					SELECT 1 FROM json_each(attestations.actors)
					WHERE value = ?
				) AND EXISTS (
					SELECT 1 FROM json_each(attestations.subjects)
					WHERE value = ?
				)`,
				ai.actor, entity,
			)
			if err != nil {
				return errors.Wrapf(err, "failed to delete attestations for actor %s", ai.actor)
			}

			// Track deletions for telemetry
			if rowsAffected, err := result.RowsAffected(); err == nil {
				totalDeleted += int(rowsAffected)
			}
		}

		// Log enforcement event for observability
		if totalDeleted > 0 {
			bs.logStorageEvent("entity_actors_limit", "", "", entity, totalDeleted, limit)
		}
	}

	return nil
}
