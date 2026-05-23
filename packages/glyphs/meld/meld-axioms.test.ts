/**
 * Meld Axiom tests
 *
 * Axiom: each side of a glyph accepts at most one connection.
 * A glyph with an occupied right-outgoing port cannot emit another
 * right edge; a glyph whose left (right-incoming) is occupied cannot
 * receive another right-incoming edge. Same for bottom/top.
 *
 * Duplicated from web/ts/components/glyph/meld/meld-axioms.test.ts
 * to live with the package source. The web/ copy may be removed once
 * the package owns its own CI.
 */

import { describe, test, expect } from 'bun:test';
import { getMeldOptions, isPortFree } from './meldability';

/** Helper: build a composition DOM with children matching the edge IDs */
function compWith(...glyphs: Array<{ id: string; cls: string }>): HTMLElement {
    const comp = document.createElement('div');
    comp.className = 'melded-composition';
    for (const g of glyphs) {
        const el = document.createElement('div');
        el.className = g.cls;
        el.setAttribute('data-glyph-id', g.id);
        comp.appendChild(el);
    }
    return comp;
}

describe('Spike: one glyph per side axiom', () => {

    test('py with right-outgoing occupied (py→prompt) cannot accept another right-append', () => {
        const comp = compWith(
            { id: 'py1', cls: 'canvas-py-glyph' },
            { id: 'prompt1', cls: 'canvas-prompt-glyph' },
        );
        const edges = [{ from: 'py1', to: 'prompt1', direction: 'right' }];

        const options = getMeldOptions('canvas-prompt-glyph', comp, edges);

        const pyAppend = options.find(o => o.glyphId === 'py1' && o.direction === 'right');
        expect(pyAppend).toBeUndefined();
    });

    test('prompt with left side occupied (py→prompt) cannot receive second left meld', () => {
        const comp = compWith(
            { id: 'py1', cls: 'canvas-py-glyph' },
            { id: 'prompt1', cls: 'canvas-prompt-glyph' },
        );
        const edges = [{ from: 'py1', to: 'prompt1', direction: 'right' }];

        const options = getMeldOptions('canvas-ax-glyph', comp, edges);

        const promptPrepend = options.find(o => o.glyphId === 'prompt1');
        expect(promptPrepend).toBeUndefined();

        const pyPrepend = options.find(o => o.glyphId === 'py1' && o.incomingRole === 'from');
        expect(pyPrepend).toBeDefined();
        expect(pyPrepend!.direction).toBe('right');
    });

    test('py with bottom occupied (py→result) rejects second bottom attachment', () => {
        const comp = compWith(
            { id: 'py1', cls: 'canvas-py-glyph' },
            { id: 'result1', cls: 'canvas-result-glyph' },
        );
        const edges = [{ from: 'py1', to: 'result1', direction: 'bottom' }];

        const options = getMeldOptions('canvas-result-glyph', comp, edges);

        const pyBottom = options.find(o => o.glyphId === 'py1' && o.direction === 'bottom');
        expect(pyBottom).toBeUndefined();
    });

    test('py with right occupied still allows bottom meld (different side)', () => {
        const comp = compWith(
            { id: 'py1', cls: 'canvas-py-glyph' },
            { id: 'prompt1', cls: 'canvas-prompt-glyph' },
        );
        const edges = [{ from: 'py1', to: 'prompt1', direction: 'right' }];

        const options = getMeldOptions('canvas-result-glyph', comp, edges);

        const pyBottom = options.find(o => o.glyphId === 'py1' && o.direction === 'bottom');
        expect(pyBottom).toBeDefined();
        expect(pyBottom!.incomingRole).toBe('to');
    });
});

describe('Jenny: saturated chain', () => {

    test('[ax → py → prompt] — right axis fully saturated, bottom axis open', () => {
        const comp = compWith(
            { id: 'ax1', cls: 'canvas-ax-glyph' },
            { id: 'py1', cls: 'canvas-py-glyph' },
            { id: 'prompt1', cls: 'canvas-prompt-glyph' },
        );
        const edges = [
            { from: 'ax1', to: 'py1', direction: 'right' },
            { from: 'py1', to: 'prompt1', direction: 'right' },
        ];

        expect(isPortFree('ax1', 'right', 'outgoing', edges)).toBe(false);
        expect(isPortFree('py1', 'right', 'incoming', edges)).toBe(false);
        expect(isPortFree('py1', 'right', 'outgoing', edges)).toBe(false);
        expect(isPortFree('prompt1', 'right', 'incoming', edges)).toBe(false);

        expect(isPortFree('py1', 'bottom', 'outgoing', edges)).toBe(true);
        expect(isPortFree('prompt1', 'bottom', 'outgoing', edges)).toBe(true);

        expect(getMeldOptions('canvas-py-glyph', comp, edges)).toEqual([]);
        expect(getMeldOptions('canvas-ax-glyph', comp, edges)).toEqual([]);

        const resultOptions = getMeldOptions('canvas-result-glyph', comp, edges);
        expect(resultOptions.length).toBe(2);
        expect(resultOptions.find(o => o.glyphId === 'py1' && o.direction === 'bottom')).toBeDefined();
        expect(resultOptions.find(o => o.glyphId === 'prompt1' && o.direction === 'bottom')).toBeDefined();
    });
});
