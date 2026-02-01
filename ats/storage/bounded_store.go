// Package storage provides attestation storage with bounded limits to prevent unbounded growth.
// Default 16/64/64 storage strategy (configurable):
// - 16 attestations per (actor, context) pair
// - 64 contexts per actor
// - 64 actors per entity (subject)
package storage

import (
	"context"
	"database/sql"
	"sync"
	"time"

	"go.uber.org/zap"

	"github.com/teranos/QNTX/ats"
	"github.com/teranos/QNTX/ats/types"
	"github.com/teranos/QNTX/errors"
)

const (
	// SQL query to get storage statistics (counts of attestations, actors, subjects, contexts)
	queryStorageStats = `
		SELECT
			COUNT(*) as total_attestations,
			COUNT(DISTINCT json_extract(actors, '$[0]')) as unique_actors,
			COUNT(DISTINCT json_extract(subjects, '$')) as unique_subjects,
			COUNT(DISTINCT json_extract(contexts, '$')) as unique_contexts
		FROM attestations`
)

// BoundedStore implements configurable storage limits for attestations
type BoundedStore struct {
	db     *sql.DB
	store  *SQLStore
	logger *zap.SugaredLogger
	config *BoundedStoreConfig

	// Cache for type definitions (used by rich search)
	typeFieldsCache     map[string][]string
	typeFieldsCacheLock sync.RWMutex
	typeFieldsCacheTime time.Time
}

// NewBoundedStore creates a new bounded storage manager with default limits (16/64/64)
func NewBoundedStore(db *sql.DB, logger *zap.SugaredLogger) *BoundedStore {
	return NewBoundedStoreWithConfig(db, logger, nil)
}

// NewBoundedStoreWithConfig creates a bounded storage manager with custom limits
// Pass nil config to use defaults (16/64/64)
func NewBoundedStoreWithConfig(db *sql.DB, logger *zap.SugaredLogger, config *BoundedStoreConfig) *BoundedStore {
	if config == nil {
		config = DefaultBoundedStoreConfig()
	}

	// Validate config: zero or negative limits are invalid (use positive limits)
	if config.ActorContextLimit <= 0 {
		config.ActorContextLimit = DefaultActorContextLimit
	}
	if config.ActorContextsLimit <= 0 {
		config.ActorContextsLimit = DefaultActorContextsLimit
	}
	if config.EntityActorsLimit <= 0 {
		config.EntityActorsLimit = DefaultEntityActorsLimit
	}

	return &BoundedStore{
		db:     db,
		store:  NewSQLStore(db, logger),
		logger: logger,
		config: config,
	}
}

// CreateAttestation inserts a new attestation into the database with quota enforcement (implements ats.AttestationStore)
// Note: Observer notification is handled by SQLStore.CreateAttestation
func (bs *BoundedStore) CreateAttestation(ctx context.Context, as *types.As) error {
	if err := bs.store.CreateAttestation(ctx, as); err != nil {
		return errors.Wrap(err, "failed to create attestation")
	}

	// Enforce bounded storage limits after insertion
	bs.enforceLimits(as)

	return nil
}

// AttestationExists checks if an attestation with the given ID exists (implements ats.AttestationStore)
func (bs *BoundedStore) AttestationExists(ctx context.Context, asid string) bool {
	return bs.store.AttestationExists(ctx, asid)
}

// GenerateAndCreateAttestation generates a vanity ASID and creates a self-certifying attestation (implements ats.AttestationStore)
// Note: Observer notification is handled by SQLStore.CreateAttestation (called internally)
func (bs *BoundedStore) GenerateAndCreateAttestation(ctx context.Context, cmd *types.AsCommand) (*types.As, error) {
	return bs.store.GenerateAndCreateAttestation(ctx, cmd)
}

// GetAttestations retrieves attestations based on filters (implements ats.AttestationStore)
func (bs *BoundedStore) GetAttestations(ctx context.Context, filters ats.AttestationFilter) ([]*types.As, error) {
	return bs.store.GetAttestations(ctx, filters)
}

// CreateAttestationWithLimits creates an attestation and enforces storage limits (implements ats.BoundedStore)
// Note: Observer notification is handled by SQLStore.CreateAttestation (called internally)
func (bs *BoundedStore) CreateAttestationWithLimits(ctx context.Context, cmd *types.AsCommand) (*types.As, error) {
	// First create the attestation (observer notification happens in SQLStore)
	as, err := bs.store.GenerateAndCreateAttestation(ctx, cmd)
	if err != nil {
		return nil, errors.Wrap(err, "failed to create attestation")
	}

	// Then enforce limits synchronously to avoid database connection issues
	bs.enforceLimits(as)

	return as, nil
}

// GetStorageStats returns current storage statistics (implements ats.BoundedStore)
func (bs *BoundedStore) GetStorageStats(ctx context.Context) (*ats.StorageStats, error) {
	stats := &ats.StorageStats{}

	// Combine all counts into a single query to reduce database round trips
	err := bs.db.QueryRowContext(ctx, queryStorageStats).Scan(
		&stats.TotalAttestations,
		&stats.UniqueActors,
		&stats.UniqueSubjects,
		&stats.UniqueContexts,
	)
	if err != nil {
		return nil, errors.Wrap(err, "failed to query storage stats")
	}

	return stats, nil
}
