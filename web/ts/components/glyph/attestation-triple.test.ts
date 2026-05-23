/**
 * Tests for attestation triple rendering — "subject is predicate of context".
 */

import { describe, it, expect, mock } from 'bun:test';

mock.module('@generated/sym.js', () => ({
    AX: 'AX',
    Watcher: { repeat: (n: number) => 'W'.repeat(n) },
}));

mock.module('../../watcher-predicates', () => ({
    getWatchersByPredicate: () => new Map(),
    eyeStyle: () => ({ color: '#fff', shadow: 'none' }),
}));

mock.module('@qntx/glyphs', () => ({
    preventDrag: () => {},
}));

const { renderTriple } = await import('./attestation-triple');

import type { Attestation } from '../../generated/proto/plugin/grpc/protocol/atsstore';

function makeAttestation(overrides: Partial<Attestation> = {}): Attestation {
    return {
        subjects: ['batch'],
        predicates: ['crawl-timeout'],
        contexts: ['levi:batch'],
        actors: ['system'],
        ...overrides,
    } as Attestation;
}

const palette = { value: '#aaa', keyword: '#666' };

// -- Basic rendering --

describe('Tim: renderTriple basic output', () => {
    it('renders subject is predicate of context', () => {
        const el = renderTriple(makeAttestation(), { palette });
        expect(el.textContent).toBe('batch is crawl-timeout of levi:batch');
    });

    it('joins multiple subjects with comma', () => {
        const el = renderTriple(makeAttestation({ subjects: ['a', 'b', 'c'] }), { palette });
        expect(el.textContent).toContain('a, b, c');
    });

    it('joins multiple predicates with comma', () => {
        const el = renderTriple(makeAttestation({ predicates: ['x', 'y'] }), { palette });
        expect(el.textContent).toContain('x, y');
    });

    it('joins multiple contexts with comma', () => {
        const el = renderTriple(makeAttestation({ contexts: ['ctx1', 'ctx2'] }), { palette });
        expect(el.textContent).toContain('ctx1, ctx2');
    });

    it('defaults to span wrapper', () => {
        const el = renderTriple(makeAttestation(), { palette });
        expect(el.tagName).toBe('SPAN');
    });

    it('respects tag option', () => {
        const el = renderTriple(makeAttestation(), { palette, tag: 'div' });
        expect(el.tagName).toBe('DIV');
    });

    it('applies monospace font', () => {
        const el = renderTriple(makeAttestation(), { palette });
        expect(el.style.fontFamily).toBe('monospace');
    });
});

// -- Missing data --

describe('Spike: renderTriple missing fields', () => {
    it('shows N/A for missing subjects', () => {
        const el = renderTriple(makeAttestation({ subjects: undefined }), { palette });
        expect(el.textContent).toContain('N/A is crawl-timeout');
    });

    it('shows N/A for missing predicates', () => {
        const el = renderTriple(makeAttestation({ predicates: undefined }), { palette });
        expect(el.textContent).toContain('batch is N/A of');
    });

    it('shows N/A for missing contexts', () => {
        const el = renderTriple(makeAttestation({ contexts: undefined }), { palette });
        expect(el.textContent).toContain('of N/A');
    });

    it('shows N/A for empty arrays', () => {
        const el = renderTriple(makeAttestation({ subjects: [], predicates: [], contexts: [] }), { palette });
        expect(el.textContent).toBe('N/A is N/A of N/A');
    });
});

// -- showAsPrefix --

describe('Tim: showAsPrefix', () => {
    it('prepends "as " before subject', () => {
        const el = renderTriple(makeAttestation(), { palette, showAsPrefix: true });
        expect(el.textContent).toBe('as batch is crawl-timeout of levi:batch');
    });

    it('omits "as " by default', () => {
        const el = renderTriple(makeAttestation(), { palette });
        expect(el.textContent).not.toMatch(/^as /);
    });
});

// -- Color application --

describe('Tim: palette colors', () => {
    it('applies value color to subject', () => {
        const el = renderTriple(makeAttestation(), { palette });
        const spans = el.querySelectorAll('span') as NodeListOf<HTMLElement>;
        const subjectSpan = Array.from(spans).find(s => s.textContent === 'batch')!;
        // happy-dom keeps hex, JSDOM converts to rgb
        const c = subjectSpan.style.color;
        expect(c === '#aaa' || c.includes('170')).toBe(true);
    });

    it('applies keyword color to "is"', () => {
        const el = renderTriple(makeAttestation(), { palette });
        const spans = el.querySelectorAll('span') as NodeListOf<HTMLElement>;
        const isSpan = Array.from(spans).find(s => s.textContent === ' is ')!;
        const c = isSpan.style.color;
        expect(c === '#666' || c.includes('102')).toBe(true);
    });

    it('applies keyword color to "of"', () => {
        const el = renderTriple(makeAttestation(), { palette });
        const spans = el.querySelectorAll('span') as NodeListOf<HTMLElement>;
        const ofSpan = Array.from(spans).find(s => s.textContent === ' of ')!;
        const c = ofSpan.style.color;
        expect(c === '#666' || c.includes('102')).toBe(true);
    });
});

// -- onKeywordClick --

describe('Tim: onKeywordClick', () => {
    it('"is" click passes "is [predicate]"', () => {
        const clicked = mock(() => {});
        const el = renderTriple(makeAttestation(), { palette, onKeywordClick: clicked });
        const spans = el.querySelectorAll('span') as NodeListOf<HTMLElement>;
        const isSpan = Array.from(spans).find(s => s.textContent === ' is ')!;
        isSpan.click();
        expect(clicked).toHaveBeenCalledTimes(1);
        expect(clicked.mock.calls[0][0]).toBe('is crawl-timeout');
    });

    it('"of" click passes "of [context]"', () => {
        const clicked = mock(() => {});
        const el = renderTriple(makeAttestation(), { palette, onKeywordClick: clicked });
        const spans = el.querySelectorAll('span') as NodeListOf<HTMLElement>;
        const ofSpan = Array.from(spans).find(s => s.textContent === ' of ')!;
        ofSpan.click();
        expect(clicked).toHaveBeenCalledTimes(1);
        expect(clicked.mock.calls[0][0]).toBe('of levi:batch');
    });

    it('"as" click passes subject', () => {
        const clicked = mock(() => {});
        const el = renderTriple(makeAttestation(), { palette, showAsPrefix: true, onKeywordClick: clicked });
        const spans = el.querySelectorAll('span') as NodeListOf<HTMLElement>;
        const asSpan = Array.from(spans).find(s => s.textContent === 'as ')!;
        asSpan.click();
        expect(clicked).toHaveBeenCalledTimes(1);
        expect(clicked.mock.calls[0][0]).toBe('batch');
    });

    it('adds pointer cursor when clickable', () => {
        const el = renderTriple(makeAttestation(), { palette, onKeywordClick: () => {} });
        const spans = el.querySelectorAll('span') as NodeListOf<HTMLElement>;
        const isSpan = Array.from(spans).find(s => s.textContent === ' is ')!;
        expect(isSpan.style.cursor).toBe('pointer');
    });

    it('no pointer cursor without callback', () => {
        const el = renderTriple(makeAttestation(), { palette });
        const spans = el.querySelectorAll('span') as NodeListOf<HTMLElement>;
        const isSpan = Array.from(spans).find(s => s.textContent === ' is ')!;
        expect(isSpan.style.cursor).not.toBe('pointer');
    });
});

describe('Jenny: onKeywordClick with multiple values', () => {
    it('"is" joins multiple predicates', () => {
        const clicked = mock(() => {});
        const att = makeAttestation({ predicates: ['timeout', 'error'] });
        const el = renderTriple(att, { palette, onKeywordClick: clicked });
        const spans = el.querySelectorAll('span') as NodeListOf<HTMLElement>;
        const isSpan = Array.from(spans).find(s => s.textContent === ' is ')!;
        isSpan.click();
        expect(clicked.mock.calls[0][0]).toBe('is timeout, error');
    });

    it('"of" joins multiple contexts', () => {
        const clicked = mock(() => {});
        const att = makeAttestation({ contexts: ['ctx1', 'ctx2'] });
        const el = renderTriple(att, { palette, onKeywordClick: clicked });
        const spans = el.querySelectorAll('span') as NodeListOf<HTMLElement>;
        const ofSpan = Array.from(spans).find(s => s.textContent === ' of ')!;
        ofSpan.click();
        expect(clicked.mock.calls[0][0]).toBe('of ctx1, ctx2');
    });
});
