// Package sym defines canonical symbols for QNTX SEG operations and system markers.
// These symbols are stable across UI, CLI, and documentation.
package sym

// Primary SEG operators - these have UI components and commands
const (
	I  = "⍟" // self - your vantage point into QNTX
	AM = "≡" // am - configuration and system settings
	IX = "⨳" // ix - ingest/import external data
	AX = "⋈" // ax - expand/query, contextual surfacing
	BY = "⌬" // by - actor/catalyst/origin (all forms: creator, source, user)
	AT = "✦" // at - temporal marker/moment
	SO = "⟶" // so - therefore/consequent action
	SE = "⊨" // se - semantic search/entailment

	// Attestation building blocks (not UI elements)
	// These are fundamental components of the attestation pattern:
	// "subject IS predicate OF context BY actor AT time"
	AS = "+" // as - assert/emit an attestation
	IS = "=" // is - identity/equivalence in attestations
	OF = "∈" // of - membership/belonging in attestations
	// TODO: Consider alternative typeable symbol for OF

	// System infrastructure symbols
	Pulse      = "꩜" // Pulse system: async jobs, rate limiting, budget management (always prefix logs)
	PulseOpen  = "✿" // Graceful startup with orphaned job recovery
	PulseClose = "❀" // Graceful shutdown with checkpoint preservation
	DB         = "⊔" // Database/storage layer
	Prose      = "▣" // Documentation and prose content
	Doc        = "▤" // Document/file content (PDF, etc.)
	Subcanvas  = "⌗" // Nested canvas (subcanvas workspace)
)

// PaletteOrder defines the canonical ordering for UI controls,
// shortcuts, selection bars, etc.
// Only includes primary SEG operators (not attestation building blocks)
var PaletteOrder = []string{
	I,
	AM,
	IX,
	AX,
	BY,
	AT,
	SO,
	SE,
}

// SymbolToCommand maps symbols to their text command equivalents
// for dual-mode acceptance (backwards compatibility)
// Includes both primary SEG operators and attestation building blocks
var SymbolToCommand = map[string]string{
	// Primary SEG operators
	I:  "i",
	AM: "am",
	IX: "ix",
	AX: "ax",
	BY: "by",
	AT: "at",
	SO: "so",
	SE: "se",
	// Attestation building blocks
	AS: "as",
	IS: "is",
	OF: "of",
}

// CommandToSymbol maps text commands to their canonical symbols
// for normalization and display purposes
var CommandToSymbol = map[string]string{
	// Primary SEG operators
	"i":  I,
	"am": AM,
	"ix": IX,
	"ax": AX,
	"by": BY,
	"at": AT,
	"so": SO,
	"se": SE,
	// Attestation building blocks
	"as": AS,
	"is": IS,
	"of": OF,
}

// CommandDescriptions provides human-readable explanations
// for tooltip hover states
var CommandDescriptions = map[string]string{
	// Primary SEG operators
	"i":  "Self — Your vantage point into QNTX",
	"am": "Configuration — System settings and state",
	"ix": "Ingest — Import external data",
	"ax": "Expand — Query and surface related context",
	"by": "Actor — Origin of action (creator/source/user)",
	"at": "Temporal — Time marker/moment",
	"so": "Therefore — Consequent action/trigger",
	"se": "Semantic — Meaning-based search and entailment",
	// Attestation building blocks
	"as": "Assert — Emit an attestation",
	"is": "Identity — Subject/equivalence in attestations",
	"of": "Membership — Element-of/belonging in attestations",
}
