/**
 * What a canvas saved before the attestation glyph moved to ⎔ means by "+".
 *
 * A canvas keeps the symbol and nothing else about which glyph a record is. An
 * attestation glyph placed before the move still says "+", and reading the mark
 * alone would draw it as whatever "+" means now. The content is what decides.
 */

import { describe, test, expect } from 'bun:test';
import { getGlyphTypeBySavedSymbol, getGlyphTypeBySymbol } from './glyph-registry';
import { AS, Attestation, Triplet } from '../../sym';

const anAttestation = JSON.stringify({
    id: 'AS-BATCH-CRAWLTIM-LEVIBATC-QU7JWN2F',
    subjects: ['batch'],
    predicates: ['crawl-timeout'],
    contexts: ['levi:batch'],
});

describe('a record saved under the old mark', () => {
    test('"+" holding one attestation is the attestation glyph', () => {
        expect(getGlyphTypeBySavedSymbol(AS, anAttestation)?.title).toBe('Attestation');
    });

    test('it is the same entry ⎔ resolves to', () => {
        expect(getGlyphTypeBySavedSymbol(AS, anAttestation))
            .toBe(getGlyphTypeBySymbol(Attestation));
    });

    test('a subject alone is enough to know it', () => {
        expect(getGlyphTypeBySavedSymbol(AS, JSON.stringify({ subjects: ['batch'] }))?.title)
            .toBe('Attestation');
    });

    test('a predicate alone is enough', () => {
        expect(getGlyphTypeBySavedSymbol(AS, JSON.stringify({ predicates: ['crawl-timeout'] }))?.title)
            .toBe('Attestation');
    });

    test('a context alone is enough', () => {
        expect(getGlyphTypeBySavedSymbol(AS, JSON.stringify({ contexts: ['levi:batch'] }))?.title)
            .toBe('Attestation');
    });
});

describe('what "+" does not claim', () => {
    test('"+" holding a bare string is left to whatever "+" means', () => {
        expect(getGlyphTypeBySavedSymbol(AS, JSON.stringify('batch')))
            .toBe(getGlyphTypeBySymbol(AS));
    });

    test('"+" holding nothing is left the same way', () => {
        expect(getGlyphTypeBySavedSymbol(AS)).toBe(getGlyphTypeBySymbol(AS));
    });

    test('"+" holding unparseable content is left the same way', () => {
        expect(getGlyphTypeBySavedSymbol(AS, 'batch is crawl-timeout'))
            .toBe(getGlyphTypeBySymbol(AS));
    });

    test('"+" holding a list is left the same way', () => {
        expect(getGlyphTypeBySavedSymbol(AS, JSON.stringify([{ subjects: ['batch'] }])))
            .toBe(getGlyphTypeBySymbol(AS));
    });

    test('"+" holding an object of something else is left the same way', () => {
        expect(getGlyphTypeBySavedSymbol(AS, JSON.stringify({ code: 'print(1)' })))
            .toBe(getGlyphTypeBySymbol(AS));
    });
});

describe('every other mark reads as itself', () => {
    test('⎔ is the attestation glyph', () => {
        expect(getGlyphTypeBySavedSymbol(Attestation)?.title).toBe('Attestation');
    });

    test('⫶ stays the triplet even holding attestations', () => {
        expect(getGlyphTypeBySavedSymbol(Triplet, `[${anAttestation}]`)?.title).toBe('Triplet');
    });

    test('a mark nothing is registered under resolves to nothing', () => {
        expect(getGlyphTypeBySavedSymbol('nothing-draws-this', anAttestation)).toBeUndefined();
    });
});
