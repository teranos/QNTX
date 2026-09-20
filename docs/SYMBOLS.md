# Symbols

Reference implementation: [`sym/symbols.go`](https://github.com/teranos/QNTX/blob/main/sym/symbols.go)

## SEG (Segment)

An atomic unit of the attestation grammar. The segments (`i`, `am`, `ix`, `ax`, `by`, `at`, `so`, `se`, `as`, `is`, `of`) each have three layers:

- **seg** — the grammatical unit (what it IS)
- **sym** — the visual expression (how it LOOKS: `⋈`, `⨳`, `+`, `=`, `∈`, `⌬`, `✦`)
- **element** — the interactive form (how you INTERACT with it) — not all symbols have an element. An element's forms beyond the tray — window, panel, canvas — are its manifestations: [teranos/elements VISION.md](https://github.com/teranos/elements/blob/main/VISION.md)

## Primary Segments

These symbols have UI components and keyboard shortcuts (user-configurable):

| Symbol | Command | Meaning | Usage |
|--------|---------|---------|--------|
| `⍟` | i | Self | Your vantage point into QNTX - the current user/session |
| `≡` | am | Configuration | System settings and state |
| `⨳` | ix | Ingest | Import external data |
| `⋈` | ax | Expand | Query and surface related context |
| `⌬` | by | Actor | All forms: creator, source, authenticated user |
| `✦` | at | Temporal | Time marker/moment |
| `⟶` | so | Therefore | Consequent action/trigger |
| `⊨` | se | Semantic | Meaning-based search and entailment |

## Attestation Building Blocks

Fundamental components of attestations (not UI elements):

| Symbol | Concept | Role in Attestation |
|--------|---------|---------------------|
| `+` | as | Assert - emit an attestation |
| `=` | is | |
| `∈` | of | Membership in "predicate OF context" |

*Note: Consider alternative typeable symbol for `∈` (of) for better keyboard accessibility*

## Derived Attestation Types

| Symbol | Name | Purpose |
|--------|------|---------|
| `⎔` | Attestation | One claim whole — its slots and its attributes together |
| `⫶` | Triplet | Grouped attestations sharing the same subject+predicate+context |
| `⊢` | Type | An actor's judgment that a pattern deserves a name |
| `Σ` | Sigma | Distilled/summarized attestation (sum of many observations) |

*The attestation element drew `+` until `+` went back to being the subject it
marks. A canvas saved before that still says `+`, and is read by what the
record holds rather than by the mark alone.*

## System Symbols

Infrastructure and lifecycle markers:

| Symbol | Name | Purpose |
|--------|------|---------|
| `꩜` | Pulse | Async operations, always prefix Pulse-related logs. See [API](https://github.com/teranos/QNTX/blob/main/server/openapi/openapi.json) |
| `✿` | PulseOpen | Graceful startup with orphaned job recovery. See [pulse/async/worker.go](https://github.com/teranos/QNTX/blob/main/pulse/async/worker.go) |
| `❀` | PulseClose | Graceful shutdown with checkpoint preservation. See [GRACE](adr/ADR-036-GRACE.md) |
| `⊔` | DB | Database/storage layer |
| `▣` | Prose | Documentation and prose content |
| `▤` | Doc | Document/file content (PDF, etc.) |
| `⌗` | Subcanvas | Nested canvas workspace |
| `⏿` | Watcher | Observer — rendered inline next to watched predicates in the UI. Color follows spice saturation: bright blue under low dilation, deep sea blue when relaxed, faded white when never fired |
