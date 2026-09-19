/**
 * A name in one segment element that opens another's.
 *
 * Each segment element names the other two, so wiring them to each other directly
 * would have all three importing all three. The press resolves the one it needs
 * when it happens, which is the pattern type-result-line.ts already takes to
 * type-element.ts.
 */

import { log, SEG } from '../../logger';
import type { NameCell } from '../tally';

/** Which segment a press opens. */
/**
 * What a press opens. The actor is here with the three because pressing one
 * works the same way, not because an actor is a segment of the triple.
 */
export type SegmentKind = 'subject' | 'predicate' | 'context' | 'actor';

/** Open one segment's element for one value, reading the module when pressed. */
export function openSegment(kind: SegmentKind, value: string): void {
    const opened = kind === 'subject'
        ? import('./subject-element').then(m => m.openSubjectElement(value))
        : kind === 'predicate'
            ? import('./predicate-element').then(m => m.openPredicateElement(value))
            : kind === 'context'
                ? import('./context-element').then(m => m.openContextElement(value))
                : import('./actor-element').then(m => m.openActorElement(value));

    opened.catch((err: unknown) => {
        log.error(SEG.ELEMENT, `[SegmentElement] the ${kind} element for ${value} did not open:`, err);
    });
}

/** A tally name that opens the segment element for what it names. */
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
