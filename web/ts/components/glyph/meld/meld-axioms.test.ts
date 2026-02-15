/**
 * Meld Axiom tests
 *
 * Axiom: each side of a glyph accepts at most one connection.
 * A glyph with an occupied right-outgoing port cannot emit another
 * right edge; a glyph whose left (right-incoming) is occupied cannot
 * receive another right-incoming edge. Same for bottom/top.
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
        // [py → prompt]  — py's right port is occupied
        // Try to attach a second prompt to the right of py
        // Even though py→prompt is class-compatible, py already sends right
        const comp = compWith(
            { id: 'py1', cls: 'canvas-py-glyph' },
            { id: 'prompt1', cls: 'canvas-prompt-glyph' },
        );
        const edges = [{ from: 'py1', to: 'prompt1', direction: 'right' }];

        const options = getMeldOptions('canvas-prompt-glyph', comp, edges);

        // py1 is NOT a leaf (has outgoing right) so must not appear as append anchor
        const pyAppend = options.find(o => o.glyphId === 'py1' && o.direction === 'right');
        expect(pyAppend).toBeUndefined();
    });

    test('prompt with left side occupied (py→prompt) cannot receive second left meld', () => {
        // [py → prompt]  — prompt's incoming-right is occupied by py
        // Dragging ax near prompt: class-wise ax→prompt is valid (right),
        // but prompt already has an incoming right edge from py.
        const comp = compWith(
            { id: 'py1', cls: 'canvas-py-glyph' },
            { id: 'prompt1', cls: 'canvas-prompt-glyph' },
        );
        const edges = [{ from: 'py1', to: 'prompt1', direction: 'right' }];

        const options = getMeldOptions('canvas-ax-glyph', comp, edges);

        // prompt1 must NOT appear as a prepend target (its left is taken)
        const promptPrepend = options.find(o => o.glyphId === 'prompt1');
        expect(promptPrepend).toBeUndefined();

        // ax→py1 prepend IS valid (py1 root, incoming-right free)
        const pyPrepend = options.find(o => o.glyphId === 'py1' && o.incomingRole === 'from');
        expect(pyPrepend).toBeDefined();
        expect(pyPrepend!.direction).toBe('right');
    });

    test('py with bottom occupied (py→result) rejects second bottom attachment', () => {
        // [py ↓ result]  — py's bottom port is occupied
        // Another result should not be offered below py
        const comp = compWith(
            { id: 'py1', cls: 'canvas-py-glyph' },
            { id: 'result1', cls: 'canvas-result-glyph' },
        );
        const edges = [{ from: 'py1', to: 'result1', direction: 'bottom' }];

        const options = getMeldOptions('canvas-result-glyph', comp, edges);

        // py1's bottom is occupied — no bottom option should reference py1
        const pyBottom = options.find(o => o.glyphId === 'py1' && o.direction === 'bottom');
        expect(pyBottom).toBeUndefined();
    });

    test('py with right occupied still allows bottom meld (different side)', () => {
        // [py → prompt]  — py's right is occupied, bottom is free
        // result should still be able to attach below py
        const comp = compWith(
            { id: 'py1', cls: 'canvas-py-glyph' },
            { id: 'prompt1', cls: 'canvas-prompt-glyph' },
        );
        const edges = [{ from: 'py1', to: 'prompt1', direction: 'right' }];

        const options = getMeldOptions('canvas-result-glyph', comp, edges);

        // py1's bottom is free — result CAN attach there
        const pyBottom = options.find(o => o.glyphId === 'py1' && o.direction === 'bottom');
        expect(pyBottom).toBeDefined();
        expect(pyBottom!.incomingRole).toBe('to');
    });
});

describe('Jenny: saturated chain', () => {

    test('[ax → py → prompt] — right axis fully saturated, bottom axis open', () => {
        // Every right-axis port is occupied:
        //   ax1: outgoing-right → py1
        //   py1: incoming-right ← ax1, outgoing-right → prompt1
        //   prompt1: incoming-right ← py1
        // No right-axis newcomer can join. But bottom ports on py1 and prompt1 are free.
        const comp = compWith(
            { id: 'ax1', cls: 'canvas-ax-glyph' },
            { id: 'py1', cls: 'canvas-py-glyph' },
            { id: 'prompt1', cls: 'canvas-prompt-glyph' },
        );
        const edges = [
            { from: 'ax1', to: 'py1', direction: 'right' },
            { from: 'py1', to: 'prompt1', direction: 'right' },
        ];

        // Verify port saturation via isPortFree
        expect(isPortFree('ax1', 'right', 'outgoing', edges)).toBe(false);
        expect(isPortFree('py1', 'right', 'incoming', edges)).toBe(false);
        expect(isPortFree('py1', 'right', 'outgoing', edges)).toBe(false);
        expect(isPortFree('prompt1', 'right', 'incoming', edges)).toBe(false);

        // Bottom ports remain open
        expect(isPortFree('py1', 'bottom', 'outgoing', edges)).toBe(true);
        expect(isPortFree('prompt1', 'bottom', 'outgoing', edges)).toBe(true);

        // No right-axis glyph can join
        expect(getMeldOptions('canvas-py-glyph', comp, edges)).toEqual([]);
        expect(getMeldOptions('canvas-ax-glyph', comp, edges)).toEqual([]);

        // But result CAN attach below py1 or prompt1
        const resultOptions = getMeldOptions('canvas-result-glyph', comp, edges);
        expect(resultOptions.length).toBe(2);
        expect(resultOptions.find(o => o.glyphId === 'py1' && o.direction === 'bottom')).toBeDefined();
        expect(resultOptions.find(o => o.glyphId === 'prompt1' && o.direction === 'bottom')).toBeDefined();
    });
});
