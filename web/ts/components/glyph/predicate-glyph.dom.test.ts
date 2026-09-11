/**
 * @jest-environment jsdom
 *
 * Predicate Glyph — the predicate as a thing in its own right.
 */

import { describe, test, expect, beforeEach } from 'bun:test';
import { predicateGlyphId, tallyOf, attributeTallies, renderPredicateStats } from './predicate-glyph.ts';
import type { Attestation } from '../../generated/proto/plugin/grpc/protocol/atsstore';

const anAttestation = (over: Partial<Attestation> = {}): Attestation => ({
    id: 'AS-1788000000000-abcdef',
    subjects: ['batch'],
    predicates: ['crawl-timeout'],
    contexts: ['levi:batch'],
    actors: ['did:key:z6Mkalice'],
    timestamp: 1788000000000,
    source: 'handler',
    ...over,
} as Attestation);

const filed: Attestation[] = [
    anAttestation({ subjects: ['batch'], contexts: ['levi:batch'], actors: ['alice'], attributes: '{"host":"golem.club"}' }),
    anAttestation({ subjects: ['batch'], contexts: ['levi:batch'], actors: ['bob'], attributes: '{"host":"golem.club"}' }),
    anAttestation({ subjects: ['crawl'], contexts: ['levi:crawl'], actors: ['alice'], attributes: '{"host":"other.site"}' }),
];

const sectionRows = (container: HTMLElement, cls: string): (string | null)[][] =>
    Array.from(container.querySelectorAll(`.${cls} .stand-tally`))
        .map((row) => Array.from(row.children).map((cell) => cell.textContent));

describe('Predicate glyph id', () => {
    test('the id says which predicate it is about', () => {
        expect(predicateGlyphId('staand:page_view')).toBe('predicate-staand:page_view');
    });

    test('the same predicate is the same id, so it reopens rather than duplicates', () => {
        expect(predicateGlyphId('crawl-timeout')).toBe(predicateGlyphId('crawl-timeout'));
    });
});

describe('Folding what is filed under a predicate', () => {
    test('subjects counted, most-seen first', () => {
        expect(tallyOf(filed, (a) => a.subjects)).toEqual([
            { name: 'batch', count: 2 },
            { name: 'crawl', count: 1 },
        ]);
    });

    test('contexts counted the same way', () => {
        expect(tallyOf(filed, (a) => a.contexts)).toEqual([
            { name: 'levi:batch', count: 2 },
            { name: 'levi:crawl', count: 1 },
        ]);
    });

    test('actors counted the same way', () => {
        expect(tallyOf(filed, (a) => a.actors)).toEqual([
            { name: 'alice', count: 2 },
            { name: 'bob', count: 1 },
        ]);
    });

    test('a missing field counts nothing rather than counting undefined', () => {
        expect(tallyOf([anAttestation({ subjects: undefined })], (a) => a.subjects)).toEqual([]);
    });

    test('every value of a multi-value field is counted', () => {
        const many = [anAttestation({ subjects: ['a', 'b'] }), anAttestation({ subjects: ['b'] })];
        expect(tallyOf(many, (a) => a.subjects)).toEqual([
            { name: 'b', count: 2 },
            { name: 'a', count: 1 },
        ]);
    });
});

describe('Attributes across what is filed', () => {
    test('one tally per key, values counted under it', () => {
        expect(attributeTallies(filed)).toEqual([
            { key: 'host', items: [{ name: 'golem.club', count: 2 }, { name: 'other.site', count: 1 }] },
        ]);
    });

    test('keys are ordered so the same set reads the same way twice', () => {
        const two = [anAttestation({ attributes: '{"zeta":"1","alpha":"2"}' })];
        expect(attributeTallies(two).map((t) => t.key)).toEqual(['alpha', 'zeta']);
    });

    test('an attestation with no attributes contributes none', () => {
        expect(attributeTallies([anAttestation({ attributes: undefined })])).toEqual([]);
    });

    test('a non-string value is kept rather than dropped', () => {
        const nested = [anAttestation({ attributes: '{"count":3}' })];
        expect(attributeTallies(nested)).toEqual([
            { key: 'count', items: [{ name: '3', count: 1 }] },
        ]);
    });
});

describe('What the glyph shows', () => {
    let container: HTMLElement;

    beforeEach(() => {
        container = document.createElement('div');
        document.body.appendChild(container);
    });

    test('related subjects, then contexts and actors, then attributes', () => {
        renderPredicateStats(container, 'crawl-timeout', filed);
        const headings = Array.from(container.querySelectorAll('div'))
            .map((d) => d.textContent)
            .filter((t): t is string => t !== null);
        const order = ['Related subjects', 'Related contexts', 'Related actors', 'host'];
        const found = order.map((h) => headings.findIndex((t) => t === h));
        expect(found.every((i) => i >= 0)).toBe(true);
        expect([...found].sort((a, b) => a - b)).toEqual(found);
    });

    test('subjects section holds the subjects', () => {
        renderPredicateStats(container, 'crawl-timeout', filed);
        expect(sectionRows(container, 'predicate-subjects')).toEqual([
            ['batch', '2'],
            ['crawl', '1'],
        ]);
    });

    test('contexts and actors are their own sections', () => {
        renderPredicateStats(container, 'crawl-timeout', filed);
        expect(sectionRows(container, 'predicate-contexts')).toEqual([
            ['levi:batch', '2'],
            ['levi:crawl', '1'],
        ]);
        expect(sectionRows(container, 'predicate-actors')).toEqual([
            ['alice', '2'],
            ['bob', '1'],
        ]);
    });

    test('attributes are shown under their key', () => {
        renderPredicateStats(container, 'crawl-timeout', filed);
        expect(sectionRows(container, 'predicate-attributes')).toEqual([
            ['golem.club', '2'],
            ['other.site', '1'],
        ]);
    });

    test('how much it was folded out of is said before the sections', () => {
        renderPredicateStats(container, 'crawl-timeout', filed);
        expect(container.textContent).toContain('3 filed under this predicate');
    });

    test('nothing filed still draws the sections rather than an empty panel', () => {
        renderPredicateStats(container, 'crawl-timeout', []);
        expect(container.textContent).toContain('Related subjects');
        expect(container.textContent).toContain('Attributes');
        expect(container.textContent).toContain('nothing recorded');
    });
});
