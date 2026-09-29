/**
 * @jest-environment jsdom
 *
 * Predicate Element — the predicate as a thing in its own right.
 */

import { describe, test, expect, beforeEach } from 'bun:test';
import { predicateElementId, seenOf, attributesSeen, renderPredicateStats } from './predicate-element.ts';
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

const sectionRows = (container: HTMLElement, cls: string): (string | null)[] =>
    Array.from(container.querySelectorAll(`.${cls} .sparkline-row`))
        .map((row) => row.children[0].textContent);

describe('Predicate element id', () => {
    test('the id says which predicate it is about', () => {
        expect(predicateElementId('staand:page_view')).toBe('predicate-staand:page_view');
    });

    test('the same predicate is the same id, so it reopens rather than duplicates', () => {
        expect(predicateElementId('crawl-timeout')).toBe(predicateElementId('crawl-timeout'));
    });
});

describe('Folding what is filed under a predicate', () => {
    const at = (t: number, over: Partial<Attestation>) => anAttestation({ timestamp: t, ...over });
    const timed = [
        at(1, { subjects: ['batch'], attributes: '{"host":"golem.club"}' }),
        at(2, { subjects: ['batch'], attributes: '{"host":"golem.club"}' }),
        at(3, { subjects: ['crawl'], attributes: '{"host":"other.site"}' }),
    ];

    test('every value keeps when it was seen', () => {
        expect(seenOf(timed, (a) => a.subjects)).toEqual([
            { name: 'batch', times: [1, 2] },
            { name: 'crawl', times: [3] },
        ]);
    });

    test('a missing field is seen nowhere rather than as undefined', () => {
        expect(seenOf([anAttestation({ subjects: undefined })], (a) => a.subjects)).toEqual([]);
    });

    test('every value of a multi-value field is seen', () => {
        const many = [at(1, { subjects: ['a', 'b'] }), at(2, { subjects: ['b'] })];
        expect(seenOf(many, (a) => a.subjects)).toEqual([
            { name: 'a', times: [1] },
            { name: 'b', times: [1, 2] },
        ]);
    });
});

describe('Attributes across what is filed', () => {
    test('per key, each value and when it was seen', () => {
        expect(attributesSeen(filed)).toEqual([
            { key: 'host', items: [
                { name: 'golem.club', times: [1788000000000, 1788000000000] },
                { name: 'other.site', times: [1788000000000] },
            ] },
        ]);
    });

    test('keys are ordered so the same set reads the same way twice', () => {
        const two = [anAttestation({ attributes: '{"zeta":"1","alpha":"2"}' })];
        expect(attributesSeen(two).map((t) => t.key)).toEqual(['alpha', 'zeta']);
    });

    test('an attestation with no attributes contributes none', () => {
        expect(attributesSeen([anAttestation({ attributes: undefined })])).toEqual([]);
    });

    test('a non-string value is kept rather than dropped', () => {
        const nested = [anAttestation({ attributes: '{"count":3}' })];
        expect(attributesSeen(nested)).toEqual([
            { key: 'count', items: [{ name: '3', times: [1788000000000] }] },
        ]);
    });
});

describe('What the element shows', () => {
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
            'batch',
            'crawl',
        ]);
    });

    test('contexts and actors are their own sections', () => {
        renderPredicateStats(container, 'crawl-timeout', filed);
        expect(sectionRows(container, 'predicate-contexts')).toEqual([
            'levi:batch',
            'levi:crawl',
        ]);
        expect(sectionRows(container, 'predicate-actors')).toEqual([
            'alice',
            'bob',
        ]);
    });

    test('attributes are shown under their key', () => {
        renderPredicateStats(container, 'crawl-timeout', filed);
        expect(sectionRows(container, 'predicate-attributes')).toEqual([
            'golem.club',
            'other.site',
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
