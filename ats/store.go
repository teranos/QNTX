// Package ats provides the Attestation Type System interfaces and types.
// This file defines the storage interface that separates the pure type system
// from storage implementation details.
package ats

import (
	"context"
	"time"

	"github.com/teranos/QNTX/ats/types"
)

// AttestationItem represents an item that can be converted to an attestation.
type AttestationItem interface {
	GetSubject() string
	GetPredicate() string
	GetContext() string
	GetMeta() map[string]string
}

// AttestationStore defines storage operations for attestations.
// Implementations can use any backend (SQLite, Postgres, S3, in-memory, etc.)
type AttestationStore interface {
	// CreateAttestation inserts a new attestation into storage
	CreateAttestation(as *types.As) error

	// CreateAttestationInbound inserts a synced attestation without signing (preserves provenance)
	CreateAttestationInbound(as *types.As) error

	// AttestationExists checks if an attestation with the given ID exists
	AttestationExists(asid string) bool

	// GenerateAndCreateAttestation generates a vanity ASID and creates a self-certifying attestation
	GenerateAndCreateAttestation(ctx context.Context, cmd *types.AsCommand) (*types.As, error)

	// GetAttestations retrieves attestations based on filters
	GetAttestations(filters AttestationFilter) ([]*types.As, error)
}

// BatchStore defines batch persistence operations for attestations
type BatchStore interface {
	// PersistItems converts AttestationItems to attestations and persists them to storage
	PersistItems(items []AttestationItem, sourcePrefix string) *PersistenceResult
}

// BoundedStore defines bounded storage operations that enforce quota limits
type BoundedStore interface {
	AttestationStore

	// CreateAttestationWithLimits creates an attestation and enforces storage limits
	CreateAttestationWithLimits(cmd *types.AsCommand) (*types.As, error)
}

// AliasResolver defines alias resolution operations
type AliasResolver interface {
	// ResolveAlias returns all identifiers that should be included when searching for the given identifier
	ResolveAlias(ctx context.Context, identifier string) ([]string, error)

	// CreateAlias creates a bidirectional alias between two identifiers
	CreateAlias(ctx context.Context, alias, target, createdBy string) error

	// RemoveAlias removes an alias mapping
	RemoveAlias(ctx context.Context, alias, target string) error

	// GetAllAliases returns all alias mappings
	GetAllAliases(ctx context.Context) (map[string][]string, error)
}

// AttestationFilter represents filters for querying attestations
type AttestationFilter struct {
	Actors     []string   // Filter by actors (OR logic)
	Subjects   []string   // Filter by subjects (OR logic)
	Predicates []string   // Filter by predicates (OR logic)
	Contexts   []string   // Filter by contexts (OR logic)
	Source     string     // Filter by source (exact match, e.g., "cli", "distill")
	TimeStart  *time.Time // Temporal range start
	TimeEnd    *time.Time // Temporal range end
	Limit      int        // Maximum results
}

// PersistenceResult contains the results of a batch persistence operation
type PersistenceResult struct {
	PersistedCount int
	FailureCount   int
	Errors         []string
	SuccessRate    float64
}

// AttestationQueryStore defines query operations for attestation retrieval.
// This interface abstracts storage-specific query implementations.
type AttestationQueryStore interface {
	// GetAllPredicates returns all distinct predicates in storage
	// Used for predicate discovery
	GetAllPredicates(ctx context.Context) ([]string, error)

	// GetAllContexts returns all distinct contexts in storage
	// Used for context discovery
	GetAllContexts(ctx context.Context) ([]string, error)

	// ExecuteAxQuery executes an ax filter query and returns matching attestations
	ExecuteAxQuery(ctx context.Context, filter types.AxFilter) ([]*types.As, error)
}
