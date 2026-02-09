/**
 * Tests for meld composition — create, extend, reconstruct, unmeld
 *
 * Validates the core axiom: proximity-based melding preserves element identity
 *
 * Personas:
 * - Tim: Happy path user, normal workflows
 * - Spike: Tries to break things, edge cases
 */

import { describe, test, expect } from 'bun:test';
import { Window } from 'happy-dom';
import { performMeld, unmeldComposition, isMeldedComposition, reconstructMeld, extendComposition } from './meld-composition';
import { MELD_THRESHOLD } from './meld-detect';
import type { Glyph } from '../glyph';
import { uiState } from '../../../state/ui';

// Setup happy-dom
const window = new Window();
const document = window.document;
globalThis.document = document as any;
globalThis.window = window as any;

describe('Meld System - Critical Behavior', () => {
    test('compatible glyphs meld into composition preserving element identity', () => {
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

        const axGlyph: Glyph = { id: 'ax-test', title: 'AX', renderContent: () => axElement };
        const promptGlyph: Glyph = { id: 'prompt-test', title: 'Prompt', renderContent: () => promptElement };

        const originalAxElement = axElement;
        const originalPromptElement = promptElement;

        const composition = performMeld(axElement, promptElement, axGlyph, promptGlyph);

        expect(isMeldedComposition(composition)).toBe(true);
        expect(composition.parentElement).toBe(canvas);
        expect(composition.contains(originalAxElement)).toBe(true);
        expect(composition.contains(originalPromptElement)).toBe(true);
        expect(composition.children.length).toBe(2);
        expect(composition.children[0]).toBe(originalAxElement);
        expect(composition.children[1]).toBe(originalPromptElement);
        expect(originalAxElement.parentElement).toBe(composition);
        expect(originalPromptElement.parentElement).toBe(composition);
        expect(composition.style.left).toBe('100px');
        expect(composition.style.top).toBe('100px');

        document.body.innerHTML = '';
    });

    test('unmeld restores elements to canvas preserving identity', () => {
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

        const axGlyph: Glyph = { id: 'ax-test', title: 'AX', renderContent: () => axElement };
        const promptGlyph: Glyph = { id: 'prompt-test', title: 'Prompt', renderContent: () => promptElement };

        const composition = performMeld(axElement, promptElement, axGlyph, promptGlyph);
        const result = unmeldComposition(composition);

        expect(result).not.toBe(null);
        expect(result?.glyphElements).toHaveLength(2);
        expect(result?.glyphElements[0]).toBe(axElement);
        expect(result?.glyphElements[1]).toBe(promptElement);
        expect(axElement.parentElement).toBe(canvas);
        expect(promptElement.parentElement).toBe(canvas);
        expect(axElement.style.position).toBe('absolute');
        expect(composition.parentElement).toBe(null);

        document.body.innerHTML = '';
    });
});

describe('Meld Composition - Tim (Happy Path)', () => {
    test('Tim sees melded composition contains both glyphs', () => {
        const canvas = document.createElement('div');
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

        const axGlyph: Glyph = { id: 'ax-test', title: 'AX', renderContent: () => axElement };
        const promptGlyph: Glyph = { id: 'prompt-test', title: 'Prompt', renderContent: () => promptElement };

        const composition = performMeld(axElement, promptElement, axGlyph, promptGlyph);

        expect(isMeldedComposition(composition)).toBe(true);
        expect(composition.contains(axElement)).toBe(true);
        expect(composition.contains(promptElement)).toBe(true);

        document.body.innerHTML = '';
    });
});

describe('Meld Composition - Spike (Edge Cases)', () => {
    test('Spike tries to unmeld non-composition element', () => {
        const canvas = document.createElement('div');
        document.body.appendChild(canvas);

        const regularGlyph = document.createElement('div');
        regularGlyph.className = 'canvas-ax-glyph';
        canvas.appendChild(regularGlyph);

        const result = unmeldComposition(regularGlyph);
        expect(result).toBe(null);

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
        expect(composition.getAttribute('data-glyph-id')).toBe('melded-py1-result1');

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

        expect(composition.style.flexDirection).toBe('row');
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

        const composition = performMeld(ax, py, axGlyph, pyGlyph, 'right');

        const prompt = document.createElement('div');
        prompt.className = 'canvas-prompt-glyph';
        prompt.setAttribute('data-glyph-id', 'prompt1');
        prompt.style.position = 'absolute';
        prompt.style.left = '400px';
        prompt.style.top = '100px';
        canvas.appendChild(prompt);

        extendComposition(composition, prompt, 'prompt1', 'py1', 'right', 'to');

        expect(composition.children.length).toBe(3);
        expect(composition.children[0]).toBe(ax);
        expect(composition.children[1]).toBe(py);
        expect(composition.children[2]).toBe(prompt);
        expect(prompt.parentElement).toBe(composition);
        expect(prompt.style.position).toBe('relative');
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

        const composition = performMeld(ax, py, axGlyph, pyGlyph, 'right');

        const prompt = document.createElement('div');
        prompt.className = 'canvas-prompt-glyph';
        prompt.setAttribute('data-glyph-id', 'prompt1');
        canvas.appendChild(prompt);
        extendComposition(composition, prompt, 'prompt1', 'py1', 'right', 'to');

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

        const composition = performMeld(py, prompt, pyGlyph, promptGlyph, 'right');

        const ax = document.createElement('div');
        ax.className = 'canvas-ax-glyph';
        ax.setAttribute('data-glyph-id', 'ax1');
        canvas.appendChild(ax);
        extendComposition(composition, ax, 'ax1', 'py1', 'right', 'from');

        expect(composition.children.length).toBe(3);
        expect(composition.children[0]).toBe(ax);
        expect(composition.children[1]).toBe(py);
        expect(composition.children[2]).toBe(prompt);
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

        const composition = performMeld(ax, py, axGlyph, pyGlyph, 'right');
        expect(composition.style.flexDirection).toBe('row');

        const result = document.createElement('div');
        result.className = 'canvas-result-glyph';
        result.setAttribute('data-glyph-id', 'result1');
        result.style.position = 'absolute';
        canvas.appendChild(result);

        extendComposition(composition, result, 'result1', 'py1', 'bottom', 'to');

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

        const result1 = document.createElement('div');
        result1.className = 'canvas-result-glyph';
        result1.setAttribute('data-glyph-id', 'r1');
        canvas.appendChild(result1);
        extendComposition(composition, result1, 'r1', 'py1', 'bottom', 'to');

        const result2 = document.createElement('div');
        result2.className = 'canvas-result-glyph';
        result2.setAttribute('data-glyph-id', 'r2');
        canvas.appendChild(result2);
        extendComposition(composition, result2, 'r2', 'py1', 'bottom', 'to');

        expect(composition.children.length).toBe(2);

        const subContainer = composition.children[1] as HTMLElement;
        expect(subContainer.children.length).toBe(3);
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

        const compositions = uiState.getCanvasCompositions();
        expect(compositions.find(c => c.id === oldId)).toBeUndefined();

        const newComp = compositions.find(c => c.id === 'melded-py1-prompt1');
        expect(newComp).toBeDefined();
        expect(newComp!.edges.length).toBe(2);
        expect(newComp!.edges[0]).toEqual({ from: 'ax1', to: 'py1', direction: 'right', position: 0 });
        expect(newComp!.edges[1]).toEqual({ from: 'py1', to: 'prompt1', direction: 'right', position: 1 });

        clearState();
    });
});
