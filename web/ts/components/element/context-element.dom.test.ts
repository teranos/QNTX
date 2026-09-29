/**
 * @jest-environment jsdom
 *
 * Context Element — the context as a thing in its own right.
 */

import { describe, test, expect, beforeEach } from 'bun:test';
import { contextElementId, contextSegment, renderContextStats } from './context-element';
import { OF } from '../../sym';
import type { Attestation } from '../../generated/proto/plugin/grpc/protocol/atsstore';

const anAttestation = (over: Partial<Attestation> = {}): Attestation => ({
    id: 'AS-BATCH-CRAWLTIM-LEVIBATC-QU7JWN2F',
    subjects: ['batch'],
    predicates: ['crawl-timeout'],
    contexts: ['levi:batch'],
    actors: ['alice'],
    ...over,
} as Attestation);

const inLevi: Attestation[] = [
    anAttestation({ subjects: ['batch'], predicates: ['crawl-timeout'] }),
    anAttestation({ subjects: ['batch'], predicates: ['retried'] }),
    anAttestation({ subjects: ['crawl'], predicates: ['crawl-timeout'], actors: ['bob'] }),
];

const rowsIn = (container: HTMLElement, cls: string): (string | null)[] =>
    Array.from(container.querySelectorAll(`.${cls} .sparkline-row`))
        .map((row) => row.children[0].textContent);

describe('What the context element is', () => {
    test('it draws ∈, the mark of the segment a context sits in', () => {
        expect(contextSegment().symbol).toBe(OF);
        expect(OF).toBe('∈');
    });

    test('it asks the store by context', () => {
        expect(contextSegment().param).toBe('context');
    });

    test('the id says which context it is about', () => {
        expect(contextElementId('levi:batch')).toBe('context-levi:batch');
    });

    test('it shows the other two segments, then the actors', () => {
        expect(contextSegment().sections.map(s => s.label))
            .toEqual(['Related subjects', 'Related predicates', 'Related actors']);
    });
});

describe('What is filed in a context', () => {
    let container: HTMLElement;

    beforeEach(() => {
        container = document.createElement('div');
        document.body.appendChild(container);
    });

    test('subjects, and they open theirs', () => {
        renderContextStats(container, 'levi:batch', inLevi);
        expect(rowsIn(container, 'context-subjects')).toEqual(['batch', 'crawl']);
        const first = container.querySelector('.context-subjects .sparkline-row')?.children[0] as HTMLElement;
        expect(first.dataset.segmentPress).toBe('subject');
    });

    test('predicates, and they open theirs', () => {
        renderContextStats(container, 'levi:batch', inLevi);
        expect(rowsIn(container, 'context-predicates')).toEqual(['crawl-timeout', 'retried']);
        const first = container.querySelector('.context-predicates .sparkline-row')?.children[0] as HTMLElement;
        expect(first.dataset.segmentPress).toBe('predicate');
    });

    test('how much it was folded out of is said before the sections', () => {
        renderContextStats(container, 'levi:batch', inLevi);
        expect(container.textContent).toContain('3 filed in this context');
    });
});
