package types

import (
	"time"

	"github.com/teranos/QNTX/internal/logger"
	"github.com/teranos/QNTX/internal/named"
	errors "github.com/teranos/sacred-error"
)

// As represents an attestation - a verifiable claim about subjects,
// predicates, and contexts with actor attribution and timestamps.
//
// This is Go's native representation with struct tags for JSON/DB mapping.
// Proto definitions (plugin/grpc/protocol/atsstore.proto) are used for gRPC
// and cross-language boundaries, with manual conversion at boundaries.
//
// TODO(#605): Rename to Attestation (and AsCommand → As) for clarity.
type As struct {
	ID         string         `db:"id" json:"id" validate:"required"`                       // ASID: AS + UUID
	Subjects   []string       `db:"subjects" json:"subjects" validate:"required,min=1"`     // Entities being attested about
	Predicates []string       `db:"predicates" json:"predicates" validate:"required,min=1"` // Claims being made
	Contexts   []string       `db:"contexts" json:"contexts" validate:"required,min=1"`     // Object of the claim ("of X") — a grammatical slot
	Actors     []string       `db:"actors" json:"actors" validate:"required,min=1"`         // Who made the attestation
	Timestamp  time.Time      `db:"timestamp" json:"timestamp" validate:"required"`         // When attestation was made
	Source     string         `db:"source" json:"source" validate:"required"`               // How attestation was created
	Attributes map[string]any `db:"attributes" json:"attributes,omitempty"`                 // Arbitrary JSON
	CreatedAt  time.Time      `db:"created_at" json:"created_at"`                           // Database creation time
	Signature  []byte         `db:"signature" json:"signature,omitempty"`                   // Ed25519 signature over canonical JSON
	SignerDID  string         `db:"signer_did" json:"signer_did,omitempty"`                 // did:key of a signing node
}

// AsCommand represents the parsed CLI command for creating attestations
//
// TODO(#605): Rename to As (and As → Attestation) for clarity.
type AsCommand struct {
	Subjects      []string       `json:"subjects"`             // Entities being attested about
	Predicates    []string       `json:"predicates"`           // What is being claimed (optional, defaults to ["_"])
	Contexts      []string       `json:"contexts"`             // Optional "of" context (defaults to ["_"])
	By            Authors        `json:"-"`                    // Who made the attestation: the actors it names, or the node
	When          Moment         `json:"-"`                    // When the attestation was made: a time it names, or now
	Source        string         `json:"source,omitempty"`     // Source of attestation (e.g., "cli", "github", "atproto")
	SourceVersion SourceVersion  `json:"-"`                    // The version of the source a plugin's command is stamped with
	Attributes    map[string]any `json:"attributes,omitempty"` // Arbitrary JSON
}

// SourceVersion is the version an attestation a plugin sends is stamped
// with: the one its command names, or the one the node's registry holds for
// its source. A command a plugin sends naming neither is refused.
type SourceVersion interface {
	stampsSourceVersion()
}

// VersionNamed is the version a command names.
type VersionNamed struct {
	Version named.Text
}

// VersionFromRegistry is the version the node's registry holds for the
// command's source.
type VersionFromRegistry struct{}

func (VersionNamed) stampsSourceVersion()        {}
func (VersionFromRegistry) stampsSourceVersion() {}

// Authors is who a command says made it: the actors it names, or the node.
type Authors interface {
	saysWhoMadeIt()
}

// ActorsNamed are the actors a command names, at least one.
type ActorsNamed struct {
	Actors named.Items[string]
}

// ActorNamed is the one actor a command names.
func ActorNamed(actor named.Text) ActorsNamed {
	return ActorsNamed{Actors: named.ItemOf(actor.String())}
}

// "the node"
//
// authors what its caller named nobody for, naming itself where it writes. A
// store is not the node, so it refuses a command made by the node.
type ByNode struct{}

func (ActorsNamed) saysWhoMadeIt() {}
func (ByNode) saysWhoMadeIt()      {}

// Moment is when a command says it was made: the time it names, or now.
type Moment interface {
	saysWhen()
}

// At is the time a command names.
type At struct {
	Time time.Time
}

// Now is when the command is written.
type Now struct{}

func (At) saysWhen()  {}
func (Now) saysWhen() {}

// NamedActors are the actors a command names. A store is not the node, so a
// command made by the node, or naming nobody, is refused.
func (cmd *AsCommand) NamedActors() ([]string, error) {
	switch by := cmd.By.(type) {
	case ActorsNamed:
		return by.Actors.All(), nil
	case ByNode:
		return nil, errors.Newf("an attestation about %v is made by the node, and a store is not the node: it does not invent an actor", cmd.Subjects)
	}
	return nil, errors.Newf("an attestation about %v names no actor, and a store does not invent one", cmd.Subjects)
}

// MadeAt is when a command says it was made: the time it names, or now as it
// is written. A command saying neither is refused.
func (cmd *AsCommand) MadeAt() (time.Time, error) {
	switch when := cmd.When.(type) {
	case At:
		return when.Time, nil
	case Now:
		return time.Now(), nil
	}
	return time.Time{}, errors.Newf("an attestation about %v names no time: a time, or now", cmd.Subjects)
}

// ToAs converts an AsCommand to an As struct with generated ASID and source.
// A command naming no actor, or no time, is refused.
func (cmd *AsCommand) ToAs(asid, source string) (*As, error) {
	actors, err := cmd.NamedActors()
	if err != nil {
		return nil, err
	}
	ts, err := cmd.MadeAt()
	if err != nil {
		return nil, err
	}

	// Set defaults for empty arrays
	predicates := cmd.Predicates
	if len(predicates) == 0 {
		predicates = []string{"_"}
	}

	contexts := cmd.Contexts
	if len(contexts) == 0 {
		contexts = []string{"_"}
	}

	// Determine source: prefer cmd.Source, then parameter, then deprecated fallback
	effectiveSource := cmd.Source
	if effectiveSource == "" {
		effectiveSource = source
	}
	if effectiveSource == "" {
		// DEPRECATED: Source should be explicitly provided by all callers
		// This fallback exists for backwards compatibility but will be removed
		logger.Logger.Warnw("ToAs() called without source, falling back to 'cli'", "hint", "set AsCommand.Source explicitly")
		effectiveSource = "cli"
	}

	return &As{
		ID:         asid,
		Subjects:   cmd.Subjects,
		Predicates: predicates,
		Contexts:   contexts,
		Actors:     actors,
		Timestamp:  ts,
		Source:     effectiveSource,
		Attributes: cmd.Attributes,
		CreatedAt:  time.Now(),
	}, nil
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

// AxFilter represents the parsed CLI command for querying attestations
type AxFilter struct {
	Subjects   []string  `json:"subjects"`   // Specific entities to ask about
	Predicates []string  `json:"predicates"` // What predicates to match
	Contexts   []string  `json:"contexts"`   // What contexts to match
	Actors     []string  `json:"actors"`     // Filter by specific actors
	TimeStart  TimeBound `json:"time_start"` // Where the time range starts: a time, or unbounded
	TimeEnd    TimeBound `json:"time_end"`   // Where the time range ends: a time, or unbounded
	Limit      int       `json:"limit"`      // Maximum results
	Format     string    `json:"format"`     // Output format: table, json
	SoActions  []string  `json:"so_actions"` // Actions to perform after query: ex csv, summarize, etc
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
	SQLArgs          []any               `json:"sql_args,omitempty"`
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
