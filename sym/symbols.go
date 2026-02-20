// Package sym defines canonical symbols for QNTX SEG operations and system markers.
// These symbols are stable across UI, CLI, and documentation.
//
// The source of truth for which symbols exist is proto/sym.proto.
// This package provides the Go-native string constants and lookup tables
// derived from those definitions.
package sym

import "github.com/teranos/QNTX/sym/sympb"

// Re-export proto types for consumers that want type-safe enum access.
type (
	Symbol         = sympb.Symbol
	SymbolCategory = sympb.SymbolCategory
	SymbolDef      = sympb.SymbolDef
	SymbolRegistry = sympb.SymbolRegistry
)

// Proto enum constants re-exported for convenience.
const (
	SymbolUnspecified = sympb.Symbol_SYMBOL_UNSPECIFIED
	SymbolI           = sympb.Symbol_SYMBOL_I
	SymbolAM          = sympb.Symbol_SYMBOL_AM
	SymbolIX          = sympb.Symbol_SYMBOL_IX
	SymbolAX          = sympb.Symbol_SYMBOL_AX
	SymbolBY          = sympb.Symbol_SYMBOL_BY
	SymbolAT          = sympb.Symbol_SYMBOL_AT
	SymbolSO          = sympb.Symbol_SYMBOL_SO
	SymbolSE          = sympb.Symbol_SYMBOL_SE
	SymbolAS          = sympb.Symbol_SYMBOL_AS
	SymbolIS          = sympb.Symbol_SYMBOL_IS
	SymbolOF          = sympb.Symbol_SYMBOL_OF
	SymbolPulse       = sympb.Symbol_SYMBOL_PULSE
	SymbolPulseOpen   = sympb.Symbol_SYMBOL_PULSE_OPEN
	SymbolPulseClose  = sympb.Symbol_SYMBOL_PULSE_CLOSE
	SymbolDB          = sympb.Symbol_SYMBOL_DB
	SymbolProse       = sympb.Symbol_SYMBOL_PROSE
	SymbolDoc         = sympb.Symbol_SYMBOL_DOC
	SymbolSubcanvas   = sympb.Symbol_SYMBOL_SUBCANVAS

	CategorySEGPrimary    = sympb.SymbolCategory_SYMBOL_CATEGORY_SEG_PRIMARY
	CategoryBuildingBlock = sympb.SymbolCategory_SYMBOL_CATEGORY_BUILDING_BLOCK
	CategorySystem        = sympb.SymbolCategory_SYMBOL_CATEGORY_SYSTEM
)

// Glyph string constants — the visual expression of each symbol.
// Derived from proto/sym.proto (glyph:"..." annotations on each enum variant).
//
// Primary SEG operators — have UI components and commands.
const (
	I  = "⍟" // self — your vantage point into QNTX
	AM = "≡" // am — configuration and system settings
	IX = "⨳" // ix — ingest/import external data
	AX = "⋈" // ax — expand/query, contextual surfacing
	BY = "⌬" // by — actor/catalyst/origin (all forms: creator, source, user)
	AT = "✦" // at — temporal marker/moment
	SO = "⟶" // so — therefore/consequent action
	SE = "⊨" // se — semantic search/entailment
)

// Attestation building blocks — structural, not UI elements.
// Fundamental components of: "subject IS predicate OF context BY actor AT time"
const (
	AS = "+" // as — assert/emit an attestation
	IS = "=" // is — identity/equivalence in attestations
	OF = "∈" // of — membership/belonging in attestations
)

// System infrastructure symbols.
const (
	Pulse      = "꩜" // async jobs, rate limiting, budget management
	PulseOpen  = "✿" // graceful startup with orphaned job recovery
	PulseClose = "❀" // graceful shutdown with checkpoint preservation
	DB         = "⊔" // database/storage layer
	Prose      = "▣" // documentation and prose content
	Doc        = "▤" // document/file content (PDF, etc.)
	Subcanvas  = "⌗" // nested canvas workspace
)

// entry binds a proto Symbol enum value to its glyph, command, and description.
type entry struct {
	symbol      Symbol
	glyph       string
	command     string
	label       string
	description string
	category    SymbolCategory
	palette     int // 1-based position in PaletteOrder, 0 = not in palette
}

// registry is the canonical mapping between proto enum values and symbol metadata.
// Order matches proto/sym.proto field numbers.
var registry = []entry{
	{SymbolI, I, "i", "Self", "Your vantage point into QNTX", CategorySEGPrimary, 1},
	{SymbolAM, AM, "am", "Configuration", "System settings and state", CategorySEGPrimary, 2},
	{SymbolIX, IX, "ix", "Ingest", "Import external data", CategorySEGPrimary, 3},
	{SymbolAX, AX, "ax", "Expand", "Query and surface related context", CategorySEGPrimary, 4},
	{SymbolBY, BY, "by", "Actor", "Origin of action (creator/source/user)", CategorySEGPrimary, 5},
	{SymbolAT, AT, "at", "Temporal", "Time marker/moment", CategorySEGPrimary, 6},
	{SymbolSO, SO, "so", "Therefore", "Consequent action/trigger", CategorySEGPrimary, 7},
	{SymbolSE, SE, "se", "Semantic", "Meaning-based search and entailment", CategorySEGPrimary, 8},
	{SymbolAS, AS, "as", "Assert", "Emit an attestation", CategoryBuildingBlock, 0},
	{SymbolIS, IS, "is", "Identity", "Subject/equivalence in attestations", CategoryBuildingBlock, 0},
	{SymbolOF, OF, "of", "Membership", "Element-of/belonging in attestations", CategoryBuildingBlock, 0},
	{SymbolPulse, Pulse, "", "", "Async jobs, rate limiting, budget management", CategorySystem, 0},
	{SymbolPulseOpen, PulseOpen, "", "", "Graceful startup with orphaned job recovery", CategorySystem, 0},
	{SymbolPulseClose, PulseClose, "", "", "Graceful shutdown with checkpoint preservation", CategorySystem, 0},
	{SymbolDB, DB, "", "", "Database/storage layer", CategorySystem, 0},
	{SymbolProse, Prose, "", "", "Documentation and prose content", CategorySystem, 0},
	{SymbolDoc, Doc, "", "", "Document/file content (PDF, etc.)", CategorySystem, 0},
	{SymbolSubcanvas, Subcanvas, "", "", "Nested canvas workspace", CategorySystem, 0},
}

// Lookup tables built from the registry at init time.
var (
	glyphToSymbol map[string]Symbol
	symbolToGlyph map[Symbol]string
)

func init() {
	glyphToSymbol = make(map[string]Symbol, len(registry))
	symbolToGlyph = make(map[Symbol]string, len(registry))
	for _, e := range registry {
		glyphToSymbol[e.glyph] = e.symbol
		symbolToGlyph[e.symbol] = e.glyph
	}
}

// Glyph returns the Unicode glyph string for a proto Symbol enum value.
func Glyph(s Symbol) string {
	return symbolToGlyph[s]
}

// FromGlyph returns the proto Symbol enum value for a Unicode glyph string.
func FromGlyph(glyph string) Symbol {
	if s, ok := glyphToSymbol[glyph]; ok {
		return s
	}
	return SymbolUnspecified
}

// PaletteOrder defines the canonical ordering for UI controls,
// shortcuts, selection bars, etc.
// Only includes primary SEG operators (not attestation building blocks).
var PaletteOrder = []string{I, AM, IX, AX, BY, AT, SO, SE}

// SymbolToCommand maps glyph strings to their text command equivalents.
var SymbolToCommand = map[string]string{
	I:  "i",
	AM: "am",
	IX: "ix",
	AX: "ax",
	BY: "by",
	AT: "at",
	SO: "so",
	SE: "se",
	AS: "as",
	IS: "is",
	OF: "of",
}

// CommandToSymbol maps text commands to their canonical glyph strings.
var CommandToSymbol = map[string]string{
	"i":  I,
	"am": AM,
	"ix": IX,
	"ax": AX,
	"by": BY,
	"at": AT,
	"so": SO,
	"se": SE,
	"as": AS,
	"is": IS,
	"of": OF,
}

// CommandDescriptions provides human-readable explanations for tooltip hover states.
var CommandDescriptions = map[string]string{
	"i":  "Self — Your vantage point into QNTX",
	"am": "Configuration — System settings and state",
	"ix": "Ingest — Import external data",
	"ax": "Expand — Query and surface related context",
	"by": "Actor — Origin of action (creator/source/user)",
	"at": "Temporal — Time marker/moment",
	"so": "Therefore — Consequent action/trigger",
	"se": "Semantic — Meaning-based search and entailment",
	"as": "Assert — Emit an attestation",
	"is": "Identity — Subject/equivalence in attestations",
	"of": "Membership — Element-of/belonging in attestations",
}
