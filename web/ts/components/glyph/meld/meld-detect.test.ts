/**
 * Tests for meld detection — proximity checks and bidirectional target finding
 *
 * Personas:
 * - Tim: Happy path user, normal workflows
 */

import { describe, test, expect } from 'bun:test';
import { Window } from 'happy-dom';
import { findMeldTarget, PROXIMITY_THRESHOLD } from './meld-detect';
import { uiState } from '../../../state/ui';

// Setup happy-dom
const window = new Window();
const document = window.document;
globalThis.document = document as any;
globalThis.window = window as any;

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
