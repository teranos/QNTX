/**
 * Subject Glyph (+) — the subject as a thing in its own right.
 *
 * Clicking a subject spawns the subject glyph, the same way clicking an
 * attestation spawns the attestation glyph. It draws +, which is the mark of
 * as: the segment a subject sits in.
 *
 * What it shows, in order: related predicates, related contexts and actors, and
 * attributes — folded out of what is attested about this subject.
 */

import { AS } from '../../sym';
import { log, SEG } from '../../logger';
import { openSegmentGlyph, renderSegmentStats, segmentGlyphId, type Segment } from './segment-glyph';
import { pressable } from './segment-press';
import type { Attestation } from '../../generated/proto/plugin/grpc/protocol/atsstore';

/** Built when asked rather than held at module scope (see predicate-glyph.ts). */
export function subjectSegment(): Segment {
    return {
        kind: 'subject',
        symbol: AS,
        param: 'subject',
        filed: () => 'attested about this subject',
        reading: (value) => `Reading what is attested about ${value}…`,
        sections: [
            {
                label: 'Related predicates',
                className: 'subject-predicates',
                pick: (a: Attestation) => a.predicates,
                cell: pressable('predicate'),
            },
            {
                label: 'Related contexts',
                className: 'subject-contexts',
                pick: (a: Attestation) => a.contexts,
                cell: pressable('context'),
            },
            {
                label: 'Related actors',
                className: 'subject-actors',
                pick: (a: Attestation) => a.actors,
                cell: pressable('actor'),
            },
        ],
    };
}

/** One glyph per subject, so its id says which subject it is about. */
export function subjectGlyphId(subject: string): string {
    return segmentGlyphId(subjectSegment(), subject);
}

/** Exported for tests: the sections, in the order they are shown. */
export function renderSubjectStats(container: HTMLElement, subject: string, attestations: Attestation[]): void {
    renderSegmentStats(container, subjectSegment(), subject, attestations);
}

/** Opens one subject as its own glyph. */
export function openSubjectGlyph(subject: string): void {
    log.debug(SEG.GLYPH, `[SubjectGlyph] opening ${subject}`);
    openSegmentGlyph(subjectSegment(), subject);
}
