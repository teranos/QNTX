/**
 * Tests for meld detection — proximity checks and bidirectional target finding
 *
 * Personas:
 * - Tim: Happy path user, normal workflows
 */

import { describe, test, expect } from 'bun:test';
import { findMeldTarget, PROXIMITY_THRESHOLD } from './meld-detect';
import { uiState } from '../../../state/ui';

/** Helper: mock getBoundingClientRect on an element */
function mockRect(el: HTMLElement, rect: { left: number; top: number; width: number; height: number }) {
    el.getBoundingClientRect = () => ({
        left: rect.left, top: rect.top,
        right: rect.left + rect.width, bottom: rect.top + rect.height,
        width: rect.width, height: rect.height,
        x: rect.left, y: rect.top,
        toJSON: () => ({})
    });
}

describe('Reverse Meld Detection - Tim (Happy Path)', () => {
    test('Tim drags prompt toward ax from the right — reverse meld detected', () => {
        const canvas = document.createElement('div');
        document.body.appendChild(canvas);

        const axElement = document.createElement('div');
        axElement.className = 'canvas-ax-glyph';
        axElement.setAttribute('data-glyph-id', 'ax1');
        canvas.appendChild(axElement);

        const promptElement = document.createElement('div');
        promptElement.className = 'canvas-prompt-glyph';
        promptElement.setAttribute('data-glyph-id', 'prompt1');
        canvas.appendChild(promptElement);

        // ax on the left, prompt approaching from the right (gap < threshold)
        mockRect(axElement, { left: 100, top: 100, width: 200, height: 150 });
        mockRect(promptElement, { left: 320, top: 100, width: 200, height: 150 });

        // Prompt is dragged — findMeldTarget should detect ax via reverse lookup
        const result = findMeldTarget(promptElement);

        expect(result.target).toBe(axElement);
        expect(result.reversed).toBe(true);
        expect(result.direction).toBe('right');
        expect(result.distance).toBeLessThan(PROXIMITY_THRESHOLD);

        document.body.innerHTML = '';
    });

    test('Tim drags prompt toward ax from the right — forward check finds nothing', () => {
        const canvas = document.createElement('div');
        document.body.appendChild(canvas);

        const axElement = document.createElement('div');
        axElement.className = 'canvas-ax-glyph';
        axElement.setAttribute('data-glyph-id', 'ax1');
        canvas.appendChild(axElement);

        const promptElement = document.createElement('div');
        promptElement.className = 'canvas-prompt-glyph';
        promptElement.setAttribute('data-glyph-id', 'prompt1');
        canvas.appendChild(promptElement);

        // ax on the left, prompt on the right — prompt has no right→ax port
        mockRect(axElement, { left: 100, top: 100, width: 200, height: 150 });
        mockRect(promptElement, { left: 320, top: 100, width: 200, height: 150 });

        const result = findMeldTarget(promptElement);

        // Should NOT find ax via forward lookup (prompt→ax is not in registry)
        // but SHOULD find via reverse (ax→right→prompt)
        expect(result.target).toBe(axElement);
        expect(result.reversed).toBe(true);

        document.body.innerHTML = '';
    });

    test('Tim drags ax toward prompt from the left — forward meld detected (not reversed)', () => {
        const canvas = document.createElement('div');
        document.body.appendChild(canvas);

        const axElement = document.createElement('div');
        axElement.className = 'canvas-ax-glyph';
        axElement.setAttribute('data-glyph-id', 'ax1');
        canvas.appendChild(axElement);

        const promptElement = document.createElement('div');
        promptElement.className = 'canvas-prompt-glyph';
        promptElement.setAttribute('data-glyph-id', 'prompt1');
        canvas.appendChild(promptElement);

        // ax on the left approaching prompt on the right
        mockRect(axElement, { left: 100, top: 100, width: 200, height: 150 });
        mockRect(promptElement, { left: 320, top: 100, width: 200, height: 150 });

        const result = findMeldTarget(axElement);

        expect(result.target).toBe(promptElement);
        expect(result.reversed).toBe(false);
        expect(result.direction).toBe('right');

        document.body.innerHTML = '';
    });
});

describe('Subcanvas top/bottom disambiguation', () => {
    test('Tim drags subcanvas below another subcanvas — detects bottom (not top)', () => {
        // Bug: subcanvas has both 'top' and 'bottom' ports. Forward 'top' detection
        // (AAA looks up at BBB) must not beat reverse 'bottom' (BBB looks down at AAA),
        // otherwise the edge from=BBB,to=AAA,direction=top places AAA above BBB.
        const canvas = document.createElement('div');
        document.body.appendChild(canvas);

        const bbb = document.createElement('div');
        bbb.className = 'canvas-subcanvas-glyph';
        bbb.setAttribute('data-glyph-id', 'bbb');
        canvas.appendChild(bbb);

        const aaa = document.createElement('div');
        aaa.className = 'canvas-subcanvas-glyph';
        aaa.setAttribute('data-glyph-id', 'aaa');
        canvas.appendChild(aaa);

        // BBB on top, AAA directly below
        mockRect(bbb, { left: 100, top: 100, width: 200, height: 150 });
        mockRect(aaa, { left: 100, top: 280, width: 200, height: 150 });

        const result = findMeldTarget(aaa);

        expect(result.target).toBe(bbb);
        // Must be 'bottom' (reverse: BBB→AAA) not 'top' (forward: AAA→BBB)
        expect(result.direction).toBe('bottom');
        expect(result.reversed).toBe(true);
        expect(result.distance).toBeLessThan(PROXIMITY_THRESHOLD);

        document.body.innerHTML = '';
    });

    test('Tim drags subcanvas below BBB in CCC-BBB composition — detects bottom on BBB', () => {
        uiState.setCanvasCompositions([]);
        const canvas = document.createElement('div');
        document.body.appendChild(canvas);

        const ccc = document.createElement('div');
        ccc.className = 'canvas-subcanvas-glyph';
        ccc.setAttribute('data-glyph-id', 'ccc');
        const bbb = document.createElement('div');
        bbb.className = 'canvas-subcanvas-glyph';
        bbb.setAttribute('data-glyph-id', 'bbb');

        const comp = document.createElement('div');
        comp.className = 'melded-composition';
        comp.setAttribute('data-glyph-id', 'melded-ccc-bbb');
        comp.appendChild(ccc);
        comp.appendChild(bbb);
        canvas.appendChild(comp);

        uiState.setCanvasCompositions([{
            id: 'melded-ccc-bbb',
            edges: [{ from: 'ccc', to: 'bbb', direction: 'right', position: 0 }],
            x: 100, y: 100,
        }]);

        const aaa = document.createElement('div');
        aaa.className = 'canvas-subcanvas-glyph';
        aaa.setAttribute('data-glyph-id', 'aaa');
        canvas.appendChild(aaa);

        mockRect(ccc, { left: 100, top: 100, width: 200, height: 150 });
        mockRect(bbb, { left: 300, top: 100, width: 200, height: 150 });
        mockRect(aaa, { left: 300, top: 280, width: 200, height: 150 });

        const result = findMeldTarget(aaa);

        expect(result.target).toBe(bbb);
        expect(result.direction).toBe('bottom');
        expect(result.reversed).toBe(true);
        expect(result.distance).toBeLessThan(PROXIMITY_THRESHOLD);

        document.body.innerHTML = '';
        uiState.setCanvasCompositions([]);
    });
});

describe('findMeldTarget detects composition targets', () => {
    test('standalone prompt finds py inside composition via reverse detection', () => {
        uiState.setCanvasCompositions([]);
        const canvas = document.createElement('div');
        document.body.appendChild(canvas);

        // ax|py composition
        const ax = document.createElement('div');
        ax.className = 'canvas-ax-glyph';
        ax.setAttribute('data-glyph-id', 'ax1');
        const py = document.createElement('div');
        py.className = 'canvas-py-glyph';
        py.setAttribute('data-glyph-id', 'py1');

        const comp = document.createElement('div');
        comp.className = 'melded-composition';
        comp.setAttribute('data-glyph-id', 'melded-ax1-py1');
        comp.appendChild(ax);
        comp.appendChild(py);
        canvas.appendChild(comp);

        // Standalone prompt approaching from the right
        const prompt = document.createElement('div');
        prompt.className = 'canvas-prompt-glyph';
        prompt.setAttribute('data-glyph-id', 'prompt1');
        canvas.appendChild(prompt);

        // py (inside comp) is to the left, prompt is approaching
        mockRect(py, { left: 300, top: 100, width: 200, height: 150 });
        mockRect(prompt, { left: 520, top: 100, width: 200, height: 150 });
        // ax is further left, shouldn't be closest
        mockRect(ax, { left: 100, top: 100, width: 200, height: 150 });

        const result = findMeldTarget(prompt);

        // py inside composition should be found via reverse (py→right→prompt)
        expect(result.target).toBe(py);
        expect(result.reversed).toBe(true);
        expect(result.direction).toBe('right');
        expect(result.distance).toBeLessThan(PROXIMITY_THRESHOLD);

        document.body.innerHTML = '';
        uiState.setCanvasCompositions([]);
    });
});

