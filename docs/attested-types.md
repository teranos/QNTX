# Attested Types

Types exist through [attestation](attestation.md) - 'restaurant' is real because someone attested it, not because a schema declares it. That attestation carries everything: what fields matter, which ones are searchable, how it appears visually, and crucially, who made these decisions and when. Types can contradict, overlap, and evolve because they're attestations - the mess is the message. Multiple actors might attest different meanings for 'restaurant' - a health inspector sees safety fields, a food critic sees ambiance, a delivery app sees logistics. This multiplicity isn't a problem to solve - it's the actual shape of knowledge in contested domains. Types become part of the conversation, not the rules that govern it.

## Symbol

`⊢` (turnstile) — an actor's judgment that a pattern deserves a name. From type theory: "I assert this has type." Available as `sym.Type` (Go) and `Type` from `@generated/sym.js` (frontend).

## Attestation format

A type attestation is: `[typeSubject] is type`. No context — a type exists because it was attested, not because it belongs to a namespace. The `source` field records who attested it. Attributes carry display metadata (color, label, opacity) and semantic information. `rich_string_fields` declares which attribute names contain searchable text (used for full-text search and embedding). `array_fields` declares which attributes hold lists.

The type name is its own actor (self-certifying in typespace), avoiding bounded storage limits.

## Identity

Type attestations use compact ASUIDs: `TY-{NAME}-{SUFFIX}` (e.g. `TY-COMIT-7K4M3B9X`).

The prefix carries the semantics — no need to repeat "TYPE" as a segment.
