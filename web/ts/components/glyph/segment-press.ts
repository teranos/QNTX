/**
 * A name in one segment glyph that opens another's.
 *
 * Each segment glyph names the other two, so wiring them to each other directly
 * would have all three importing all three. The press resolves the one it needs
 * when it happens, which is the pattern type-result-line.ts already takes to
 * type-glyph.ts.
 */

import { log, SEG } from '../../logger';
import type { NameCell } from '../tally';

/** Which segment a press opens. */
export type SegmentKind = 'subject' | 'predicate' | 'context';

/** Open one segment's glyph for one value, reading the module when pressed. */
export function openSegment(kind: SegmentKind, value: string): void {
    const opened = kind === 'subject'
        ? import('./subject-glyph').then(m => m.openSubjectGlyph(value))
        : kind === 'predicate'
            ? import('./predicate-glyph').then(m => m.openPredicateGlyph(value))
            : import('./context-glyph').then(m => m.openContextGlyph(value));

    opened.catch((err: unknown) => {
        log.error(SEG.GLYPH, `[SegmentGlyph] the ${kind} glyph for ${value} did not open:`, err);
    });
}

/** A tally name that opens the segment glyph for what it names. */
export function pressable(kind: SegmentKind): NameCell {
    return (name: string) => {
        const span = document.createElement('span');
        span.textContent = name;
        span.className = `segment-press segment-press-${kind}`;
        span.style.cursor = 'pointer';
        span.dataset.segmentPress = kind;
        span.addEventListener('click', (e) => {
            e.stopPropagation();
            openSegment(kind, name);
        });
        return span;
    };
}
