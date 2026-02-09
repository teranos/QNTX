/**
 * Tests for meld detection — proximity checks and bidirectional target finding
 *
 * Personas:
 * - Tim: Happy path user, normal workflows
 */

import { describe, test, expect } from 'bun:test';
import { Window } from 'happy-dom';
import { findMeldTarget, canInitiateMeld, canReceiveMeld, PROXIMITY_THRESHOLD } from './meld-detect';
import { performMeld } from './meld-composition';
import type { Glyph } from './glyph';
import { uiState } from '../../state/ui';

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

describe('Gate functions for compositions', () => {
    test('canInitiateMeld returns true for composition elements', () => {
        const comp = document.createElement('div');
        comp.className = 'melded-composition';
        expect(canInitiateMeld(comp)).toBe(true);
    });

    test('canReceiveMeld returns true for composition elements', () => {
        const comp = document.createElement('div');
        comp.className = 'melded-composition';
        expect(canReceiveMeld(comp)).toBe(true);
    });

    test('canInitiateMeld still works for glyph elements', () => {
        const ax = document.createElement('div');
        ax.className = 'canvas-ax-glyph';
        expect(canInitiateMeld(ax)).toBe(true);
    });

    test('canInitiateMeld returns false for unrelated elements', () => {
        const div = document.createElement('div');
        div.className = 'some-other-class';
        expect(canInitiateMeld(div)).toBe(false);
    });
});

describe('findMeldTarget detects composition near standalone glyph', () => {
    test('dragging py|py composition near standalone prompt glyph', () => {
        uiState.setCanvasCompositions([]);
        const canvas = document.createElement('div');
        document.body.appendChild(canvas);

        // Composition: py|py (being dragged)
        const py1 = document.createElement('div');
        py1.className = 'canvas-py-glyph';
        py1.setAttribute('data-glyph-id', 'py1');
        const py2 = document.createElement('div');
        py2.className = 'canvas-py-glyph';
        py2.setAttribute('data-glyph-id', 'py2');

        const comp = document.createElement('div');
        comp.className = 'melded-composition';
        comp.setAttribute('data-glyph-id', 'melded-py1-py2');
        comp.appendChild(py1);
        comp.appendChild(py2);
        canvas.appendChild(comp);

        // Standalone prompt
        const prompt = document.createElement('div');
        prompt.className = 'canvas-prompt-glyph';
        prompt.setAttribute('data-glyph-id', 'prompt1');
        canvas.appendChild(prompt);

        // Register composition in state
        uiState.setCanvasCompositions([
            { id: 'melded-py1-py2', edges: [{ from: 'py1', to: 'py2', direction: 'right', position: 0 }], x: 100, y: 100 }
        ]);

        // comp to the left, prompt to the right, gap < threshold
        mockRect(comp, { left: 100, top: 100, width: 300, height: 150 });
        mockRect(prompt, { left: 420, top: 100, width: 200, height: 150 });

        const result = findMeldTarget(comp);

        // Should find standalone prompt as target (py2 leaf → prompt is valid 'right')
        expect(result.target).toBe(prompt);
        expect(result.direction).toBe('right');
        expect(result.reversed).toBe(false);
        expect(result.distance).toBeLessThan(PROXIMITY_THRESHOLD);

        document.body.innerHTML = '';
        uiState.setCanvasCompositions([]);
    });
});

describe('findMeldTarget comp-to-comp via performMeld', () => {
    test('compositions created via performMeld are detected', () => {
        uiState.setCanvasCompositions([]);
        const canvas = document.createElement('div');
        document.body.appendChild(canvas);

        // Create comp1: ax|py via performMeld
        const ax = document.createElement('div');
        ax.className = 'canvas-ax-glyph';
        ax.setAttribute('data-glyph-id', 'ax1');
        ax.style.position = 'absolute';
        ax.style.left = '100px';
        ax.style.top = '100px';
        canvas.appendChild(ax);

        const py1 = document.createElement('div');
        py1.className = 'canvas-py-glyph';
        py1.setAttribute('data-glyph-id', 'py1');
        py1.style.position = 'absolute';
        py1.style.left = '200px';
        py1.style.top = '100px';
        canvas.appendChild(py1);

        const axGlyph: Glyph = { id: 'ax1', title: 'AX', renderContent: () => ax };
        const py1Glyph: Glyph = { id: 'py1', title: 'Py', renderContent: () => py1 };
        const comp1 = performMeld(ax, py1, axGlyph, py1Glyph, 'right');

        // Create comp2: py|prompt via performMeld
        const py2 = document.createElement('div');
        py2.className = 'canvas-py-glyph';
        py2.setAttribute('data-glyph-id', 'py2');
        py2.style.position = 'absolute';
        py2.style.left = '500px';
        py2.style.top = '100px';
        canvas.appendChild(py2);

        const prompt = document.createElement('div');
        prompt.className = 'canvas-prompt-glyph';
        prompt.setAttribute('data-glyph-id', 'prompt1');
        prompt.style.position = 'absolute';
        prompt.style.left = '600px';
        prompt.style.top = '100px';
        canvas.appendChild(prompt);

        const py2Glyph: Glyph = { id: 'py2', title: 'Py2', renderContent: () => py2 };
        const promptGlyph: Glyph = { id: 'prompt1', title: 'Prompt', renderContent: () => prompt };
        const comp2 = performMeld(py2, prompt, py2Glyph, promptGlyph, 'right');

        // Verify state has both compositions
        const comps = uiState.getCanvasCompositions();
        expect(comps.length).toBe(2);

        // Mock rects: comp1 to the left, comp2 to the right
        mockRect(comp1, { left: 100, top: 100, width: 300, height: 150 });
        mockRect(comp2, { left: 420, top: 100, width: 300, height: 150 });

        const result = findMeldTarget(comp1);

        expect(result.target).toBe(comp2);
        expect(result.direction).toBe('right');
        expect(result.distance).toBeLessThan(PROXIMITY_THRESHOLD);

        document.body.innerHTML = '';
        uiState.setCanvasCompositions([]);
    });
});

describe('findMeldTarget detects composition-to-composition', () => {
    test('dragging ax|py composition near py|prompt composition (user scenario)', () => {
        uiState.setCanvasCompositions([]);
        const canvas = document.createElement('div');
        document.body.appendChild(canvas);

        // Composition 1: ax|py (being dragged)
        const ax = document.createElement('div');
        ax.className = 'canvas-ax-glyph';
        ax.setAttribute('data-glyph-id', 'ax1');
        const py1 = document.createElement('div');
        py1.className = 'canvas-py-glyph';
        py1.setAttribute('data-glyph-id', 'py1');

        const comp1 = document.createElement('div');
        comp1.className = 'melded-composition';
        comp1.setAttribute('data-glyph-id', 'melded-ax1-py1');
        comp1.appendChild(ax);
        comp1.appendChild(py1);
        canvas.appendChild(comp1);

        // Composition 2: py|prompt (stationary) — py2 is the root, prompt is the leaf
        const py2 = document.createElement('div');
        py2.className = 'canvas-py-glyph';
        py2.setAttribute('data-glyph-id', 'py2');
        const prompt = document.createElement('div');
        prompt.className = 'canvas-prompt-glyph';
        prompt.setAttribute('data-glyph-id', 'prompt1');

        const comp2 = document.createElement('div');
        comp2.className = 'melded-composition';
        comp2.setAttribute('data-glyph-id', 'melded-py2-prompt1');
        comp2.appendChild(py2);
        comp2.appendChild(prompt);
        canvas.appendChild(comp2);

        // Register compositions in state
        uiState.setCanvasCompositions([
            { id: 'melded-ax1-py1', edges: [{ from: 'ax1', to: 'py1', direction: 'right', position: 0 }], x: 100, y: 100 },
            { id: 'melded-py2-prompt1', edges: [{ from: 'py2', to: 'prompt1', direction: 'right', position: 0 }], x: 500, y: 100 }
        ]);

        // comp1 to the left, comp2 to the right, gap < threshold
        mockRect(comp1, { left: 100, top: 100, width: 300, height: 150 });
        mockRect(comp2, { left: 420, top: 100, width: 300, height: 150 });

        const result = findMeldTarget(comp1);

        // py1 (leaf of comp1) → py2 (root of comp2): py→py is 'right'
        expect(result.target).toBe(comp2);
        expect(result.direction).toBe('right');
        expect(result.distance).toBeLessThan(PROXIMITY_THRESHOLD);

        document.body.innerHTML = '';
        uiState.setCanvasCompositions([]);
    });

    test('dragging ax|py composition near prompt|py2 composition', () => {
        uiState.setCanvasCompositions([]);
        const canvas = document.createElement('div');
        document.body.appendChild(canvas);

        // Composition 1: ax|py (being dragged)
        const ax = document.createElement('div');
        ax.className = 'canvas-ax-glyph';
        ax.setAttribute('data-glyph-id', 'ax1');
        const py = document.createElement('div');
        py.className = 'canvas-py-glyph';
        py.setAttribute('data-glyph-id', 'py1');

        const comp1 = document.createElement('div');
        comp1.className = 'melded-composition';
        comp1.setAttribute('data-glyph-id', 'melded-ax1-py1');
        comp1.appendChild(ax);
        comp1.appendChild(py);
        canvas.appendChild(comp1);

        // Composition 2: prompt|py2 (stationary)
        const prompt = document.createElement('div');
        prompt.className = 'canvas-prompt-glyph';
        prompt.setAttribute('data-glyph-id', 'prompt1');
        const py2 = document.createElement('div');
        py2.className = 'canvas-py-glyph';
        py2.setAttribute('data-glyph-id', 'py2');

        const comp2 = document.createElement('div');
        comp2.className = 'melded-composition';
        comp2.setAttribute('data-glyph-id', 'melded-prompt1-py2');
        comp2.appendChild(prompt);
        comp2.appendChild(py2);
        canvas.appendChild(comp2);

        // Register compositions in state
        uiState.setCanvasCompositions([
            { id: 'melded-ax1-py1', edges: [{ from: 'ax1', to: 'py1', direction: 'right', position: 0 }], x: 100, y: 100 },
            { id: 'melded-prompt1-py2', edges: [{ from: 'prompt1', to: 'py2', direction: 'right', position: 0 }], x: 500, y: 100 }
        ]);

        // comp1 is to the left, comp2 to the right, gap < threshold
        mockRect(comp1, { left: 100, top: 100, width: 300, height: 150 });
        mockRect(comp2, { left: 420, top: 100, width: 300, height: 150 });

        const result = findMeldTarget(comp1);

        expect(result.target).toBe(comp2);
        expect(result.direction).toBe('right');
        expect(result.distance).toBeLessThan(PROXIMITY_THRESHOLD);

        document.body.innerHTML = '';
        uiState.setCanvasCompositions([]);
    });
});
