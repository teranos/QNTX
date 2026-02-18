# AXIOMS

## Element Axiom

A glyph is exactly one DOM element for its entire lifetime. Glyphs are reparented, never cloned. Scroll position, form state, and rendered content survive every transition between manifestations.

Violation throws `INVARIANT VIOLATION` at runtime.

## One-Per-Side Axiom

Each side of a glyph accepts at most one meld connection. Enforced at detection, extension, and commit layers.

## Attestation Flow Axioms

**One attestation, one execution.** A downstream glyph fires once per incoming attestation. Not a batch. Not a list. If upstream produces five attestations, downstream fires five times.

**Everything flowing through the DAG is an attestation.** AX results, py output via `attest()`, prompt results — the unit of flow is always an attestation. No ephemeral intermediaries.

**Watching, not polling.** The meld edge is a live subscription. When an attestation enters the system that matches the edge's filter, the downstream glyph fires.

**The edge is the watcher.** A composition edge `from→to` declares a reactive subscription. The meld DAG compiles down to watcher subscriptions. Each edge IS a watcher definition scoped to the composition.

**Subscriptions compile eagerly.** The moment two glyphs meld, the subscription activates. Not on play. On meld. The DAG is live from the moment it's assembled.
