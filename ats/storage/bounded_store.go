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
	"github.com/teranos/errors"
)

// BoundedStore implements configurable storage limits for attestations
type BoundedStore struct {
	db     *sql.DB
	store  ats.AttestationStore
	logger *zap.SugaredLogger
	config *BoundedStoreConfig

	// Cache for type definitions (used by rich search)
	typeFieldsCache     map[string][]string
	typeFieldsCacheLock sync.RWMutex
	typeFieldsCacheTime time.Time
}

// NewBoundedStore creates a new bounded storage manager with default limits (16/64/64).
// store may be nil when only enforcement/rich-field methods are needed.
func NewBoundedStore(db *sql.DB, store ats.AttestationStore, logger *zap.SugaredLogger) *BoundedStore {
	return NewBoundedStoreWithConfig(db, store, logger, nil)
}

// NewBoundedStoreWithConfig creates a bounded storage manager with custom limits.
// store may be nil when only enforcement/rich-field methods are needed.
// Pass nil config to use defaults (16/64/64).
func NewBoundedStoreWithConfig(db *sql.DB, store ats.AttestationStore, logger *zap.SugaredLogger, config *BoundedStoreConfig) *BoundedStore {
	if config == nil {
		config = DefaultBoundedStoreConfig()
	}

	return &BoundedStore{
		db:     db,
		store:  store,
		logger: logger,
		config: config,
	}
}

// CreateAttestation inserts a new attestation into the database (implements ats.AttestationStore).
// Enforcement runs through Rust inside RustBackedStore.CreateAttestation.
func (bs *BoundedStore) CreateAttestation(as *types.As) error {
	if err := bs.store.CreateAttestation(as); err != nil {
		return errors.Wrap(err, "failed to create attestation")
	}

	return nil
}

// AttestationExists checks if an attestation with the given ID exists (implements ats.AttestationStore)
func (bs *BoundedStore) AttestationExists(asid string) bool {
	return bs.store.AttestationExists(asid)
}

// GenerateAndCreateAttestation generates a vanity ASID and creates a self-certifying attestation (implements ats.AttestationStore)
// Note: Observer notification is handled by RustBackedStore.CreateAttestation (called internally)
func (bs *BoundedStore) GenerateAndCreateAttestation(ctx context.Context, cmd *types.AsCommand) (*types.As, error) {
	return bs.store.GenerateAndCreateAttestation(ctx, cmd)
}

// BatchGenerateAndCreateAttestations delegates to the underlying store's batch method.
func (bs *BoundedStore) BatchGenerateAndCreateAttestations(ctx context.Context, cmds []*types.AsCommand) (int, error) {
	type batchCreator interface {
		BatchGenerateAndCreateAttestations(ctx context.Context, cmds []*types.AsCommand) (int, error)
	}
	if bc, ok := bs.store.(batchCreator); ok {
		return bc.BatchGenerateAndCreateAttestations(ctx, cmds)
	}
	// Fallback: individual writes
	for i, cmd := range cmds {
		if _, err := bs.store.GenerateAndCreateAttestation(ctx, cmd); err != nil {
			return i, err
		}
	}
	return len(cmds), nil
}

// GetAttestations retrieves attestations based on filters (implements ats.AttestationStore)
func (bs *BoundedStore) GetAttestations(filters ats.AttestationFilter) ([]*types.As, error) {
	return bs.store.GetAttestations(filters)
}

// CreateAttestationWithLimits creates an attestation (implements ats.BoundedStore).
// Enforcement runs through Rust inside RustBackedStore.CreateAttestation.
func (bs *BoundedStore) CreateAttestationWithLimits(cmd *types.AsCommand) (*types.As, error) {
	as, err := bs.store.GenerateAndCreateAttestation(context.Background(), cmd)
	if err != nil {
		return nil, errors.Wrap(err, "failed to create attestation")
	}

	return as, nil
}

// FlushEnforcement runs enforcement directly through Rust for all recent attestations.
// Used by tests to verify enforcement behavior synchronously.
func (bs *BoundedStore) FlushEnforcement() {
	rbs, ok := bs.store.(*RustBackedStore)
	if !ok {
		return
	}
	// Get all unique actors/contexts/subjects and enforce
	actors, _ := rbs.rust.GetAllContexts()
	if actors == nil {
		actors = []string{}
	}
	// Run a broad enforcement pass
	allActors := []string{}
	allContexts := []string{}
	allSubjects := []string{}

	// Query all attestations to collect dimensions
	all, err := rbs.rust.GetAttestations(ats.AttestationFilter{})
	if err != nil {
		return
	}
	actorSet := map[string]struct{}{}
	contextSet := map[string]struct{}{}
	subjectSet := map[string]struct{}{}
	for _, a := range all {
		for _, v := range a.Actors {
			actorSet[v] = struct{}{}
		}
		for _, v := range a.Contexts {
			contextSet[v] = struct{}{}
		}
		for _, v := range a.Subjects {
			subjectSet[v] = struct{}{}
		}
	}
	for k := range actorSet {
		allActors = append(allActors, k)
	}
	for k := range contextSet {
		allContexts = append(allContexts, k)
	}
	for k := range subjectSet {
		allSubjects = append(allSubjects, k)
	}

	cfg := rbs.enforcementCfg
	rbs.rust.EnforceLimits(allActors, allContexts, allSubjects, cfg)
}

// nullIfEmpty returns nil for empty strings (for nullable SQL columns)
func nullIfEmpty(s string) interface{} {
	if s == "" {
		return nil
	}
	return s
}
