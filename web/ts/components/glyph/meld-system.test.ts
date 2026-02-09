/**
 * Tests for glyph melding behavior
 *
 * Validates the core axiom: proximity-based melding preserves element identity
 *
 * Personas:
 * - Tim: Happy path user, normal workflows
 * - Spike: Tries to break things, edge cases
 * - Jenny: Power user, complex scenarios
 */

import { describe, test, expect } from 'bun:test';
import { Window } from 'happy-dom';
import { performMeld, unmeldComposition, isMeldedComposition, reconstructMeld, findMeldTarget, extendComposition, MELD_THRESHOLD, PROXIMITY_THRESHOLD } from './meld-system';
import type { Glyph } from './glyph';
import { uiState } from '../../state/ui';

// Setup happy-dom
const window = new Window();
const document = window.document;
globalThis.document = document as any;
globalThis.window = window as any;

describe('Meld System - Critical Behavior', () => {
    test('compatible glyphs meld into composition preserving element identity', () => {
        // Setup: Create canvas and two compatible glyphs (ax + prompt)
        const canvas = document.createElement('div');
        canvas.className = 'canvas';
        document.body.appendChild(canvas);

        const axElement = document.createElement('div');
        axElement.className = 'canvas-ax-glyph';
        axElement.style.position = 'absolute';
        axElement.style.left = '100px';
        axElement.style.top = '100px';
        canvas.appendChild(axElement);

        const promptElement = document.createElement('div');
        promptElement.className = 'canvas-prompt-glyph';
        promptElement.style.position = 'absolute';
        promptElement.style.left = `${100 + MELD_THRESHOLD - 5}px`; // Within meld threshold
        promptElement.style.top = '100px';
        canvas.appendChild(promptElement);

        const axGlyph: Glyph = {
            id: 'ax-test',
            title: 'AX',
            renderContent: () => axElement
        };

        const promptGlyph: Glyph = {
            id: 'prompt-test',
            title: 'Prompt',
            renderContent: () => promptElement
        };

        // Store references to verify identity preservation
        const originalAxElement = axElement;
        const originalPromptElement = promptElement;

        // Action: Perform meld
        const composition = performMeld(axElement, promptElement, axGlyph, promptGlyph);

        // Assert: Composition created and added to canvas
        expect(isMeldedComposition(composition)).toBe(true);
        expect(composition.parentElement).toBe(canvas);

        // Assert: Original elements are children of composition (NOT clones)
        expect(composition.contains(originalAxElement)).toBe(true);
        expect(composition.contains(originalPromptElement)).toBe(true);
        expect(composition.children.length).toBe(2);

        // Assert: Elements are the SAME objects (identity preserved)
        expect(composition.children[0]).toBe(originalAxElement);
        expect(composition.children[1]).toBe(originalPromptElement);

        // Assert: Elements no longer directly in canvas (reparented)
        expect(canvas.contains(originalAxElement)).toBe(true); // Still in canvas via composition
        expect(originalAxElement.parentElement).toBe(composition); // But parent is composition
        expect(originalPromptElement.parentElement).toBe(composition);

        // Assert: Composition positioned at ax location
        expect(composition.style.left).toBe('100px');
        expect(composition.style.top).toBe('100px');

        // Cleanup
        document.body.innerHTML = '';
    });

    test('unmeld restores elements to canvas preserving identity', () => {
        // Setup: Create canvas and melded composition
        const canvas = document.createElement('div');
        canvas.className = 'canvas';
        document.body.appendChild(canvas);

        const axElement = document.createElement('div');
        axElement.className = 'canvas-ax-glyph';
        axElement.setAttribute('data-glyph-id', 'ax-test');
        axElement.style.position = 'absolute';
        axElement.style.left = '100px';
        axElement.style.top = '100px';
        canvas.appendChild(axElement);

        const promptElement = document.createElement('div');
        promptElement.className = 'canvas-prompt-glyph';
        promptElement.setAttribute('data-glyph-id', 'prompt-test');
        promptElement.style.position = 'absolute';
        promptElement.style.left = `${100 + MELD_THRESHOLD - 5}px`;
        promptElement.style.top = '100px';
        canvas.appendChild(promptElement);

        const axGlyph: Glyph = {
            id: 'ax-test',
            title: 'AX',
            renderContent: () => axElement
        };

        const promptGlyph: Glyph = {
            id: 'prompt-test',
            title: 'Prompt',
            renderContent: () => promptElement
        };

        // Store references to verify identity preservation
        const originalAxElement = axElement;
        const originalPromptElement = promptElement;

        // Create meld first
        const composition = performMeld(axElement, promptElement, axGlyph, promptGlyph);
        expect(composition.parentElement).toBe(canvas);

        // Action: Unmeld the composition
        const result = unmeldComposition(composition);

        // Assert: Result contains original elements
        expect(result).not.toBe(null);
        expect(result?.glyphElements).toHaveLength(2);
        expect(result?.glyphElements[0]).toBe(originalAxElement);
        expect(result?.glyphElements[1]).toBe(originalPromptElement);

        // Assert: Elements restored to canvas
        expect(originalAxElement.parentElement).toBe(canvas);
        expect(originalPromptElement.parentElement).toBe(canvas);

        // Assert: Elements have absolute positioning
        expect(originalAxElement.style.position).toBe('absolute');
        expect(originalPromptElement.style.position).toBe('absolute');

        // Assert: Composition removed from DOM
        expect(composition.parentElement).toBe(null);

        // Cleanup
        document.body.innerHTML = '';
    });
});

describe('Meld Composition Drag - Tim (Happy Path)', () => {
    test('Tim sees melded composition contains both glyphs', () => {
        const canvas = document.createElement('div');
        canvas.className = 'canvas';
        document.body.appendChild(canvas);

        const axElement = document.createElement('div');
        axElement.className = 'canvas-ax-glyph';
        axElement.style.position = 'absolute';
        axElement.style.left = '100px';
        axElement.style.top = '100px';
        canvas.appendChild(axElement);

        const promptElement = document.createElement('div');
        promptElement.className = 'canvas-prompt-glyph';
        promptElement.style.position = 'absolute';
        promptElement.style.left = `${100 + MELD_THRESHOLD - 5}px`;
        promptElement.style.top = '100px';
        canvas.appendChild(promptElement);

        const axGlyph: Glyph = {
            id: 'ax-test',
            title: 'AX',
            renderContent: () => axElement
        };

        const promptGlyph: Glyph = {
            id: 'prompt-test',
            title: 'Prompt',
            renderContent: () => promptElement
        };

        // Tim performs meld
        const composition = performMeld(axElement, promptElement, axGlyph, promptGlyph);

        // Composition is identified as melded
        expect(isMeldedComposition(composition)).toBe(true);

        // Contains both child glyphs
        expect(composition.contains(axElement)).toBe(true);
        expect(composition.contains(promptElement)).toBe(true);

        // Cleanup
        document.body.innerHTML = '';
    });
});

describe('Meld Composition Drag - Spike (Edge Cases)', () => {
    test('Spike tries to unmeld non-composition element', () => {
        const canvas = document.createElement('div');
        canvas.className = 'canvas';
        document.body.appendChild(canvas);

        // Spike tries to unmeld a regular glyph (not a composition)
        const regularGlyph = document.createElement('div');
        regularGlyph.className = 'canvas-ax-glyph';
        canvas.appendChild(regularGlyph);

        const result = unmeldComposition(regularGlyph);

        // Unmeld fails gracefully
        expect(result).toBe(null);

        // Cleanup
        document.body.innerHTML = '';
    });
});

describe('Directional Melding', () => {
    test('bottom meld: py + result uses column layout', () => {
        const canvas = document.createElement('div');
        document.body.appendChild(canvas);

        const pyElement = document.createElement('div');
        pyElement.className = 'canvas-py-glyph';
        pyElement.style.position = 'absolute';
        pyElement.style.left = '100px';
        pyElement.style.top = '100px';
        canvas.appendChild(pyElement);

        const resultElement = document.createElement('div');
        resultElement.className = 'canvas-result-glyph';
        resultElement.style.position = 'absolute';
        resultElement.style.left = '100px';
        resultElement.style.top = '200px';
        canvas.appendChild(resultElement);

        const pyGlyph: Glyph = { id: 'py1', title: 'Py', renderContent: () => pyElement };
        const resultGlyph: Glyph = { id: 'result1', title: 'Result', renderContent: () => resultElement };

        const composition = performMeld(pyElement, resultElement, pyGlyph, resultGlyph, 'bottom');

        expect(composition.style.flexDirection).toBe('column');
        expect(composition.contains(pyElement)).toBe(true);
        expect(composition.contains(resultElement)).toBe(true);

        document.body.innerHTML = '';
    });

    test('bottom meld: note above prompt uses column layout', () => {
        const canvas = document.createElement('div');
        document.body.appendChild(canvas);

        const noteElement = document.createElement('div');
        noteElement.className = 'canvas-note-glyph';
        noteElement.style.position = 'absolute';
        noteElement.style.left = '100px';
        noteElement.style.top = '100px';
        canvas.appendChild(noteElement);

        const promptElement = document.createElement('div');
        promptElement.className = 'canvas-prompt-glyph';
        promptElement.style.position = 'absolute';
        promptElement.style.left = '100px';
        promptElement.style.top = '200px';
        canvas.appendChild(promptElement);

        const noteGlyph: Glyph = { id: 'note1', title: 'Note', renderContent: () => noteElement };
        const promptGlyph: Glyph = { id: 'prompt1', title: 'Prompt', renderContent: () => promptElement };

        const composition = performMeld(noteElement, promptElement, noteGlyph, promptGlyph, 'bottom');

        expect(composition.style.flexDirection).toBe('column');
        expect(composition.contains(noteElement)).toBe(true);
        expect(composition.contains(promptElement)).toBe(true);

        document.body.innerHTML = '';
    });

    test('right meld: ax + py uses row layout', () => {
        const canvas = document.createElement('div');
        document.body.appendChild(canvas);

        const axElement = document.createElement('div');
        axElement.className = 'canvas-ax-glyph';
        axElement.style.position = 'absolute';
        axElement.style.left = '100px';
        axElement.style.top = '100px';
        canvas.appendChild(axElement);

        const pyElement = document.createElement('div');
        pyElement.className = 'canvas-py-glyph';
        pyElement.style.position = 'absolute';
        pyElement.style.left = '200px';
        pyElement.style.top = '100px';
        canvas.appendChild(pyElement);

        const axGlyph: Glyph = { id: 'ax1', title: 'AX', renderContent: () => axElement };
        const pyGlyph: Glyph = { id: 'py1', title: 'Py', renderContent: () => pyElement };

        const composition = performMeld(axElement, pyElement, axGlyph, pyGlyph, 'right');

        expect(composition.style.flexDirection).toBe('row');

        document.body.innerHTML = '';
    });

    test('edge stores correct direction', () => {
        const canvas = document.createElement('div');
        document.body.appendChild(canvas);

        const pyElement = document.createElement('div');
        pyElement.className = 'canvas-py-glyph';
        pyElement.style.position = 'absolute';
        pyElement.style.left = '100px';
        pyElement.style.top = '100px';
        canvas.appendChild(pyElement);

        const resultElement = document.createElement('div');
        resultElement.className = 'canvas-result-glyph';
        resultElement.style.position = 'absolute';
        resultElement.style.left = '100px';
        resultElement.style.top = '200px';
        canvas.appendChild(resultElement);

        const pyGlyph: Glyph = { id: 'py1', title: 'Py', renderContent: () => pyElement };
        const resultGlyph: Glyph = { id: 'result1', title: 'Result', renderContent: () => resultElement };

        const composition = performMeld(pyElement, resultElement, pyGlyph, resultGlyph, 'bottom');

        // Composition container identifies via data-glyph-id only; child glyphs
        // are discoverable through edges (no binary initiator/target attributes)
        expect(composition.getAttribute('data-glyph-id')).toBe('melded-py1-result1');

        document.body.innerHTML = '';
    });
});

describe('Reverse Meld Detection - Tim (Happy Path)', () => {
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

describe('Direction-aware reconstructMeld', () => {
    test('reconstructs horizontal composition from right edges', () => {
        const canvas = document.createElement('div');
        document.body.appendChild(canvas);

        const ax = document.createElement('div');
        ax.setAttribute('data-glyph-id', 'ax1');
        canvas.appendChild(ax);

        const py = document.createElement('div');
        py.setAttribute('data-glyph-id', 'py1');
        canvas.appendChild(py);

        const edges = [{ from: 'ax1', to: 'py1', direction: 'right', position: 0 }];
        const composition = reconstructMeld([ax, py], edges, 'comp1', 50, 75);

        expect(composition.style.flexDirection).toBe('row');
        expect(composition.style.left).toBe('50px');
        expect(composition.style.top).toBe('75px');
        expect(composition.children.length).toBe(2);
        expect(composition.children[0]).toBe(ax);
        expect(composition.children[1]).toBe(py);

        document.body.innerHTML = '';
    });

    test('reconstructs vertical composition from bottom edges', () => {
        const canvas = document.createElement('div');
        document.body.appendChild(canvas);

        const py = document.createElement('div');
        py.setAttribute('data-glyph-id', 'py1');
        canvas.appendChild(py);

        const result = document.createElement('div');
        result.setAttribute('data-glyph-id', 'result1');
        canvas.appendChild(result);

        const edges = [{ from: 'py1', to: 'result1', direction: 'bottom', position: 0 }];
        const composition = reconstructMeld([py, result], edges, 'comp2', 100, 100);

        expect(composition.style.flexDirection).toBe('column');
        expect(composition.contains(py)).toBe(true);
        expect(composition.contains(result)).toBe(true);

        document.body.innerHTML = '';
    });

    test('mixed right+bottom edges use row layout with sub-container', () => {
        const canvas = document.createElement('div');
        document.body.appendChild(canvas);

        const ax = document.createElement('div');
        ax.setAttribute('data-glyph-id', 'ax1');
        canvas.appendChild(ax);

        const py = document.createElement('div');
        py.setAttribute('data-glyph-id', 'py1');
        canvas.appendChild(py);

        const result = document.createElement('div');
        result.setAttribute('data-glyph-id', 'result1');
        canvas.appendChild(result);

        const edges = [
            { from: 'ax1', to: 'py1', direction: 'right', position: 0 },
            { from: 'py1', to: 'result1', direction: 'bottom', position: 1 }
        ];
        const composition = reconstructMeld([ax, py, result], edges, 'comp3', 0, 0);

        // Mixed edges with right present → row layout
        expect(composition.style.flexDirection).toBe('row');

        // ax is direct child, py+result in sub-container
        expect(composition.children.length).toBe(2);
        expect(composition.children[0]).toBe(ax);

        const subContainer = composition.children[1] as HTMLElement;
        expect(subContainer.classList.contains('meld-sub-container')).toBe(true);
        expect(subContainer.style.flexDirection).toBe('column');
        expect(subContainer.children[0]).toBe(py);
        expect(subContainer.children[1]).toBe(result);

        document.body.innerHTML = '';
    });

    test('reparents elements without cloning', () => {
        const canvas = document.createElement('div');
        document.body.appendChild(canvas);

        const py = document.createElement('div');
        py.setAttribute('data-glyph-id', 'py1');
        canvas.appendChild(py);

        const result = document.createElement('div');
        result.setAttribute('data-glyph-id', 'result1');
        canvas.appendChild(result);

        const originalPy = py;
        const originalResult = result;

        const edges = [{ from: 'py1', to: 'result1', direction: 'bottom', position: 0 }];
        const composition = reconstructMeld([py, result], edges, 'comp4', 0, 0);

        expect(composition.children[0]).toBe(originalPy);
        expect(composition.children[1]).toBe(originalResult);
        expect(originalPy.style.position).toBe('relative');

        document.body.innerHTML = '';
    });
});

// TODO(#441): Phase 2-5 - Multi-glyph chain functionality tests
describe('Composition Extension - Tim (Happy Path)', () => {
    function clearState() {
        uiState.setCanvasCompositions([]);
        document.body.innerHTML = '';
    }

    test('Tim extends ax|py composition with prompt (append)', () => {
        clearState();
        const canvas = document.createElement('div');
        document.body.appendChild(canvas);

        const ax = document.createElement('div');
        ax.className = 'canvas-ax-glyph';
        ax.setAttribute('data-glyph-id', 'ax1');
        ax.style.position = 'absolute';
        ax.style.left = '100px';
        ax.style.top = '100px';
        canvas.appendChild(ax);

        const py = document.createElement('div');
        py.className = 'canvas-py-glyph';
        py.setAttribute('data-glyph-id', 'py1');
        py.style.position = 'absolute';
        py.style.left = '200px';
        py.style.top = '100px';
        canvas.appendChild(py);

        const axGlyph: Glyph = { id: 'ax1', title: 'AX', renderContent: () => ax };
        const pyGlyph: Glyph = { id: 'py1', title: 'Py', renderContent: () => py };

        // Create initial 2-glyph composition
        const composition = performMeld(ax, py, axGlyph, pyGlyph, 'right');

        // Standalone prompt to append
        const prompt = document.createElement('div');
        prompt.className = 'canvas-prompt-glyph';
        prompt.setAttribute('data-glyph-id', 'prompt1');
        prompt.style.position = 'absolute';
        prompt.style.left = '400px';
        prompt.style.top = '100px';
        canvas.appendChild(prompt);

        // Extend: append prompt after py (leaf)
        extendComposition(composition, prompt, 'prompt1', 'py1', 'right', 'to');

        // 3 children in correct order
        expect(composition.children.length).toBe(3);
        expect(composition.children[0]).toBe(ax);
        expect(composition.children[1]).toBe(py);
        expect(composition.children[2]).toBe(prompt);

        // Element identity preserved
        expect(prompt.parentElement).toBe(composition);
        expect(prompt.style.position).toBe('relative');

        // ID regenerated
        expect(composition.getAttribute('data-glyph-id')).toBe('melded-py1-prompt1');

        clearState();
    });

    test('Tim extends ax|py|prompt into 4-glyph chain', () => {
        clearState();
        const canvas = document.createElement('div');
        document.body.appendChild(canvas);

        const ax = document.createElement('div');
        ax.className = 'canvas-ax-glyph';
        ax.setAttribute('data-glyph-id', 'ax1');
        ax.style.position = 'absolute';
        ax.style.left = '100px';
        ax.style.top = '100px';
        canvas.appendChild(ax);

        const py = document.createElement('div');
        py.className = 'canvas-py-glyph';
        py.setAttribute('data-glyph-id', 'py1');
        py.style.position = 'absolute';
        py.style.left = '200px';
        py.style.top = '100px';
        canvas.appendChild(py);

        const axGlyph: Glyph = { id: 'ax1', title: 'AX', renderContent: () => ax };
        const pyGlyph: Glyph = { id: 'py1', title: 'Py', renderContent: () => py };

        // Create initial 2-glyph composition and extend to 3
        const composition = performMeld(ax, py, axGlyph, pyGlyph, 'right');

        const prompt = document.createElement('div');
        prompt.className = 'canvas-prompt-glyph';
        prompt.setAttribute('data-glyph-id', 'prompt1');
        canvas.appendChild(prompt);
        extendComposition(composition, prompt, 'prompt1', 'py1', 'right', 'to');

        // Now extend to 4: add another py after prompt
        const py2 = document.createElement('div');
        py2.className = 'canvas-py-glyph';
        py2.setAttribute('data-glyph-id', 'py2');
        canvas.appendChild(py2);
        extendComposition(composition, py2, 'py2', 'prompt1', 'right', 'to');

        expect(composition.children.length).toBe(4);
        expect(composition.children[3]).toBe(py2);

        clearState();
    });

    test('Tim prepends a glyph before root', () => {
        clearState();
        const canvas = document.createElement('div');
        document.body.appendChild(canvas);

        const py = document.createElement('div');
        py.className = 'canvas-py-glyph';
        py.setAttribute('data-glyph-id', 'py1');
        py.style.position = 'absolute';
        py.style.left = '200px';
        py.style.top = '100px';
        canvas.appendChild(py);

        const prompt = document.createElement('div');
        prompt.className = 'canvas-prompt-glyph';
        prompt.setAttribute('data-glyph-id', 'prompt1');
        prompt.style.position = 'absolute';
        prompt.style.left = '300px';
        prompt.style.top = '100px';
        canvas.appendChild(prompt);

        const pyGlyph: Glyph = { id: 'py1', title: 'Py', renderContent: () => py };
        const promptGlyph: Glyph = { id: 'prompt1', title: 'Prompt', renderContent: () => prompt };

        // Create py|prompt composition
        const composition = performMeld(py, prompt, pyGlyph, promptGlyph, 'right');

        // Prepend ax before py (root)
        const ax = document.createElement('div');
        ax.className = 'canvas-ax-glyph';
        ax.setAttribute('data-glyph-id', 'ax1');
        canvas.appendChild(ax);
        extendComposition(composition, ax, 'ax1', 'py1', 'right', 'from');

        // ax should be first child (prepended)
        expect(composition.children.length).toBe(3);
        expect(composition.children[0]).toBe(ax);
        expect(composition.children[1]).toBe(py);
        expect(composition.children[2]).toBe(prompt);

        // ID reflects the prepend edge
        expect(composition.getAttribute('data-glyph-id')).toBe('melded-ax1-py1');

        clearState();
    });

    test('Tim extends ax|py with result below py (cross-axis sub-container)', () => {
        clearState();
        const canvas = document.createElement('div');
        document.body.appendChild(canvas);

        const ax = document.createElement('div');
        ax.className = 'canvas-ax-glyph';
        ax.setAttribute('data-glyph-id', 'ax1');
        ax.style.position = 'absolute';
        ax.style.left = '100px';
        ax.style.top = '100px';
        canvas.appendChild(ax);

        const py = document.createElement('div');
        py.className = 'canvas-py-glyph';
        py.setAttribute('data-glyph-id', 'py1');
        py.style.position = 'absolute';
        py.style.left = '200px';
        py.style.top = '100px';
        canvas.appendChild(py);

        const axGlyph: Glyph = { id: 'ax1', title: 'AX', renderContent: () => ax };
        const pyGlyph: Glyph = { id: 'py1', title: 'Py', renderContent: () => py };

        // Create horizontal composition
        const composition = performMeld(ax, py, axGlyph, pyGlyph, 'right');
        expect(composition.style.flexDirection).toBe('row');

        // Add result below py (cross-axis)
        const result = document.createElement('div');
        result.className = 'canvas-result-glyph';
        result.setAttribute('data-glyph-id', 'result1');
        result.style.position = 'absolute';
        canvas.appendChild(result);

        extendComposition(composition, result, 'result1', 'py1', 'bottom', 'to');

        // ax is direct child, py+result wrapped in sub-container
        expect(composition.children.length).toBe(2);
        expect(composition.children[0]).toBe(ax);

        const subContainer = composition.children[1] as HTMLElement;
        expect(subContainer.classList.contains('meld-sub-container')).toBe(true);
        expect(subContainer.style.flexDirection).toBe('column');
        expect(subContainer.children[0]).toBe(py);
        expect(subContainer.children[1]).toBe(result);

        clearState();
    });

    test('Tim runs py twice in ax|py — second result joins existing sub-container', () => {
        clearState();
        const canvas = document.createElement('div');
        document.body.appendChild(canvas);

        const ax = document.createElement('div');
        ax.className = 'canvas-ax-glyph';
        ax.setAttribute('data-glyph-id', 'ax1');
        ax.style.position = 'absolute';
        ax.style.left = '100px';
        ax.style.top = '100px';
        canvas.appendChild(ax);

        const py = document.createElement('div');
        py.className = 'canvas-py-glyph';
        py.setAttribute('data-glyph-id', 'py1');
        ax.style.position = 'absolute';
        canvas.appendChild(py);

        const axGlyph: Glyph = { id: 'ax1', title: 'AX', renderContent: () => ax };
        const pyGlyph: Glyph = { id: 'py1', title: 'Py', renderContent: () => py };

        const composition = performMeld(ax, py, axGlyph, pyGlyph, 'right');

        // First result below py
        const result1 = document.createElement('div');
        result1.className = 'canvas-result-glyph';
        result1.setAttribute('data-glyph-id', 'r1');
        canvas.appendChild(result1);
        extendComposition(composition, result1, 'r1', 'py1', 'bottom', 'to');

        // Second result below py — should join existing sub-container
        const result2 = document.createElement('div');
        result2.className = 'canvas-result-glyph';
        result2.setAttribute('data-glyph-id', 'r2');
        canvas.appendChild(result2);
        extendComposition(composition, result2, 'r2', 'py1', 'bottom', 'to');

        // Still 2 direct children: ax + sub-container
        expect(composition.children.length).toBe(2);

        const subContainer = composition.children[1] as HTMLElement;
        expect(subContainer.children.length).toBe(3); // py, r1, r2
        expect(subContainer.children[0]).toBe(py);
        expect(subContainer.children[1]).toBe(result1);
        expect(subContainer.children[2]).toBe(result2);

        clearState();
    });

    test('extendComposition updates storage correctly', () => {
        clearState();
        const canvas = document.createElement('div');
        document.body.appendChild(canvas);

        const ax = document.createElement('div');
        ax.className = 'canvas-ax-glyph';
        ax.setAttribute('data-glyph-id', 'ax1');
        ax.style.position = 'absolute';
        ax.style.left = '100px';
        ax.style.top = '100px';
        canvas.appendChild(ax);

        const py = document.createElement('div');
        py.className = 'canvas-py-glyph';
        py.setAttribute('data-glyph-id', 'py1');
        py.style.position = 'absolute';
        py.style.left = '200px';
        py.style.top = '100px';
        canvas.appendChild(py);

        const axGlyph: Glyph = { id: 'ax1', title: 'AX', renderContent: () => ax };
        const pyGlyph: Glyph = { id: 'py1', title: 'Py', renderContent: () => py };

        const composition = performMeld(ax, py, axGlyph, pyGlyph, 'right');
        const oldId = composition.getAttribute('data-glyph-id');

        const prompt = document.createElement('div');
        prompt.className = 'canvas-prompt-glyph';
        prompt.setAttribute('data-glyph-id', 'prompt1');
        canvas.appendChild(prompt);
        extendComposition(composition, prompt, 'prompt1', 'py1', 'right', 'to');

        // Old composition removed from storage
        const compositions = uiState.getCanvasCompositions();
        expect(compositions.find(c => c.id === oldId)).toBeUndefined();

        // New composition exists with 2 edges
        const newComp = compositions.find(c => c.id === 'melded-py1-prompt1');
        expect(newComp).toBeDefined();
        expect(newComp!.edges.length).toBe(2);
        expect(newComp!.edges[0]).toEqual({ from: 'ax1', to: 'py1', direction: 'right', position: 0 });
        expect(newComp!.edges[1]).toEqual({ from: 'py1', to: 'prompt1', direction: 'right', position: 1 });

        clearState();
    });
});

describe('findMeldTarget detects composition targets', () => {
    function mockRect(el: HTMLElement, rect: { left: number; top: number; width: number; height: number }) {
        el.getBoundingClientRect = () => ({
            left: rect.left, top: rect.top,
            right: rect.left + rect.width, bottom: rect.top + rect.height,
            width: rect.width, height: rect.height,
            x: rect.left, y: rect.top,
            toJSON: () => ({})
        });
    }

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
