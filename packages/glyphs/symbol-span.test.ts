/**
 * Symbol rendering — one field, one mechanism (SYMRD).
 *
 * Personas:
 * - Tim: happy path — glyph.symbol becomes DOM through one helper
 * - Spike: edge cases — no symbol, symbol carried across a cursor morph
 * - Jenny: the paths that used to be blind — generic title bars, canvas-placed
 */

import { describe, test, expect, beforeEach } from 'bun:test';
import { createSymbolSpan, settleSymbolSpan } from './symbol-span';
import { canvasPlaced } from './manifestations/canvas-placed';
import { renderGlyphContent } from './manifestations/render-content';
import type { Glyph } from './glyph';

beforeEach(() => {
    document.body.innerHTML = '';
});

describe('Tim: the symbol span', () => {
    test('createSymbolSpan renders the symbol as .glyph-symbol', () => {
        const span = createSymbolSpan('⍟');
        expect(span.classList.contains('glyph-symbol')).toBe(true);
        expect(span.textContent).toBe('⍟');
    });

    // Title bar CSS gives flex: 1 to its first span — the symbol must not
    // take it from the title.
    test('the symbol keeps its natural width', () => {
        const span = createSymbolSpan('⍟');
        // happy-dom normalizes flex: none to its longhand form
        expect(['none', '0 0 auto']).toContain(span.style.flex);
    });
});

describe('Spike: symbol carried across a cursor morph', () => {
    // The span itself honors the Element Axioma — same element, new class.
    test('settleSymbolSpan settles the same element', () => {
        const cursorSpan = document.createElement('span');
        cursorSpan.className = 'glyph-cursor-symbol';
        cursorSpan.textContent = 'ax';

        const settled = settleSymbolSpan(cursorSpan);

        expect(settled).toBe(cursorSpan);
        expect(settled.classList.contains('glyph-cursor-symbol')).toBe(false);
        expect(settled.classList.contains('glyph-symbol')).toBe(true);
        expect(settled.textContent).toBe('ax');
        expect(['none', '0 0 auto']).toContain(settled.style.flex);
    });
});

describe('Jenny: native renderers display glyph.symbol', () => {
    function makeGlyph(overrides: Partial<Glyph> = {}): Glyph {
        return {
            id: 'sym-test-1',
            title: 'Self',
            renderContent: () => document.createElement('div'),
            ...overrides,
        };
    }

    // canvas-placed only showed a symbol handed over from the cursor morph;
    // a glyph placed any other way had none, and hosts recreated the span
    // themselves. glyph.symbol is enough now.
    test('canvasPlaced renders glyph.symbol when no symbolElement is carried', () => {
        const { titleBar } = canvasPlaced({
            glyph: makeGlyph({ symbol: '⍟' }),
            className: 'canvas-test-glyph',
            defaults: { x: 0, y: 0, width: 100, height: 100 },
            titleBar: { label: 'Self' },
            logLabel: 'Test',
        });

        const span = titleBar!.querySelector('.glyph-symbol');
        expect(span).not.toBeNull();
        expect(span!.textContent).toBe('⍟');
    });

    test('canvasPlaced still reuses a carried symbolElement — same element', () => {
        const carried = document.createElement('span');
        carried.className = 'glyph-cursor-symbol';
        carried.textContent = 'ax';

        const { titleBar } = canvasPlaced({
            glyph: makeGlyph({ symbol: 'ax', symbolElement: carried }),
            className: 'canvas-test-glyph',
            defaults: { x: 0, y: 0, width: 100, height: 100 },
            titleBar: { label: 'AX' },
            logLabel: 'Test',
        });

        const span = titleBar!.querySelector('.glyph-symbol');
        expect(span).toBe(carried);
    });

    // "QNTX gets ⍟ Self by building its own title bar" — not any more: the
    // generic title bar shows the symbol beside the plain-text title.
    test('the generic title bar renders symbol and title', () => {
        const element = document.createElement('div');
        document.body.appendChild(element);

        const { titleBar } = renderGlyphContent(element, makeGlyph({ symbol: '⍟' }), 'Test');

        const symbol = titleBar.querySelector('.glyph-symbol');
        expect(symbol).not.toBeNull();
        expect(symbol!.textContent).toBe('⍟');
        expect(titleBar.textContent).toContain('Self');
    });

    test('no symbol, no span — the title stands alone', () => {
        const element = document.createElement('div');
        document.body.appendChild(element);

        const { titleBar } = renderGlyphContent(element, makeGlyph(), 'Test');

        expect(titleBar.querySelector('.glyph-symbol')).toBeNull();
        expect(titleBar.textContent).toContain('Self');
    });
});
