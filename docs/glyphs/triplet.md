# Triplet Glyph (⫶)

The triplet is the primary way a user interacts with attestations. It groups all attestations sharing the same subject + predicate + context into one browsable glyph.

## Problem

Attestations are individual events. But users think in terms of claims — "batch is crawl-timeout of levi:batch" is one concept, not four rows. The current attestation glyph (+) shows exactly one attestation. When an ax query returns N identical triplets, the UI renders N identical rows, each spawning its own glyph. There is no "this claim, across all its occurrences" view.

## The Triplet

The **triplet** (⫶) is the natural unit of attestation interaction:

- On canvas: a triplet glyph holds all attestations with the same subject + predicate + context. Title bar shows the triplet and a count. Individual attestations (with differing timestamps, actors, attributes) are browsable inside via the existing pager pattern.
- In ax results: identical triplets collapse into one row with a count badge. Double-clicking opens the triplet glyph on canvas.
- As the default: when a user double-clicks an ax result, they get a triplet glyph, not an attestation glyph. The triplet is the primary surface.

The attestation glyph (+) remains for lone attestations that cannot be grouped into a triplet (single occurrence, no siblings).

## Grouping Key

Same **subject + predicate + context** = same triplet. Actor and timestamp vary — those are what you browse inside the glyph.

## Symbol

`⫶` — triple colon. Three dots = three parts of the triplet (subject, predicate, context).

## Hierarchy

| Symbol | Name | Role |
|--------|------|------|
| `⫶` | Triplet | Primary attestation interaction — grouped by claim |
| `+` | Attestation | Fallback for lone ungroupable attestations |
| `Σ` | Sigma | Statistical aggregate of many attestations |
| `⊢` | Type | Type definition (metadata about a category) |

## Palette

The triplet takes over as the primary attestation surface. Its palette is a quiet blue-grey — lighter than the current attestation azure, with a subtle blue touch. Easy on the eyes, high readability, low visual noise. The blue is a hint, not a statement.

The attestation glyph (+) inherits the current attestation palette (muted azure: `#8a969b` keyword, `#a0a8ad` value) as-is, since it becomes the secondary/fallback view.

Color migration:
- Triplet (⫶): new palette — slightly lighter, slight blue lean
- Attestation (+): keeps current azure palette unchanged
- Sigma (Σ): amber, unchanged
- Type (⊢): violet, unchanged

## Meta Pill

Hover the pill at the bottom of the title bar to see a summary of the group: actors, sources, time range. Up to 5 items shown directly; overflow behind a "+N more" label that expands on hover. Each item highlights on hover, click spawns the attestation glyph.

## Interactive Keywords

The title bar reads `as subject is predicate of context`. The keywords `as`, `is`, `of` are clickable navigation axes:

- **as** → spawns AX glyph querying the subject
- **is** → spawns AX glyph querying `is [predicate]`
- **of** → spawns AX glyph querying `of [context]`

Hover a keyword: it highlights and after 400ms the `⋈` symbol fades in. Click: an AX glyph spawns attached to the cursor; click again to place it on the canvas.

## File Layout

Attribute rendering shared across all attestation glyphs lives in `attestation-attrs.ts`. Bio-visualization renderers (FASTA, structure, AlphaFold) live in `glyph/bioviz/`. Canvas spawn logic is in `spawn-on-canvas.ts`.
