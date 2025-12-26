// Package storage provides attestation storage with bounded limits to prevent unbounded growth.
// It implements the 16/64/64 storage strategy:
// - 16 attestations per (actor, context) pair
// - 64 contexts per actor
// - 64 actors per entity (subject)
//
// TODO(#181): Make storage limits configurable instead of hardcoded.
// See: https://github.com/sbvh-nl/qntx/issues/181
package storage

import (
	"database/sql"
	"fmt"

	"go.uber.org/zap"

	"github.com/teranos/QNTX/ats"
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

	// SQL query to get storage statistics (counts of attestations, actors, subjects, contexts)
	queryStorageStats = `
		SELECT
			COUNT(*) as total_attestations,
			COUNT(DISTINCT actor) as unique_actors,
			COUNT(DISTINCT json_extract(subjects, '$')) as unique_subjects,
			COUNT(DISTINCT json_extract(contexts, '$')) as unique_contexts
		FROM attestations`
)

// BoundedStore implements the 16/64/64 storage limits for attestations
// - 16 attestations per (actor, context) pair
// - 64 contexts per actor
// - 64 actors per entity (subject)
type BoundedStore struct {
	db     *sql.DB
	store  *SQLStore
	logger *zap.SugaredLogger
}

// NewBoundedStore creates a new bounded storage manager
func NewBoundedStore(db *sql.DB, logger *zap.SugaredLogger) *BoundedStore {
	return &BoundedStore{
		db:     db,
		store:  NewSQLStore(db, logger),
		logger: logger,
	}
}

// CreateAttestation inserts a new attestation into the database with quota enforcement (implements ats.AttestationStore)
func (bs *BoundedStore) CreateAttestation(as *types.As) error {
	if err := bs.store.CreateAttestation(as); err != nil {
		return err
	}

	// Enforce bounded storage limits after insertion
	bs.enforceLimits(as)

	return nil
}

// AttestationExists checks if an attestation with the given ID exists (implements ats.AttestationStore)
func (bs *BoundedStore) AttestationExists(asid string) bool {
	return bs.store.AttestationExists(asid)
}

// GenerateAndCreateAttestation generates a vanity ASID and creates a self-certifying attestation (implements ats.AttestationStore)
func (bs *BoundedStore) GenerateAndCreateAttestation(cmd *types.AsCommand) (*types.As, error) {
	return bs.store.GenerateAndCreateAttestation(cmd)
}

// GetAttestations retrieves attestations based on filters (implements ats.AttestationStore)
func (bs *BoundedStore) GetAttestations(filters ats.AttestationFilter) ([]*types.As, error) {
	return bs.store.GetAttestations(filters)
}

// CreateAttestationWithLimits creates an attestation and enforces storage limits (implements ats.BoundedStore)
func (bs *BoundedStore) CreateAttestationWithLimits(cmd *types.AsCommand) (*types.As, error) {
	// First create the attestation
	as, err := bs.store.GenerateAndCreateAttestation(cmd)
	if err != nil {
		return nil, fmt.Errorf("failed to create attestation: %w", err)
	}

	// Then enforce limits synchronously to avoid database connection issues
	bs.enforceLimits(as)

	return as, nil
}

// GetStorageStats returns current storage statistics (implements ats.BoundedStore)
func (bs *BoundedStore) GetStorageStats() (*ats.StorageStats, error) {
	stats := &ats.StorageStats{}

	// Combine all counts into a single query to reduce database round trips
	err := bs.db.QueryRow(queryStorageStats).Scan(
		&stats.TotalAttestations,
		&stats.UniqueActors,
		&stats.UniqueSubjects,
		&stats.UniqueContexts,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to query storage stats: %w", err)
	}

	return stats, nil
}

// enforceLimits applies the 16/64/64 storage limits
func (bs *BoundedStore) enforceLimits(as *types.As) {
	if as == nil {
		if bs.logger != nil {
			bs.logger.Warn("enforceLimits called with nil attestation")
		}
		return
	}

	// 1. Enforce 16 attestations per (actor, context) - remove oldest
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

	// 2. Enforce 64 contexts per actor - remove least used
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

	// 3. Enforce 64 actors per entity - remove least recent
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

// enforceActorContextLimit keeps only 16 most recent attestations for this actor+context
func (bs *BoundedStore) enforceActorContextLimit(actor, context string) error {
	// TODO(#181): Make this limit configurable (currently hardcoded to 16)
	const limit = 16

	// Count current attestations for this actor+context
	var count int
	err := bs.db.QueryRow(AttestationCountByActorContextQuery, actor, context).Scan(&count)
	if err != nil {
		return fmt.Errorf("failed to count attestations: %w", err)
	}

	// If over limit, delete oldest ones
	if count > limit {
		deleteCount := count - limit
		_, err = bs.db.Exec(AttestationDeleteOldestByActorContextQuery, actor, context, deleteCount)
		if err != nil {
			return fmt.Errorf("failed to delete old attestations: %w", err)
		}
	}

	return nil
}

// enforceActorContextsLimit keeps only 64 most used contexts for this actor
func (bs *BoundedStore) enforceActorContextsLimit(actor string) error {
	// TODO(#181): Make this limit configurable (currently hardcoded to 64)
	const limit = 64

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

		for _, cu := range contextsToDelete {
			// Delete all attestations with this context array
			_, err = bs.db.Exec(
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
		}
	}

	return nil
}

// enforceEntityActorsLimit keeps only 64 most recent actors for this entity
func (bs *BoundedStore) enforceEntityActorsLimit(entity string) error {
	// TODO(#181): Make this limit configurable (currently hardcoded to 64)
	const limit = 64

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

		for _, ai := range actorsToDelete {
			// Delete all attestations by this actor that mention this entity
			_, err = bs.db.Exec(
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
		}
	}

	return nil
}
