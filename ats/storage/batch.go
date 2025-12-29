// Package storage provides batch persistence operations for attestations.
// It handles efficient bulk insertion with error tracking and metadata support.
package storage

import (
	"database/sql"
	"fmt"
	"time"

	"github.com/teranos/QNTX/ats"
	"github.com/teranos/QNTX/ats/ingestion"
	"github.com/teranos/QNTX/ats/types"
	"github.com/teranos/vanity-id"
)

// AttestationItem represents an item that can be converted to an attestation.
// This is an alias for ingestion.Item, enabling domain-agnostic data producers
// to work with attestation persistence without tight coupling.
type AttestationItem = ingestion.Item

// BatchPersister handles batch attestation persistence with error tracking and statistics
type BatchPersister struct {
	db           *sql.DB
	store        *SQLStore
	boundedStore *BoundedStore // Optional: for predictive warnings
	actor        string
	source       string
}

// NewBatchPersister creates a new batch attestation persister
func NewBatchPersister(db *sql.DB, actor, source string) *BatchPersister {
	return &BatchPersister{
		db:     db,
		store:  NewSQLStore(db, nil),
		actor:  actor,
		source: source,
	}
}

// NewBatchPersisterWithWarnings creates a batch persister with predictive storage warnings
func NewBatchPersisterWithWarnings(db *sql.DB, actor, source string, boundedStore *BoundedStore) *BatchPersister {
	return &BatchPersister{
		db:           db,
		store:        NewSQLStore(db, nil),
		boundedStore: boundedStore,
		actor:        actor,
		source:       source,
	}
}

// PersistItems converts AttestationItems to attestations and persists them to the database
// Returns detailed statistics and error information for reporting
func (bp *BatchPersister) PersistItems(items []AttestationItem, sourcePrefix string) *ats.PersistenceResult {
	result := &ats.PersistenceResult{
		Errors: make([]string, 0),
	}

	// Check for nil database connection
	if bp.db == nil {
		result.FailureCount = len(items)
		result.Errors = append(result.Errors, "database connection is nil")
		return result
	}

	for _, item := range items {
		// Generate unique ASID
		asid, err := id.GenerateASID(item.GetSubject(), item.GetPredicate(), sourcePrefix, bp.actor)
		if err != nil {
			result.FailureCount++
			errorMsg := fmt.Sprintf("Failed to generate ASID for %s %s: %v",
				item.GetSubject(), item.GetPredicate(), err)
			result.Errors = append(result.Errors, errorMsg)
			continue
		}

		// Create attestation from AttestationItem with proper predicate/context structure
		// Use self-certifying ASID: if bp.actor is empty, use the generated ASID as its own actor
		actor := bp.actor
		if actor == "" {
			actor = asid
		}

		attestation := types.As{
			ID:         asid,
			Subjects:   []string{item.GetSubject()},
			Predicates: []string{item.GetPredicate()}, // Predicate is the verb/relationship
			Contexts:   []string{item.GetObject()},    // Context holds the value/object
			Actors:     []string{actor},               // Self-certifying: ASID vouches for itself
			Timestamp:  time.Now(),
			Source:     bp.source,
			Attributes: make(map[string]interface{}),
		}

		// Add metadata as attributes if available
		meta := item.GetMeta()
		if len(meta) > 0 {
			for k, v := range meta {
				attestation.Attributes[k] = v
			}
		}

		// Persist to database
		if err := bp.store.CreateAttestation(&attestation); err != nil {
			result.FailureCount++
			errorMsg := fmt.Sprintf("Failed to persist attestation %s %s %s: %v",
				item.GetSubject(), item.GetPredicate(), item.GetObject(), err)
			result.Errors = append(result.Errors, errorMsg)
			continue
		}

		// Log predictive warnings if bounded store is configured
		if bp.boundedStore != nil {
			bp.boundedStore.logStorageWarnings(&attestation)
		}

		result.PersistedCount++
	}

	// Calculate success rate
	totalItems := len(items)
	if totalItems > 0 {
		result.SuccessRate = float64(result.PersistedCount) / float64(totalItems) * 100
	}

	return result
}
