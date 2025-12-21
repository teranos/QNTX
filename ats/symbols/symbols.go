package symbols

// Canonical symbols for QNTX SEG operations.
// These should stay stable across UI, CLI and docs.
const (
	SymI  = "⍟" // self / operator vantage point
	SymAM = "≡" // AM structure / internal interpretation
	SymIX = "⨳" // ingest / import
	SymAX = "⋈" // expand / contextual surfacing
	SymAS = "+" // assert / emit .ats
	SymIS = "=" // identity / equivalence
	SymOF = "∈" // membership / element-of / belonging
	SymBY = "⌬" // actor / catalyst / origin of action
	SymAT = "✦" // event / temporal marker
	SymSO = "⟶" // therefore / consequent action / trigger

	// System symbols (not SEG operators)
	SymPulse = "꩜" // Pulse system: async jobs, rate limiting, budget management
)

// PaletteOrder defines the canonical ordering for UI controls,
// shortcuts, selection bars, etc.
var PaletteOrder = []string{
	SymI,
	SymAM,
	SymIX,
	SymAX,
	SymAS,
	SymIS,
	SymOF,
	SymBY,
	SymAT,
	SymSO,
}

// SymbolToCommand maps symbols to their text command equivalents
// for dual-mode acceptance (backwards compatibility)
var SymbolToCommand = map[string]string{
	SymI:  "i",
	SymAM: "am",
	SymIX: "ix",
	SymAX: "ax",
	SymAS: "as",
	SymIS: "is",
	SymOF: "of",
	SymBY: "by",
	SymAT: "at",
	SymSO: "so",
}

// CommandToSymbol maps text commands to their canonical symbols
// for normalization and display purposes
var CommandToSymbol = map[string]string{
	"i":  SymI,
	"am": SymAM,
	"ix": SymIX,
	"ax": SymAX,
	"as": SymAS,
	"is": SymIS,
	"of": SymOF,
	"by": SymBY,
	"at": SymAT,
	"so": SymSO,
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
