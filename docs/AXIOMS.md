# AXIOMS

The axioms about a [glyph](vision/glyphs.md) alone live with the glyph system, in [packages/glyphs/AXIOMAS.md](../packages/glyphs/AXIOMAS.md). What follows is where the glyph system and QNTX meet: the glyph system draws the edge, QNTX decides what an edge means.

## Attestation Flow Axioms

**One attestation, one execution.** A downstream glyph fires once per incoming attestation. Not a batch. Not a list. If upstream produces five attestations, downstream fires five times.

**Everything flowing through the DAG is an attestation.** AX results, py output via `attest()`, prompt results — the unit of flow is always an attestation. No ephemeral intermediaries.

**Watching, not polling.** The meld edge is a live subscription. When an attestation enters the system that matches the edge's filter, the downstream glyph fires.

**The edge is the watcher.** A composition edge `from→to` declares a reactive subscription. The meld DAG compiles down to watcher subscriptions. Each edge IS a watcher definition scoped to the composition.

**Subscriptions compile eagerly.** The moment two glyphs meld, the subscription activates. Not on play. On meld. The DAG is live from the moment it's assembled.

## DAG Axiom

Compositions are DAGs. Cycles cannot form.
