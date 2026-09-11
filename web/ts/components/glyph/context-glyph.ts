/**
 * Context Glyph (∈) — the context as a thing in its own right.
 *
 * Clicking a context spawns the context glyph, the same way clicking an
 * attestation spawns the attestation glyph. It draws ∈, which is the mark of
 * of: the segment a context sits in.
 *
 * What it shows, in order: related subjects, related predicates and actors, and
 * attributes — folded out of what is filed in this context.
 */

import { OF } from '../../sym';
import { log, SEG } from '../../logger';
import { openSegmentGlyph, renderSegmentStats, segmentGlyphId, type Segment } from './segment-glyph';
import { pressable } from './segment-press';
import type { Attestation } from '../../generated/proto/plugin/grpc/protocol/atsstore';

/** Built when asked rather than held at module scope (see predicate-glyph.ts). */
export function contextSegment(): Segment {
    return {
        kind: 'context',
        symbol: OF,
        param: 'context',
        filed: () => 'filed in this context',
        reading: (value) => `Reading what is filed in ${value}…`,
        sections: [
            {
                label: 'Related subjects',
                className: 'context-subjects',
                pick: (a: Attestation) => a.subjects,
                cell: pressable('subject'),
            },
            {
                label: 'Related predicates',
                className: 'context-predicates',
                pick: (a: Attestation) => a.predicates,
                cell: pressable('predicate'),
            },
            {
                label: 'Related actors',
                className: 'context-actors',
                pick: (a: Attestation) => a.actors,
                cell: pressable('actor'),
            },
        ],
    };
}

/** One glyph per context, so its id says which context it is about. */
export function contextGlyphId(context: string): string {
    return segmentGlyphId(contextSegment(), context);
}

/** Exported for tests: the sections, in the order they are shown. */
export function renderContextStats(container: HTMLElement, context: string, attestations: Attestation[]): void {
    renderSegmentStats(container, contextSegment(), context, attestations);
}

/** Opens one context as its own glyph. */
export function openContextGlyph(context: string): void {
    log.debug(SEG.GLYPH, `[ContextGlyph] opening ${context}`);
    openSegmentGlyph(contextSegment(), context);
}
