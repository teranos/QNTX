package sym

// Canonical symbols for QNTX SEG operations.
// These should stay stable across UI, CLI and docs.
const (
	I  = "⍟" // self / operator vantage point
	AM = "≡" // AM structure / internal interpretation
	IX = "⨳" // ingest / import
	AX = "⋈" // expand / contextual surfacing
	AS = "+" // assert / emit .ats
	IS = "=" // identity / equivalence
	OF = "∈" // membership / element-of / belonging
	BY = "⌬" // actor / catalyst / origin of action
	AT = "✦" // event / temporal marker
	SO = "⟶" // therefore / consequent action / trigger

	// System symbols (not SEG operators)
	Pulse = "꩜" // Pulse system: async jobs, rate limiting, budget management
)

// PaletteOrder defines the canonical ordering for UI controls,
// shortcuts, selection bars, etc.
var PaletteOrder = []string{
	I,
	AM,
	IX,
	AX,
	AS,
	IS,
	OF,
	BY,
	AT,
	SO,
}

// SymbolToCommand maps symbols to their text command equivalents
// for dual-mode acceptance (backwards compatibility)
var SymbolToCommand = map[string]string{
	I:  "i",
	AM: "am",
	IX: "ix",
	AX: "ax",
	AS: "as",
	IS: "is",
	OF: "of",
	BY: "by",
	AT: "at",
	SO: "so",
}

// CommandToSymbol maps text commands to their canonical symbols
// for normalization and display purposes
var CommandToSymbol = map[string]string{
	"i":  I,
	"am": AM,
	"ix": IX,
	"ax": AX,
	"as": AS,
	"is": IS,
	"of": OF,
	"by": BY,
	"at": AT,
	"so": SO,
}

// CommandDescriptions provides human-readable explanations
// for tooltip hover states
var CommandDescriptions = map[string]string{
	"i":  "Self — Your vantage point into QNTX",
	"am": "Structure — QNTX's internal understanding",
	"ix": "Ingest — Import external data",
	"ax": "Expand — Surface related context",
	"as": "Assert — Emit an attestation",
	"is": "Identity — Subject/equivalence",
	"of": "Membership — Element-of/belonging",
	"by": "Actor — Catalyst/origin of action",
	"at": "Event — Temporal marker/moment",
	"so": "Therefore — Consequent action",
}
