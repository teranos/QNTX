/**
 * @jest-environment jsdom
 *
 * Actor Glyph — what an actor is often paired with.
 */

import { describe, test, expect, beforeEach } from 'bun:test';
import { actorGlyphId, actorSegment, renderActorStats } from './actor-glyph';
import { BY } from '../../sym';
import type { Attestation } from '../../generated/proto/plugin/grpc/protocol/atsstore';

const anAttestation = (over: Partial<Attestation> = {}): Attestation => ({
    id: 'AS-BATCH-CRAWLTIM-LEVIBATC-QU7JWN2F',
    subjects: ['batch'],
    predicates: ['crawl-timeout'],
    contexts: ['levi:batch'],
    actors: ['did:key:z6Mkalice'],
    ...over,
} as Attestation);

const byAlice: Attestation[] = [
    anAttestation({ predicates: ['crawl-timeout'], contexts: ['levi:batch'] }),
    anAttestation({ predicates: ['crawl-timeout'], contexts: ['levi:batch'] }),
    anAttestation({ predicates: ['crawl-timeout'], contexts: ['levi:crawl'] }),
    anAttestation({ predicates: ['handler'], contexts: ['levi:batch'] }),
    anAttestation({ predicates: ['module'], contexts: ['_'] }),
];

const rowsIn = (container: HTMLElement, cls: string): (string | null)[][] =>
    Array.from(container.querySelectorAll(`.${cls} .stand-tally`))
        .map((row) => Array.from(row.children).map((cell) => cell.textContent));

describe('What the actor glyph is', () => {
    test('it draws ⌬, the mark of by', () => {
        expect(actorSegment().symbol).toBe(BY);
        expect(BY).toBe('⌬');
    });

    test('it asks the store by actor', () => {
        expect(actorSegment().param).toBe('actor');
    });

    test('the id says which actor it is about', () => {
        expect(actorGlyphId('did:key:z6Mkalice')).toBe('actor-did:key:z6Mkalice');
    });

    test('the same actor is the same id, so it reopens rather than duplicates', () => {
        expect(actorGlyphId('ground')).toBe(actorGlyphId('ground'));
    });

    test('it shows what it is paired with, and nothing else', () => {
        expect(actorSegment().sections.map(s => s.label))
            .toEqual(['Often paired with', 'In these contexts']);
    });
});

describe('What an actor is paired with', () => {
    let container: HTMLElement;

    beforeEach(() => {
        container = document.createElement('div');
        document.body.appendChild(container);
    });

    test('predicates counted, most-paired first', () => {
        renderActorStats(container, 'did:key:z6Mkalice', byAlice);
        expect(rowsIn(container, 'actor-predicates')).toEqual([
            ['crawl-timeout', '3'],
            ['handler', '1'],
            ['module', '1'],
        ]);
    });

    test('contexts counted, most-used first', () => {
        renderActorStats(container, 'did:key:z6Mkalice', byAlice);
        expect(rowsIn(container, 'actor-contexts')).toEqual([
            ['levi:batch', '3'],
            ['_', '1'],
            ['levi:crawl', '1'],
        ]);
    });

    test('a predicate opens its own glyph', () => {
        renderActorStats(container, 'did:key:z6Mkalice', byAlice);
        const first = container.querySelector('.actor-predicates .stand-tally')?.children[0] as HTMLElement;
        expect(first.dataset.segmentPress).toBe('predicate');
    });

    test('a context opens its own glyph', () => {
        renderActorStats(container, 'did:key:z6Mkalice', byAlice);
        const first = container.querySelector('.actor-contexts .stand-tally')?.children[0] as HTMLElement;
        expect(first.dataset.segmentPress).toBe('context');
    });

    test('how much it was folded out of is said before the sections', () => {
        renderActorStats(container, 'did:key:z6Mkalice', byAlice);
        expect(container.textContent).toContain('5 attested by this actor');
    });

    test('an actor that attested one thing says so rather than drawing nothing', () => {
        renderActorStats(container, 'ground', []);
        expect(container.querySelector('.actor-predicates')?.textContent).toContain('nothing recorded');
        expect(container.querySelector('.actor-contexts')?.textContent).toContain('nothing recorded');
    });
});
