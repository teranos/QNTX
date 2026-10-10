// This file defines the storage interfaces that separate the pure type system
// from storage implementation details.

package ats

import (
	"context"
	"math"
	"time"

	"github.com/teranos/QNTX/ats/types"
)

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
	Limit      int        // How many rows: 0 is none, and every row is EveryRow
}

// EveryRow is the limit of a query that wants every matching row: a high value,
// said, since a limit of 0 is 0 rows.
const EveryRow = math.MaxInt32

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
