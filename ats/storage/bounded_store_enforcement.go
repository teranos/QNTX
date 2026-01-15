package storage

import (
	"fmt"

	"github.com/teranos/QNTX/ats/types"
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
		return fmt.Errorf("failed to count attestations: %w", err)
	}

	// If over limit, delete oldest ones
	if count > limit {
		deleteCount := count - limit

		// Collect sample data before deletion
		evictionDetails := &EvictionDetails{
			SamplePredicates: make([]string, 0),
			SampleSubjects:   make([]string, 0),
		}
		sampleRows, _ := bs.db.Query(
			`SELECT predicates, subjects, timestamp FROM attestations, json_each(actors) as a, json_each(contexts) as c
			WHERE a.value = ? AND c.value = ?
			ORDER BY timestamp ASC
			LIMIT 3`,
			actor, context,
		)
		if sampleRows != nil {
			for sampleRows.Next() {
				var predJSON, subjJSON, timestamp string
				if err := sampleRows.Scan(&predJSON, &subjJSON, &timestamp); err == nil {
					evictionDetails.SamplePredicates = append(evictionDetails.SamplePredicates, predJSON)
					evictionDetails.SampleSubjects = append(evictionDetails.SampleSubjects, subjJSON)
					if evictionDetails.LastSeen == "" || timestamp > evictionDetails.LastSeen {
						evictionDetails.LastSeen = timestamp
					}
				}
			}
			sampleRows.Close()
		}

		_, err = bs.db.Exec(AttestationDeleteOldestByActorContextQuery, actor, context, deleteCount)
		if err != nil {
			return fmt.Errorf("failed to delete old attestations: %w", err)
		}

		// Log enforcement event with details
		bs.logStorageEventWithDetails("actor_context_limit", actor, context, "", deleteCount, limit, evictionDetails)
	}

	return nil
}

// enforceActorContextsLimit keeps only N most used contexts for this actor
func (bs *BoundedStore) enforceActorContextsLimit(actor string) error {
	limit := bs.config.ActorContextsLimit

	// Get all contexts for this actor with usage counts
	rows, err := bs.db.Query(queryActorContexts, actor)
	if err != nil {
		return fmt.Errorf("failed to query actor contexts: %w", err)
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
			return fmt.Errorf("failed to scan context usage: %w", err)
		}
		contexts = append(contexts, cu)
	}

	if err = rows.Err(); err != nil {
		return fmt.Errorf("error iterating over contexts: %w", err)
	}

	// If over limit, delete attestations for least used contexts
	if len(contexts) > limit {
		contextsToDelete := contexts[:len(contexts)-limit] // Keep most used (at end)
		totalDeleted := 0

		// Collect eviction details
		evictionDetails := &EvictionDetails{
			EvictedContexts:  make([]string, 0, len(contextsToDelete)),
			SamplePredicates: make([]string, 0),
			SampleSubjects:   make([]string, 0),
		}

		for _, cu := range contextsToDelete {
			evictionDetails.EvictedContexts = append(evictionDetails.EvictedContexts, cu.contextArray)

			// Collect sample data from first context being evicted
			if len(evictionDetails.SamplePredicates) == 0 {
				sampleRows, _ := bs.db.Query(
					`SELECT predicates, subjects, timestamp FROM attestations
					WHERE EXISTS (
						SELECT 1 FROM json_each(attestations.actors)
						WHERE value = ?
					) AND contexts = ?
					LIMIT 3`,
					actor, cu.contextArray,
				)
				if sampleRows != nil {
					for sampleRows.Next() {
						var predJSON, subjJSON, timestamp string
						if err := sampleRows.Scan(&predJSON, &subjJSON, &timestamp); err == nil {
							evictionDetails.SamplePredicates = append(evictionDetails.SamplePredicates, predJSON)
							evictionDetails.SampleSubjects = append(evictionDetails.SampleSubjects, subjJSON)
							if evictionDetails.LastSeen == "" || timestamp > evictionDetails.LastSeen {
								evictionDetails.LastSeen = timestamp
							}
						}
					}
					sampleRows.Close()
				}
			}

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
				return fmt.Errorf("failed to delete attestations for context %s: %w", cu.contextArray, err)
			}

			// Track deletions for telemetry
			if rowsAffected, err := result.RowsAffected(); err == nil {
				totalDeleted += int(rowsAffected)
			}
		}

		// Log enforcement event with details
		if totalDeleted > 0 {
			bs.logStorageEventWithDetails("actor_contexts_limit", actor, "", "", totalDeleted, limit, evictionDetails)
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
		return fmt.Errorf("failed to query entity actors: %w", err)
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
			return fmt.Errorf("failed to scan actor info: %w", err)
		}
		actors = append(actors, ai)
	}

	if err = rows.Err(); err != nil {
		return fmt.Errorf("error iterating over actors: %w", err)
	}

	// If over limit, delete attestations for least recent actors
	if len(actors) > limit {
		actorsToDelete := actors[:len(actors)-limit] // Keep most recent (at end)
		totalDeleted := 0

		// Collect eviction details for all actors being evicted
		evictionDetails := &EvictionDetails{
			EvictedActors:    make([]string, 0, len(actorsToDelete)),
			SamplePredicates: make([]string, 0),
			SampleSubjects:   make([]string, 0),
		}

		for _, ai := range actorsToDelete {
			evictionDetails.EvictedActors = append(evictionDetails.EvictedActors, ai.actor)
			if evictionDetails.LastSeen == "" || ai.lastSeen > evictionDetails.LastSeen {
				evictionDetails.LastSeen = ai.lastSeen
			}

			// Collect sample data from attestations being evicted (first actor only, to limit output)
			if len(evictionDetails.SamplePredicates) == 0 {
				sampleRows, _ := bs.db.Query(
					`SELECT predicates, subjects FROM attestations
					WHERE EXISTS (
						SELECT 1 FROM json_each(attestations.actors)
						WHERE value = ?
					) AND EXISTS (
						SELECT 1 FROM json_each(attestations.subjects)
						WHERE value = ?
					) LIMIT 3`,
					ai.actor, entity,
				)
				if sampleRows != nil {
					for sampleRows.Next() {
						var predJSON, subjJSON string
						if err := sampleRows.Scan(&predJSON, &subjJSON); err == nil {
							evictionDetails.SamplePredicates = append(evictionDetails.SamplePredicates, predJSON)
							evictionDetails.SampleSubjects = append(evictionDetails.SampleSubjects, subjJSON)
						}
					}
					sampleRows.Close()
				}
			}

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
				return fmt.Errorf("failed to delete attestations for actor %s: %w", ai.actor, err)
			}

			// Track deletions for telemetry
			if rowsAffected, err := result.RowsAffected(); err == nil {
				totalDeleted += int(rowsAffected)
			}
		}

		// Log enforcement event with detailed eviction data
		if totalDeleted > 0 {
			bs.logStorageEventWithDetails("entity_actors_limit", "", "", entity, totalDeleted, limit, evictionDetails)
		}
	}

	return nil
}
