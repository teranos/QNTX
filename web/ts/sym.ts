/**
 * The canonical symbols, stable across UI, CLI and documentation.
 *
 * These are not a wire shape. Proto declares what the node and the browser send
 * each other (ADR-006); a bag of named constants is neither a message nor an
 * enum, and forcing it into one would describe it worse than this does.
 *
 * So it is written here and held to sym/symbols.go by sym.test.ts, which reads
 * the Go and fails if the two ever disagree. One place to change a symbol, and
 * a check that catches the other place being forgotten.
 */

// Primary SEG operators — these have UI components and commands.
export const I = '⍟';          // self — your vantage point into QNTX
export const AM = '≡';         // am — configuration and system settings
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

/** Symbol to its text command, for dual-mode acceptance. */
export const SymbolToCommand: Record<string, string> = {
    [I]: 'i',
    [AM]: 'am',
    [IX]: 'ix',
    [AX]: 'ax',
    [BY]: 'by',
    [AT]: 'at',
    [SO]: 'so',
    [SE]: 'se',
    [AS]: 'as',
    [IS]: 'is',
    [OF]: 'of',
};

/** Text command to its canonical symbol, for normalisation and display. */
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
    am: 'Configuration — System settings and state',
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
