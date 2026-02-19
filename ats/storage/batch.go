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
	actor        string
	source       string
	boundedStore *BoundedStore // Optional: for predictive storage warnings
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

// WithBoundedStore adds a bounded store for predictive storage warnings
// When set, PersistItems will check for storage limits approaching and include warnings
func (bp *BatchPersister) WithBoundedStore(bs *BoundedStore) *BatchPersister {
	bp.boundedStore = bs
	return bp
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
			Contexts:   []string{item.GetContext()},
			Actors:     []string{actor}, // Self-certifying: ASID vouches for itself
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
				item.GetSubject(), item.GetPredicate(), item.GetContext(), err)
			result.Errors = append(result.Errors, errorMsg)
			continue
		}

		result.PersistedCount++

		// Check for predictive storage warnings if bounded store is configured
		if bp.boundedStore != nil {
			warnings := bp.boundedStore.CheckStorageStatus(&attestation)
			for _, w := range warnings {
				result.Warnings = append(result.Warnings, &ats.StorageWarning{
					Actor:         w.Actor,
					Context:       w.Context,
					Current:       w.Current,
					Limit:         w.Limit,
					FillPercent:   w.FillPercent,
					TimeUntilFull: w.TimeUntilFull.String(),
				})
			}
		}
	}

	// Calculate success rate
	totalItems := len(items)
	if totalItems > 0 {
		result.SuccessRate = float64(result.PersistedCount) / float64(totalItems) * 100
	}

	// Deduplicate warnings (same actor/context pair may appear multiple times)
	result.Warnings = deduplicateWarnings(result.Warnings)

	return result
}

// deduplicateWarnings removes duplicate warnings for the same actor/context pair
func deduplicateWarnings(warnings []*ats.StorageWarning) []*ats.StorageWarning {
	if len(warnings) == 0 {
		return warnings
	}

	seen := make(map[string]bool)
	var unique []*ats.StorageWarning

	for _, w := range warnings {
		key := w.Actor + "|" + w.Context
		if !seen[key] {
			seen[key] = true
			unique = append(unique, w)
		}
	}

	return unique
}
