// Package storage provides attestation storage with bounded limits to prevent unbounded growth.
// Default 16/64/64 storage strategy (configurable):
// - 16 attestations per (actor, context) pair
// - 64 contexts per actor
// - 64 actors per entity (subject)
package storage

import (
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

	// Negative limits are invalid; default via am/defaults.go handles "omit = use default".
	// Zero means zero (QNTX LAW): 0 limit = no attestations retained for that dimension.
	if config.ActorContextLimit < 0 {
		config.ActorContextLimit = DefaultActorContextLimit
	}
	if config.ActorContextsLimit < 0 {
		config.ActorContextsLimit = DefaultActorContextsLimit
	}
	if config.EntityActorsLimit < 0 {
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
func (s *BoundedStore) CreateAttestation(as *types.As) error {
	if err := s.store.CreateAttestation(as); err != nil {
		return errors.Wrap(err, "failed to create attestation")
	}

	// Enforce bounded storage limits after insertion
	s.enforceLimits(as)

	return nil
}

// AttestationExists checks if an attestation with the given ID exists (implements ats.AttestationStore)
func (s *BoundedStore) AttestationExists(asid string) bool {
	return s.store.AttestationExists(asid)
}

// GenerateAndCreateAttestation generates a vanity ASID and creates a self-certifying attestation (implements ats.AttestationStore)
// Note: Observer notification is handled by SQLStore.CreateAttestation (called internally)
func (s *BoundedStore) GenerateAndCreateAttestation(cmd *types.AsCommand) (*types.As, error) {
	return s.store.GenerateAndCreateAttestation(cmd)
}

// GetAttestations retrieves attestations based on filters (implements ats.AttestationStore)
func (s *BoundedStore) GetAttestations(filters ats.AttestationFilter) ([]*types.As, error) {
	return s.store.GetAttestations(filters)
}

// CreateAttestationWithLimits creates an attestation and enforces storage limits (implements ats.BoundedStore)
// Note: Observer notification is handled by SQLStore.CreateAttestation (called internally)
func (s *BoundedStore) CreateAttestationWithLimits(cmd *types.AsCommand) (*types.As, error) {
	// First create the attestation (observer notification happens in SQLStore)
	as, err := s.store.GenerateAndCreateAttestation(cmd)
	if err != nil {
		return nil, errors.Wrap(err, "failed to create attestation")
	}

	// Then enforce limits synchronously to avoid database connection issues
	s.enforceLimits(as)

	return as, nil
}

// GetStorageStats returns current storage statistics (implements ats.BoundedStore)
func (s *BoundedStore) GetStorageStats() (*ats.StorageStats, error) {
	stats := &ats.StorageStats{}

	// Combine all counts into a single query to reduce database round trips
	err := s.db.QueryRow(queryStorageStats).Scan(
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
