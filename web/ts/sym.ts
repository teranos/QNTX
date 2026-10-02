// The symbol: the one place a segment is mapped to a glyph.
// "UI owns presentation, the symbols glyphs"

// Primary SEG operators — these have UI components and commands.
export const I = '⍟';          // self — your vantage point into QNTX
export const AM = '≡';         // am — being: the node, the server
export const IX = '⨳';         // ix — ingest/import external data
export const AX = '⋈';         // ax — expand/query, contextual surfacing
export const BY = '⌬';         // by — actor/catalyst/origin
export const AT = '✦';         // at — temporal marker/moment
export const SO = '⟶';         // so — therefore/consequent action
export const SE = '⊨';         // se — semantic search/entailment

// Attestation building blocks, not UI elements. The pattern is
// "subject IS predicate OF context BY actor AT time".
export const AS = '+';         // as — assert/emit an attestation
export const IS = '=';         // is
export const OF = '∈';         // of — membership/belonging

// Derived attestation types.
// Attestation is one claim whole — its slots and its attributes together. The
// element for it drew '+' until '+' went back to being the subject it marks: a
// mark for one slot said the wrong thing on a thing made of all of them.
export const Attestation = '⎔'; // one claim whole, slots and attributes together
export const Triplet = '⫶';    // grouped attestations sharing subject+predicate+context
export const Type = '⊢';       // an actor's judgment that a pattern deserves a name
export const Sigma = 'Σ';      // distilled attestation, the sum of many observations

// System infrastructure.
export const Watcher = '⏿';    // observer/monitor for attestation patterns
export const Pulse = '꩜';      // async jobs, rate limiting, budget — always prefix logs
export const PulseOpen = '✿';  // graceful startup with orphaned job recovery
export const PulseClose = '❀'; // graceful shutdown with checkpoint preservation
export const DB = '⊔';         // database/storage layer
export const Prose = '▣';      // documentation and prose content
export const Doc = '▤';        // document/file content
export const Subcanvas = '⌗';  // nested canvas workspace
export const Transcript = '🧵'; // one session Ground recorded, read as what was said and done
export const Ground = '⏚';      // the Ground element, aware of anything Ground
export const Parity = '≍';     // a signum held to a reference it follows

/** Segment to its glyph. */
export const CommandToSymbol: Record<string, string> = {
    i: I,
    am: AM,
    ix: IX,
    ax: AX,
    by: BY,
    at: AT,
    so: SO,
    se: SE,
    as: AS,
    is: IS,
    of: OF,
};

/** What each command means, for a tooltip. */
export const CommandDescriptions: Record<string, string> = {
    i: 'Self — Your vantage point into QNTX',
    am: 'Being — The node, the server',
    ix: 'Ingest — Import external data',
    ax: 'Expand — Query and surface related context',
    by: 'Actor — Origin of action (creator/source/user)',
    at: 'Temporal — Time marker/moment',
    so: 'Therefore — Consequent action/trigger',
    se: 'Semantic — Meaning-based search and entailment',
    as: 'Assert — Emit an attestation',
    is: '',
    of: 'Membership — Element-of/belonging in attestations',
};
