/**
 * Predicate Glyph (=) — the predicate as a thing in its own right.
 *
 * Clicking a predicate spawns the predicate glyph, the same way clicking an
 * attestation spawns the attestation glyph.
 *
 * What it shows, in order: related subjects, related contexts and actors, and
 * attributes — folded out of the attestations filed under this predicate, the
 * way a sigma folds its observations. The subjects and contexts are pressable,
 * and open theirs.
 */

import { IS } from '../../sym';
import { log, SEG } from '../../logger';
import { openSegmentGlyph, renderSegmentStats, segmentGlyphId, type Segment } from './segment-glyph';
import { pressable } from './segment-press';
import type { Attestation } from '../../generated/proto/plugin/grpc/protocol/atsstore';

export { tallyOf, attributeTallies } from './segment-glyph';

/**
 * Built when asked rather than held at module scope: the bundler resolves a
 * const that points at another const to undefined (web/CLAUDE.md), and every
 * symbol here is one.
 */
export function predicateSegment(): Segment {
    return {
        kind: 'predicate',
        symbol: IS,
        param: 'predicate',
        filed: () => 'filed under this predicate',
        reading: (value) => `Reading what is filed under ${value}…`,
        sections: [
            {
                label: 'Related subjects',
                className: 'predicate-subjects',
                pick: (a: Attestation) => a.subjects,
                cell: pressable('subject'),
            },
            {
                label: 'Related contexts',
                className: 'predicate-contexts',
                pick: (a: Attestation) => a.contexts,
                cell: pressable('context'),
            },
            {
                label: 'Related actors',
                className: 'predicate-actors',
                pick: (a: Attestation) => a.actors,
                cell: pressable('actor'),
            },
        ],
    };
}

/** One glyph per predicate, so its id says which predicate it is about. */
export function predicateGlyphId(predicate: string): string {
    return segmentGlyphId(predicateSegment(), predicate);
}

/** Exported for tests: the three sections, in the order they are asked for. */
export function renderPredicateStats(container: HTMLElement, predicate: string, attestations: Attestation[]): void {
    renderSegmentStats(container, predicateSegment(), predicate, attestations);
}

/** Opens one predicate as its own glyph. */
export function openPredicateGlyph(predicate: string): void {
    log.debug(SEG.GLYPH, `[PredicateGlyph] opening ${predicate}`);
    openSegmentGlyph(predicateSegment(), predicate);
}
