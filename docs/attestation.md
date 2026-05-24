# Attestation

The atomic unit of QNTX. A signed, immutable claim.

## The Quintuple

`[Subject] is [Predicate] of [Context] by [Actor] at [Time]`

| Field | Symbol | What it answers |
|-------|--------|-----------------|
| Subject | — | What is being described? |
| Predicate | `=` | What is being asserted? |
| Context | `∈` | In what scope? |
| Actor | `⌬` | Who claims this? |
| Time | `✦` | When? |

## Properties

- **Immutable.** Once written, never modified.
- **Append-only.** No retraction. To supersede a claim, attest a new one.
- **Actor-bearing.** Every attestation knows who said it. Two actors can make contradictory claims about the same subject — both are valid.
- **Convergent.** Sync is set union. Commutative, idempotent, no conflict resolution. Two claims aren't a conflict — they're two claims.

## The Triplet (⫶)

Subject + predicate + context. The *content* of the claim, stripped of provenance. This is what users see and interact with. The [triplet glyph](vision/triplet-glyph.md) groups all attestations sharing a triplet.

## The Sigma (Σ)

When bounded storage evicts old attestations, they are distilled into sigmas — compressed aggregates that preserve statistical shape (min/max/sum/count, histograms, frequencies) while releasing individual events. Sigmas are attestations. They can be recursively meta-distilled.

## Relation to the Datom

Datomic's datom is `[entity, attribute, value, transaction, added?]`. Same shape, same append-only accumulation model. The attestation adds the actor dimension — making every fact a situated claim — and removes retraction.
