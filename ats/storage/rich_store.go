// Package storage provides attestation storage.
package storage

import (
	"context"
	"database/sql"
	"sync"
	"time"

	"go.uber.org/zap"

	"github.com/teranos/QNTX/ats"
	"github.com/teranos/QNTX/ats/types"
	errors "github.com/teranos/sacred-error"
)

// RichStore is a store and the database it reads rich fields from: rich
// search over attributes, and the type definitions that name them.
type RichStore struct {
	db     *sql.DB
	store  ats.AttestationStore
	logger *zap.SugaredLogger

	// Cache for type definitions (used by rich search)
	typeFieldsCache     map[string][]string
	typeFieldsCacheLock sync.RWMutex
	typeFieldsCacheTime time.Time
}

// NewRichStore wraps a store with the database its rich fields are read from.
// store may be nil when only the rich-field methods are needed.
func NewRichStore(db *sql.DB, store ats.AttestationStore, logger *zap.SugaredLogger) *RichStore {
	return &RichStore{db: db, store: store, logger: logger}
}

// CreateAttestation inserts a new attestation into the database (implements ats.AttestationStore).
func (bs *RichStore) CreateAttestation(as *types.As) error {
	if err := bs.store.CreateAttestation(as); err != nil {
		return errors.Wrap(err, "failed to create attestation")
	}

	return nil
}

// AttestationExists checks if an attestation with the given ID exists (implements ats.AttestationStore)
func (bs *RichStore) AttestationExists(asid string) bool {
	return bs.store.AttestationExists(asid)
}

// GenerateAndCreateAttestation generates a vanity ASID and creates a self-certifying attestation (implements ats.AttestationStore)
// Note: Observer notification is handled by RustBackedStore.CreateAttestation (called internally)
func (bs *RichStore) GenerateAndCreateAttestation(ctx context.Context, cmd *types.AsCommand) (*types.As, error) {
	return bs.store.GenerateAndCreateAttestation(ctx, cmd)
}

// BatchGenerateAndCreateAttestations delegates to the underlying store's batch method.
func (bs *RichStore) BatchGenerateAndCreateAttestations(ctx context.Context, cmds []*types.AsCommand) (int, error) {
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
func (bs *RichStore) GetAttestations(filters ats.AttestationFilter) ([]*types.As, error) {
	return bs.store.GetAttestations(filters)
}

// nullIfEmpty returns nil for empty strings (for nullable SQL columns)
func nullIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}
