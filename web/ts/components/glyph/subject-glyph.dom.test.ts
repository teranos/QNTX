/**
 * @jest-environment jsdom
 *
 * Subject Glyph — the subject as a thing in its own right.
 *
 * What it is for is knowing which predicates a subject carries, so that is the
 * section it leads with and the one most of these are about.
 */

import { describe, test, expect, beforeEach } from 'bun:test';
import { subjectGlyphId, subjectSegment, renderSubjectStats } from './subject-glyph';
import { AS } from '../../sym';
import type { Attestation } from '../../generated/proto/plugin/grpc/protocol/atsstore';

const anAttestation = (over: Partial<Attestation> = {}): Attestation => ({
    id: 'AS-BATCH-CRAWLTIM-LEVIBATC-QU7JWN2F',
    subjects: ['batch'],
    predicates: ['crawl-timeout'],
    contexts: ['levi:batch'],
    actors: ['alice'],
    timestamp: 1788000000000,
    source: 'handler',
    ...over,
} as Attestation);

const aboutBatch: Attestation[] = [
    anAttestation({ predicates: ['crawl-timeout'], contexts: ['levi:batch'], actors: ['alice'] }),
    anAttestation({ predicates: ['crawl-timeout'], contexts: ['levi:batch'], actors: ['bob'] }),
    anAttestation({ predicates: ['crawl-timeout'], contexts: ['levi:crawl'], actors: ['alice'] }),
    anAttestation({ predicates: ['retried'], contexts: ['levi:batch'], actors: ['alice'], attributes: '{"attempt":"2"}' }),
    anAttestation({ predicates: ['type'], contexts: ['levi:batch'], actors: ['carol'] }),
];

const rowsIn = (container: HTMLElement, cls: string): (string | null)[][] =>
    Array.from(container.querySelectorAll(`.${cls} .stand-tally`))
        .map((row) => Array.from(row.children).map((cell) => cell.textContent));

describe('What the subject glyph is', () => {
    test('it draws +, the mark of the segment a subject sits in', () => {
        expect(subjectSegment().symbol).toBe(AS);
        expect(AS).toBe('+');
    });

    test('it asks the store by subject', () => {
        expect(subjectSegment().param).toBe('subject');
    });

    test('the id says which subject it is about', () => {
        expect(subjectGlyphId('batch')).toBe('subject-batch');
    });

    test('the same subject is the same id, so it reopens rather than duplicates', () => {
        expect(subjectGlyphId('batch')).toBe(subjectGlyphId('batch'));
    });
});

describe('Which predicates the subject carries', () => {
    let container: HTMLElement;

    beforeEach(() => {
        container = document.createElement('div');
        document.body.appendChild(container);
    });

    test('predicates are what it leads with', () => {
        expect(subjectSegment().sections[0].label).toBe('Related predicates');
    });

    test('every predicate it carries, counted, most-carried first', () => {
        renderSubjectStats(container, 'batch', aboutBatch);
        expect(rowsIn(container, 'subject-predicates')).toEqual([
            ['crawl-timeout', '3'],
            ['retried', '1'],
            ['type', '1'],
        ]);
    });

    test('a predicate opens its own glyph', () => {
        renderSubjectStats(container, 'batch', aboutBatch);
        const first = container.querySelector('.subject-predicates .stand-tally')?.children[0] as HTMLElement;
        expect(first.dataset.segmentPress).toBe('predicate');
        expect(first.style.cursor).toBe('pointer');
    });

    test('a subject carrying none says so rather than drawing nothing', () => {
        renderSubjectStats(container, 'batch', [anAttestation({ predicates: undefined })]);
        expect(container.querySelector('.subject-predicates')?.textContent).toContain('nothing recorded');
    });
});

describe('The rest of what it shows', () => {
    let container: HTMLElement;

    beforeEach(() => {
        container = document.createElement('div');
        document.body.appendChild(container);
    });

    test('contexts and actors follow the predicates, in that order', () => {
        expect(subjectSegment().sections.map(s => s.label))
            .toEqual(['Related predicates', 'Related contexts', 'Related actors']);
    });

    test('contexts are counted and open theirs', () => {
        renderSubjectStats(container, 'batch', aboutBatch);
        expect(rowsIn(container, 'subject-contexts')).toEqual([
            ['levi:batch', '4'],
            ['levi:crawl', '1'],
        ]);
        const first = container.querySelector('.subject-contexts .stand-tally')?.children[0] as HTMLElement;
        expect(first.dataset.segmentPress).toBe('context');
    });

    test('actors are counted and are not a segment, so they open nothing', () => {
        renderSubjectStats(container, 'batch', aboutBatch);
        expect(rowsIn(container, 'subject-actors')).toEqual([
            ['alice', '3'],
            ['bob', '1'],
            ['carol', '1'],
        ]);
        const first = container.querySelector('.subject-actors .stand-tally')?.children[0] as HTMLElement;
        expect(first.dataset.segmentPress).toBeUndefined();
    });

    test('attributes are shown under their key', () => {
        renderSubjectStats(container, 'batch', aboutBatch);
        expect(rowsIn(container, 'subject-attributes')).toEqual([['2', '1']]);
    });

    test('how much it was folded out of is said before the sections', () => {
        renderSubjectStats(container, 'batch', aboutBatch);
        expect(container.textContent).toContain('5 attested about this subject');
    });

    test('the subject it is about is named', () => {
        renderSubjectStats(container, 'batch', aboutBatch);
        expect(container.textContent).toContain('batch');
    });
});
