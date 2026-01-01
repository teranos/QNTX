package types

import (
	"time"
)

// As represents an attestation - a verifiable claim about subjects,
// predicates, and contexts with actor attribution and timestamps
type As struct {
	ID         string                 `db:"id" json:"id" validate:"required"`                       // ASID: AS + UUID
	Subjects   []string               `db:"subjects" json:"subjects" validate:"required,min=1"`     // Entities being attested about
	Predicates []string               `db:"predicates" json:"predicates" validate:"required,min=1"` // What is being claimed
	Contexts   []string               `db:"contexts" json:"contexts" validate:"required,min=1"`     // Optional "of" context
	Actors     []string               `db:"actors" json:"actors" validate:"required,min=1"`         // Who made the attestation
	Timestamp  time.Time              `db:"timestamp" json:"timestamp" validate:"required"`         // When attestation was made
	Source     string                 `db:"source" json:"source" validate:"required"`               // How attestation was created
	Attributes map[string]interface{} `db:"attributes" json:"attributes,omitempty"`                 // Arbitrary JSON
	CreatedAt  time.Time              `db:"created_at" json:"created_at"`                           // Database creation time
}

// AsCommand represents the parsed CLI command for creating attestations
type AsCommand struct {
	Subjects   []string               `json:"subjects"`             // Entities being attested about
	Predicates []string               `json:"predicates"`           // What is being claimed (optional, defaults to ["_"])
	Contexts   []string               `json:"contexts"`             // Optional "of" context (defaults to ["_"])
	Actors     []string               `json:"actors"`               // Who made the attestation (optional, uses default)
	Timestamp  time.Time              `json:"timestamp"`            // When attestation was made (optional, uses now)
	Attributes map[string]interface{} `json:"attributes,omitempty"` // Arbitrary JSON
}

// ToAs converts an AsCommand to an As struct with generated ASID
func (cmd *AsCommand) ToAs(asid string) *As {
	// Set defaults for empty arrays
	predicates := cmd.Predicates
	if len(predicates) == 0 {
		predicates = []string{"_"}
	}

	contexts := cmd.Contexts
	if len(contexts) == 0 {
		contexts = []string{"_"}
	}

	return &As{
		ID:         asid,
		Subjects:   cmd.Subjects,
		Predicates: predicates,
		Contexts:   contexts,
		Actors:     cmd.Actors,
		Timestamp:  cmd.Timestamp,
		Source:     "cli",
		Attributes: cmd.Attributes,
		CreatedAt:  time.Now(),
	}
}

// IsExistenceAttestation returns true if this is a simple existence attestation
func (as *As) IsExistenceAttestation() bool {
	return len(as.Predicates) == 1 && as.Predicates[0] == "_" &&
		len(as.Contexts) == 1 && as.Contexts[0] == "_"
}

// HasMultipleDimensions returns true if this attestation has multiple subjects, predicates, or contexts
func (as *As) HasMultipleDimensions() bool {
	return len(as.Subjects) > 1 || len(as.Predicates) > 1 || len(as.Contexts) > 1
}

// GetCartesianCount returns the total number of individual claims this attestation represents
func (as *As) GetCartesianCount() int {
	return len(as.Subjects) * len(as.Predicates) * len(as.Contexts)
}

// OverFilter represents temporal comparison for "over X years/months" queries
type OverFilter struct {
	Value    float64 `json:"value"`    // The numeric value (e.g., 5 for "5y")
	Unit     string  `json:"unit"`     // The unit: "y" for years, "m" for months
	Operator string  `json:"operator"` // Comparison operator: "over" means >=
}

// AxFilter represents the parsed CLI command for querying attestations
type AxFilter struct {
	Subjects       []string    `json:"subjects"`        // Specific entities to ask about
	Predicates     []string    `json:"predicates"`      // What predicates to match (with fuzzy)
	Contexts       []string    `json:"contexts"`        // What contexts to match
	Actors         []string    `json:"actors"`          // Filter by specific actors
	TimeStart      *time.Time  `json:"time_start"`      // Temporal range start
	TimeEnd        *time.Time  `json:"time_end"`        // Temporal range end
	OverComparison *OverFilter `json:"over_comparison"` // Temporal comparison (e.g., "over 5y")
	Limit          int         `json:"limit"`           // Maximum results
	Format         string      `json:"format"`          // Output format: table, json
	SoActions      []string    `json:"so_actions"`      // Actions to perform after query: ex csv, summarize, etc
}

// AxResult represents the result of an ax query
type AxResult struct {
	Attestations []As       `json:"attestations"`    // All matching attestations
	Conflicts    []Conflict `json:"conflicts"`       // Identified conflicts
	Summary      AxSummary  `json:"summary"`         // Aggregated information
	Format       string     `json:"format"`          // Output format for display
	Debug        AxDebug    `json:"debug,omitempty"` // Debug information (verbose mode)
}

// AxDebug provides debugging information for ax queries
type AxDebug struct {
	ExecutionTimeMs  int64               `json:"execution_time_ms"`
	SQLQuery         string              `json:"sql_query,omitempty"`
	SQLArgs          []interface{}       `json:"sql_args,omitempty"`
	OriginalFilter   AxFilter            `json:"original_filter"`
	ExpandedFilter   AxFilter            `json:"expanded_filter,omitempty"`
	AliasExpansions  map[string][]string `json:"alias_expansions,omitempty"`
	DatabaseRowCount int                 `json:"database_row_count"`
}

// AxSummary provides aggregated information about ax results
type AxSummary struct {
	TotalAttestations int            `json:"total_attestations"`
	UniqueSubjects    map[string]int `json:"unique_subjects"`
	UniquePredicates  map[string]int `json:"unique_predicates"`
	UniqueContexts    map[string]int `json:"unique_contexts"`
	UniqueActors      map[string]int `json:"unique_actors"`
}

// Conflict represents conflicting attestations
type Conflict struct {
	Subject      string `json:"subject"`
	Predicate    string `json:"predicate"`
	Context      string `json:"context"`
	Attestations []As   `json:"attestations"`
	Resolution   string `json:"resolution"` // "conflict", "evolution", "verification", "no_conflict"
}
