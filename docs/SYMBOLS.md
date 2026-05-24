# Symbols

Reference implementation: [`sym/symbols.go`](https://github.com/teranos/QNTX/blob/main/sym/symbols.go)

## SEG (Segment)

An atomic unit of the attestation grammar. The segments (`i`, `am`, `ix`, `ax`, `by`, `at`, `so`, `se`, `as`, `is`, `of`) each have three layers:

- **seg** — the grammatical unit (what it IS)
- **sym** — the visual expression (how it LOOKS: `⋈`, `⨳`, `+`, `=`, `∈`, `⌬`, `✦`)
- **glyph** — the interactive manifestation (how you INTERACT with it) — not all symbols have a glyph

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
| `⫶` | Triplet | Grouped attestations sharing the same subject+predicate+context |
| `⊢` | Type | An actor's judgment that a pattern deserves a name |
| `Σ` | Sigma | Distilled/summarized attestation (sum of many observations) |

## System Symbols

Infrastructure and lifecycle markers:

| Symbol | Name | Purpose |
|--------|------|---------|
| `꩜` | Pulse | Async operations, always prefix Pulse-related logs. See [API](api/pulse-jobs.md) |
| `✿` | PulseOpen | Graceful startup with orphaned job recovery. See [Opening & Closing](development/grace.md) |
| `❀` | PulseClose | Graceful shutdown with checkpoint preservation. See [Opening & Closing](development/grace.md) |
| `⊔` | DB | Database/storage layer |
| `▣` | Prose | Documentation and prose content |
| `▤` | Doc | Document/file content (PDF, etc.) |
| `⌗` | Subcanvas | Nested canvas workspace |
| `⏿` | Watcher | Observer — rendered inline next to watched predicates in the UI. Color follows spice saturation: bright blue under low dilation, deep sea blue when relaxed, faded white when never fired |

## Manifestation

The visual form a glyph takes when it morphs beyond the GlyphRun. A glyph can manifest as a window, canvas, fullscreen overlay, modal, tooltip, or any other interactive surface. The same DOM element, the same identity — different manifestations. See [glyphs.md](vision/glyphs.md) for the full vision.
