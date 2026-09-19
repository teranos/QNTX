/**
 * Tests for meld composition — create, extend, reconstruct, unmeld
 *
 * Validates the core axiom: proximity-based melding preserves element identity
 * Layout: Absolute positioning from DAG grid positions
 *
 * Personas:
 * - Tim: Happy path user, normal workflows
 * - Spike: Tries to break things, edge cases
 */

import { describe, test, expect, beforeEach } from 'bun:test';
import { performMeld, unmeldComposition, isMeldedComposition, reconstructMeld, extendComposition, detachElement, MELD_THRESHOLD, configureElements } from '@teranos/elements';
import type { Element } from '@teranos/elements';
import { uiState } from '../../../state/ui';
import { addComposition, removeComposition, findCompositionByElement } from '../../../state/compositions';

// Wire CanvasHost so package meld code can access composition state
beforeEach(() => {
    configureElements({
        canvasHost: {
            saveCanvasElement: (item) => uiState.addCanvasElement(item),
            getCanvasElements: () => uiState.getCanvasElements(),
            getTransform: () => ({ panX: 0, panY: 0, scale: 1 }),
            getSelectedElementIds: () => [],
            isElementSelected: () => false,
            saveComposition: (composition) => addComposition(composition),
            removeComposition: (id) => removeComposition(id),
            findCompositionByElement: (elementId) => findCompositionByElement(elementId),
            flushSync() {},
        },
    });
});

/** Get direct element children of a composition (no column wrappers with absolute positioning) */
function getElementChildren(composition: HTMLElement): HTMLElement[] {
    return Array.from(composition.querySelectorAll('[data-element-id]')) as HTMLElement[];
}

describe('Meld System - Critical Behavior', () => {
    test('compatible elements meld into composition preserving element identity', () => {
        const canvas = document.createElement('div');
        canvas.className = 'canvas';
        document.body.appendChild(canvas);

        const axElement = document.createElement('div');
        axElement.className = 'canvas-ax-element';
        axElement.setAttribute('data-element-id', 'ax-test');
        axElement.style.position = 'absolute';
        axElement.style.left = '100px';
        axElement.style.top = '100px';
        canvas.appendChild(axElement);

        const promptElement = document.createElement('div');
        promptElement.className = 'canvas-prompt-element';
        promptElement.setAttribute('data-element-id', 'prompt-test');
        promptElement.style.position = 'absolute';
        promptElement.style.left = `${100 + MELD_THRESHOLD - 5}px`;
        promptElement.style.top = '100px';
        canvas.appendChild(promptElement);

        const axItem: Element = { id: 'ax-test', title: 'AX', renderContent: () => axElement };
        const promptItem: Element = { id: 'prompt-test', title: 'Prompt', renderContent: () => promptElement };

        const originalAxElement = axElement;
        const originalPromptElement = promptElement;

        const composition = performMeld(axElement, promptElement, axItem, promptItem);

        expect(isMeldedComposition(composition)).toBe(true);
        expect(composition.parentElement).toBe(canvas);
        expect(composition.contains(originalAxElement)).toBe(true);
        expect(composition.contains(originalPromptElement)).toBe(true);
        expect(composition.contains(originalAxElement)).toBe(true);
        expect(composition.contains(originalPromptElement)).toBe(true);
        expect(composition.style.left).toBe('100px');
        expect(composition.style.top).toBe('100px');

        // Both elements are direct children (absolute positioning, no column wrappers)
        const children = getElementChildren(composition);
        expect(children).toContain(originalAxElement);
        expect(children).toContain(originalPromptElement);
        expect(children.length).toBe(2);

        document.body.innerHTML = '';
    });

    test('unmeld restores elements to canvas preserving identity', () => {
        const canvas = document.createElement('div');
        canvas.className = 'canvas';
        document.body.appendChild(canvas);

        const axElement = document.createElement('div');
        axElement.className = 'canvas-ax-element';
        axElement.setAttribute('data-element-id', 'ax-test');
        axElement.style.position = 'absolute';
        axElement.style.left = '100px';
        axElement.style.top = '100px';
        canvas.appendChild(axElement);

        const promptElement = document.createElement('div');
        promptElement.className = 'canvas-prompt-element';
        promptElement.setAttribute('data-element-id', 'prompt-test');
        promptElement.style.position = 'absolute';
        promptElement.style.left = `${100 + MELD_THRESHOLD - 5}px`;
        promptElement.style.top = '100px';
        canvas.appendChild(promptElement);

        const axItem: Element = { id: 'ax-test', title: 'AX', renderContent: () => axElement };
        const promptItem: Element = { id: 'prompt-test', title: 'Prompt', renderContent: () => promptElement };

        const composition = performMeld(axElement, promptElement, axItem, promptItem);
        const result = unmeldComposition(composition);

        expect(result).not.toBe(null);
        expect(result?.members).toHaveLength(2);
        expect(result?.members[0]).toBe(axElement);
        expect(result?.members[1]).toBe(promptElement);
        expect(axElement.parentElement).toBe(canvas);
        expect(promptElement.parentElement).toBe(canvas);
        expect(axElement.style.position).toBe('absolute');
        expect(axElement.style.gridRow).toBe('');
        expect(axElement.style.gridColumn).toBe('');
        expect(composition.parentElement).toBe(null);

        document.body.innerHTML = '';
    });
});

describe('Meld Composition - Tim (Happy Path)', () => {
    test('Tim sees melded composition contains both elements', () => {
        const canvas = document.createElement('div');
        document.body.appendChild(canvas);

        const axElement = document.createElement('div');
        axElement.className = 'canvas-ax-element';
        axElement.setAttribute('data-element-id', 'ax-test');
        axElement.style.position = 'absolute';
        axElement.style.left = '100px';
        axElement.style.top = '100px';
        canvas.appendChild(axElement);

        const promptElement = document.createElement('div');
        promptElement.className = 'canvas-prompt-element';
        promptElement.setAttribute('data-element-id', 'prompt-test');
        promptElement.style.position = 'absolute';
        promptElement.style.left = `${100 + MELD_THRESHOLD - 5}px`;
        promptElement.style.top = '100px';
        canvas.appendChild(promptElement);

        const axItem: Element = { id: 'ax-test', title: 'AX', renderContent: () => axElement };
        const promptItem: Element = { id: 'prompt-test', title: 'Prompt', renderContent: () => promptElement };

        const composition = performMeld(axElement, promptElement, axItem, promptItem);

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

        const regularElement = document.createElement('div');
        regularElement.className = 'canvas-ax-element';
        canvas.appendChild(regularElement);

        const result = unmeldComposition(regularElement);
        expect(result).toBe(null);

        document.body.innerHTML = '';
    });
});

describe('Directional Melding', () => {
    test('bottom meld: py + result uses grid layout', () => {
        const canvas = document.createElement('div');
        document.body.appendChild(canvas);

        const pyElement = document.createElement('div');
        pyElement.className = 'canvas-py-element';
        pyElement.setAttribute('data-element-id', 'py1');
        pyElement.style.position = 'absolute';
        pyElement.style.left = '100px';
        pyElement.style.top = '100px';
        canvas.appendChild(pyElement);

        const resultElement = document.createElement('div');
        resultElement.className = 'canvas-result-element';
        resultElement.setAttribute('data-element-id', 'result1');
        resultElement.style.position = 'absolute';
        resultElement.style.left = '100px';
        resultElement.style.top = '200px';
        canvas.appendChild(resultElement);

        const pyItem: Element = { id: 'py1', title: 'Py', renderContent: () => pyElement };
        const resultItem: Element = { id: 'result1', title: 'Result', renderContent: () => resultElement };

        const composition = performMeld(pyElement, resultElement, pyItem, resultItem, 'bottom');

        expect(composition.contains(pyElement)).toBe(true);
        expect(composition.contains(resultElement)).toBe(true);
        const children = getElementChildren(composition);
        expect(children).toContain(pyElement);
        expect(children).toContain(resultElement);
        expect(children.length).toBe(2);

        document.body.innerHTML = '';
    });

    test('bottom meld: note above prompt uses column layout', () => {
        const canvas = document.createElement('div');
        document.body.appendChild(canvas);

        const noteElement = document.createElement('div');
        noteElement.className = 'canvas-note-element';
        noteElement.setAttribute('data-element-id', 'note1');
        noteElement.style.position = 'absolute';
        noteElement.style.left = '100px';
        noteElement.style.top = '100px';
        canvas.appendChild(noteElement);

        const promptElement = document.createElement('div');
        promptElement.className = 'canvas-prompt-element';
        promptElement.setAttribute('data-element-id', 'prompt1');
        promptElement.style.position = 'absolute';
        promptElement.style.left = '100px';
        promptElement.style.top = '200px';
        canvas.appendChild(promptElement);

        const noteItem: Element = { id: 'note1', title: 'Note', renderContent: () => noteElement };
        const promptItem: Element = { id: 'prompt1', title: 'Prompt', renderContent: () => promptElement };

        const composition = performMeld(noteElement, promptElement, noteItem, promptItem, 'bottom');

        expect(composition.contains(noteElement)).toBe(true);
        expect(composition.contains(promptElement)).toBe(true);
        const children = getElementChildren(composition);
        expect(children).toContain(noteElement);
        expect(children).toContain(promptElement);
        expect(children.length).toBe(2);

        document.body.innerHTML = '';
    });

    test('right meld: ax + py uses column layout', () => {
        const canvas = document.createElement('div');
        document.body.appendChild(canvas);

        const axElement = document.createElement('div');
        axElement.className = 'canvas-ax-element';
        axElement.setAttribute('data-element-id', 'ax1');
        axElement.style.position = 'absolute';
        axElement.style.left = '100px';
        axElement.style.top = '100px';
        canvas.appendChild(axElement);

        const pyElement = document.createElement('div');
        pyElement.className = 'canvas-py-element';
        pyElement.setAttribute('data-element-id', 'py1');
        pyElement.style.position = 'absolute';
        pyElement.style.left = '200px';
        pyElement.style.top = '100px';
        canvas.appendChild(pyElement);

        const axItem: Element = { id: 'ax1', title: 'AX', renderContent: () => axElement };
        const pyItem: Element = { id: 'py1', title: 'Py', renderContent: () => pyElement };

        const composition = performMeld(axElement, pyElement, axItem, pyItem, 'right');
        const children = getElementChildren(composition);
        expect(children).toContain(axElement);
        expect(children).toContain(pyElement);
        expect(children.length).toBe(2);

        document.body.innerHTML = '';
    });

    test('edge stores correct direction', () => {
        const canvas = document.createElement('div');
        document.body.appendChild(canvas);

        const pyElement = document.createElement('div');
        pyElement.className = 'canvas-py-element';
        pyElement.setAttribute('data-element-id', 'py1');
        pyElement.style.position = 'absolute';
        pyElement.style.left = '100px';
        pyElement.style.top = '100px';
        canvas.appendChild(pyElement);

        const resultElement = document.createElement('div');
        resultElement.className = 'canvas-result-element';
        resultElement.setAttribute('data-element-id', 'result1');
        resultElement.style.position = 'absolute';
        resultElement.style.left = '100px';
        resultElement.style.top = '200px';
        canvas.appendChild(resultElement);

        const pyItem: Element = { id: 'py1', title: 'Py', renderContent: () => pyElement };
        const resultItem: Element = { id: 'result1', title: 'Result', renderContent: () => resultElement };

        const composition = performMeld(pyElement, resultElement, pyItem, resultItem, 'bottom');
        expect(composition.getAttribute('data-element-id')).toBe('melded-py1-result1');

        document.body.innerHTML = '';
    });
});

describe('Direction-aware reconstructMeld', () => {
    test('reconstructs horizontal composition from right edges', () => {
        const canvas = document.createElement('div');
        document.body.appendChild(canvas);

        const ax = document.createElement('div');
        ax.setAttribute('data-element-id', 'ax1');
        canvas.appendChild(ax);

        const py = document.createElement('div');
        py.setAttribute('data-element-id', 'py1');
        canvas.appendChild(py);

        const edges = [{ from: 'ax1', to: 'py1', direction: 'right', position: 0 }];
        const composition = reconstructMeld([ax, py], edges, 'comp1', 50, 75);

        expect(composition.style.left).toBe('50px');
        expect(composition.style.top).toBe('75px');
        const children = getElementChildren(composition);
        expect(children).toContain(ax);
        expect(children).toContain(py);
        expect(children.length).toBe(2);

        document.body.innerHTML = '';
    });

    test('reconstructs vertical composition from bottom edges', () => {
        const canvas = document.createElement('div');
        document.body.appendChild(canvas);

        const py = document.createElement('div');
        py.setAttribute('data-element-id', 'py1');
        canvas.appendChild(py);

        const result = document.createElement('div');
        result.setAttribute('data-element-id', 'result1');
        canvas.appendChild(result);

        const edges = [{ from: 'py1', to: 'result1', direction: 'bottom', position: 0 }];
        const composition = reconstructMeld([py, result], edges, 'comp2', 100, 100);

        expect(composition.contains(py)).toBe(true);
        expect(composition.contains(result)).toBe(true);
        const children = getElementChildren(composition);
        expect(children).toContain(py);
        expect(children).toContain(result);
        expect(children.length).toBe(2);

        document.body.innerHTML = '';
    });

    test('mixed right+bottom edges — correct structure', () => {
        const canvas = document.createElement('div');
        document.body.appendChild(canvas);

        const ax = document.createElement('div');
        ax.setAttribute('data-element-id', 'ax1');
        canvas.appendChild(ax);

        const py = document.createElement('div');
        py.setAttribute('data-element-id', 'py1');
        canvas.appendChild(py);

        const result = document.createElement('div');
        result.setAttribute('data-element-id', 'result1');
        canvas.appendChild(result);

        const edges = [
            { from: 'ax1', to: 'py1', direction: 'right', position: 0 },
            { from: 'py1', to: 'result1', direction: 'bottom', position: 1 }
        ];
        const composition = reconstructMeld([ax, py, result], edges, 'comp3', 0, 0);

        // All 3 elements are direct children
        const children = getElementChildren(composition);
        expect(children).toContain(ax);
        expect(children).toContain(py);
        expect(children).toContain(result);
        expect(children.length).toBe(3);

        document.body.innerHTML = '';
    });

    test('reparents elements without cloning', () => {
        const canvas = document.createElement('div');
        document.body.appendChild(canvas);

        const py = document.createElement('div');
        py.setAttribute('data-element-id', 'py1');
        canvas.appendChild(py);

        const result = document.createElement('div');
        result.setAttribute('data-element-id', 'result1');
        canvas.appendChild(result);

        const originalPy = py;
        const originalResult = result;

        const edges = [{ from: 'py1', to: 'result1', direction: 'bottom', position: 0 }];
        const composition = reconstructMeld([py, result], edges, 'comp4', 0, 0);

        expect(composition.contains(originalPy)).toBe(true);
        expect(composition.contains(originalResult)).toBe(true);
        expect(originalPy.style.position).toBe('absolute');

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
        ax.className = 'canvas-ax-element';
        ax.setAttribute('data-element-id', 'ax1');
        ax.style.position = 'absolute';
        ax.style.left = '100px';
        ax.style.top = '100px';
        canvas.appendChild(ax);

        const py = document.createElement('div');
        py.className = 'canvas-py-element';
        py.setAttribute('data-element-id', 'py1');
        py.style.position = 'absolute';
        py.style.left = '200px';
        py.style.top = '100px';
        canvas.appendChild(py);

        const axItem: Element = { id: 'ax1', title: 'AX', renderContent: () => ax };
        const pyItem: Element = { id: 'py1', title: 'Py', renderContent: () => py };

        const composition = performMeld(ax, py, axItem, pyItem, 'right');

        const prompt = document.createElement('div');
        prompt.className = 'canvas-prompt-element';
        prompt.setAttribute('data-element-id', 'prompt1');
        prompt.style.position = 'absolute';
        prompt.style.left = '400px';
        prompt.style.top = '100px';
        canvas.appendChild(prompt);

        extendComposition(composition, prompt, 'prompt1', 'py1', 'right', 'to');

        expect(composition.contains(prompt)).toBe(true);
        expect(prompt.style.position).toBe('absolute');
        expect(composition.getAttribute('data-element-id')).toBe('melded-py1-prompt1');

        // All 3 elements are direct children
        const children = getElementChildren(composition);
        expect(children).toContain(ax);
        expect(children).toContain(py);
        expect(children).toContain(prompt);
        expect(children.length).toBe(3);

        clearState();
    });

    test('Tim extends ax|py|prompt into 4-element chain', () => {
        clearState();
        const canvas = document.createElement('div');
        document.body.appendChild(canvas);

        const ax = document.createElement('div');
        ax.className = 'canvas-ax-element';
        ax.setAttribute('data-element-id', 'ax1');
        ax.style.position = 'absolute';
        ax.style.left = '100px';
        ax.style.top = '100px';
        canvas.appendChild(ax);

        const py = document.createElement('div');
        py.className = 'canvas-py-element';
        py.setAttribute('data-element-id', 'py1');
        py.style.position = 'absolute';
        py.style.left = '200px';
        py.style.top = '100px';
        canvas.appendChild(py);

        const axItem: Element = { id: 'ax1', title: 'AX', renderContent: () => ax };
        const pyItem: Element = { id: 'py1', title: 'Py', renderContent: () => py };

        const composition = performMeld(ax, py, axItem, pyItem, 'right');

        const prompt = document.createElement('div');
        prompt.className = 'canvas-prompt-element';
        prompt.setAttribute('data-element-id', 'prompt1');
        canvas.appendChild(prompt);
        extendComposition(composition, prompt, 'prompt1', 'py1', 'right', 'to');

        const py2 = document.createElement('div');
        py2.className = 'canvas-py-element';
        py2.setAttribute('data-element-id', 'py2');
        canvas.appendChild(py2);
        extendComposition(composition, py2, 'py2', 'prompt1', 'right', 'to');

        // All 4 elements are direct children
        const children = getElementChildren(composition);
        expect(children).toContain(ax);
        expect(children).toContain(py);
        expect(children).toContain(prompt);
        expect(children).toContain(py2);
        expect(children.length).toBe(4);

        clearState();
    });

    test('Tim prepends an element before root', () => {
        clearState();
        const canvas = document.createElement('div');
        document.body.appendChild(canvas);

        const py = document.createElement('div');
        py.className = 'canvas-py-element';
        py.setAttribute('data-element-id', 'py1');
        py.style.position = 'absolute';
        py.style.left = '200px';
        py.style.top = '100px';
        canvas.appendChild(py);

        const prompt = document.createElement('div');
        prompt.className = 'canvas-prompt-element';
        prompt.setAttribute('data-element-id', 'prompt1');
        prompt.style.position = 'absolute';
        prompt.style.left = '300px';
        prompt.style.top = '100px';
        canvas.appendChild(prompt);

        const pyItem: Element = { id: 'py1', title: 'Py', renderContent: () => py };
        const promptItem: Element = { id: 'prompt1', title: 'Prompt', renderContent: () => prompt };

        const composition = performMeld(py, prompt, pyItem, promptItem, 'right');

        const ax = document.createElement('div');
        ax.className = 'canvas-ax-element';
        ax.setAttribute('data-element-id', 'ax1');
        canvas.appendChild(ax);
        extendComposition(composition, ax, 'ax1', 'py1', 'right', 'from');

        expect(composition.getAttribute('data-element-id')).toBe('melded-ax1-py1');

        // All 3 elements are direct children
        const children = getElementChildren(composition);
        expect(children).toContain(ax);
        expect(children).toContain(py);
        expect(children).toContain(prompt);
        expect(children.length).toBe(3);

        clearState();
    });

    test('Tim extends ax|py with result below py (cross-axis, flat grid)', () => {
        clearState();
        const canvas = document.createElement('div');
        document.body.appendChild(canvas);

        const ax = document.createElement('div');
        ax.className = 'canvas-ax-element';
        ax.setAttribute('data-element-id', 'ax1');
        ax.style.position = 'absolute';
        ax.style.left = '100px';
        ax.style.top = '100px';
        canvas.appendChild(ax);

        const py = document.createElement('div');
        py.className = 'canvas-py-element';
        py.setAttribute('data-element-id', 'py1');
        py.style.position = 'absolute';
        py.style.left = '200px';
        py.style.top = '100px';
        canvas.appendChild(py);

        const axItem: Element = { id: 'ax1', title: 'AX', renderContent: () => ax };
        const pyItem: Element = { id: 'py1', title: 'Py', renderContent: () => py };

        const composition = performMeld(ax, py, axItem, pyItem, 'right');

        const result = document.createElement('div');
        result.className = 'canvas-result-element';
        result.setAttribute('data-element-id', 'result1');
        result.style.position = 'absolute';
        canvas.appendChild(result);

        extendComposition(composition, result, 'result1', 'py1', 'bottom', 'to');

        // All 3 elements are direct children
        const children = getElementChildren(composition);
        expect(children).toContain(ax);
        expect(children).toContain(py);
        expect(children).toContain(result);
        expect(children.length).toBe(3);

        clearState();
    });

    test('Tim runs py twice in ax|py — second result is direct child with grid position', () => {
        clearState();
        const canvas = document.createElement('div');
        document.body.appendChild(canvas);

        const ax = document.createElement('div');
        ax.className = 'canvas-ax-element';
        ax.setAttribute('data-element-id', 'ax1');
        ax.style.position = 'absolute';
        ax.style.left = '100px';
        ax.style.top = '100px';
        canvas.appendChild(ax);

        const py = document.createElement('div');
        py.className = 'canvas-py-element';
        py.setAttribute('data-element-id', 'py1');
        ax.style.position = 'absolute';
        canvas.appendChild(py);

        const axItem: Element = { id: 'ax1', title: 'AX', renderContent: () => ax };
        const pyItem: Element = { id: 'py1', title: 'Py', renderContent: () => py };

        const composition = performMeld(ax, py, axItem, pyItem, 'right');

        const result1 = document.createElement('div');
        result1.className = 'canvas-result-element';
        result1.setAttribute('data-element-id', 'r1');
        canvas.appendChild(result1);
        extendComposition(composition, result1, 'r1', 'py1', 'bottom', 'to');

        const result2 = document.createElement('div');
        result2.className = 'canvas-result-element';
        result2.setAttribute('data-element-id', 'r2');
        canvas.appendChild(result2);
        extendComposition(composition, result2, 'r2', 'py1', 'bottom', 'to');

        // All 4 elements are direct children
        const children = getElementChildren(composition);
        expect(children).toContain(ax);
        expect(children).toContain(py);
        expect(children).toContain(result1);
        expect(children).toContain(result2);
        expect(children.length).toBe(4);

        clearState();
    });

    test('extendComposition updates storage correctly', () => {
        clearState();
        const canvas = document.createElement('div');
        document.body.appendChild(canvas);

        const ax = document.createElement('div');
        ax.className = 'canvas-ax-element';
        ax.setAttribute('data-element-id', 'ax1');
        ax.style.position = 'absolute';
        ax.style.left = '100px';
        ax.style.top = '100px';
        canvas.appendChild(ax);

        const py = document.createElement('div');
        py.className = 'canvas-py-element';
        py.setAttribute('data-element-id', 'py1');
        py.style.position = 'absolute';
        py.style.left = '200px';
        py.style.top = '100px';
        canvas.appendChild(py);

        const axItem: Element = { id: 'ax1', title: 'AX', renderContent: () => ax };
        const pyItem: Element = { id: 'py1', title: 'Py', renderContent: () => py };

        const composition = performMeld(ax, py, axItem, pyItem, 'right');
        const oldId = composition.getAttribute('data-element-id');

        const prompt = document.createElement('div');
        prompt.className = 'canvas-prompt-element';
        prompt.setAttribute('data-element-id', 'prompt1');
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

describe('Detach Element - Tim (Happy Path)', () => {
    function clearState() {
        uiState.setCanvasCompositions([]);
        document.body.innerHTML = '';
    }

    test('Tim detaches leaf from 3-element chain → remaining 2 stay melded', () => {
        clearState();
        const canvas = document.createElement('div');
        document.body.appendChild(canvas);

        const ax = document.createElement('div');
        ax.className = 'canvas-ax-element';
        ax.setAttribute('data-element-id', 'ax1');
        ax.style.position = 'absolute';
        ax.style.left = '100px';
        ax.style.top = '100px';
        canvas.appendChild(ax);

        const py = document.createElement('div');
        py.className = 'canvas-py-element';
        py.setAttribute('data-element-id', 'py1');
        py.style.position = 'absolute';
        ax.style.left = '200px';
        ax.style.top = '100px';
        canvas.appendChild(py);

        const prompt = document.createElement('div');
        prompt.className = 'canvas-prompt-element';
        prompt.setAttribute('data-element-id', 'prompt1');
        prompt.style.position = 'absolute';
        canvas.appendChild(prompt);

        const axItem: Element = { id: 'ax1', title: 'AX', renderContent: () => ax };
        const pyItem: Element = { id: 'py1', title: 'Py', renderContent: () => py };

        const composition = performMeld(ax, py, axItem, pyItem, 'right');
        extendComposition(composition, prompt, 'prompt1', 'py1', 'right', 'to');

        // Detach the leaf (prompt1)
        const result = detachElement('prompt1', composition);

        expect(result).not.toBe(null);
        expect(result!.detachedElement).toBe(prompt);
        expect(result!.remainingComposition).toBe(composition);
        expect(prompt.parentElement).toBe(canvas);
        expect(prompt.style.position).toBe('absolute');
        expect(composition.contains(ax)).toBe(true);
        expect(composition.contains(py)).toBe(true);
        expect(composition.contains(prompt)).toBe(false);

        // Storage: remaining composition has 1 edge
        const comps = uiState.getCanvasCompositions();
        expect(comps.length).toBe(1);
        expect(comps[0].edges.length).toBe(1);
        expect(comps[0].edges[0].from).toBe('ax1');
        expect(comps[0].edges[0].to).toBe('py1');

        clearState();
    });

    test('Tim detaches root from 3-element chain → remaining 2 stay melded', () => {
        clearState();
        const canvas = document.createElement('div');
        document.body.appendChild(canvas);

        const ax = document.createElement('div');
        ax.className = 'canvas-ax-element';
        ax.setAttribute('data-element-id', 'ax1');
        ax.style.position = 'absolute';
        ax.style.left = '100px';
        ax.style.top = '100px';
        canvas.appendChild(ax);

        const py = document.createElement('div');
        py.className = 'canvas-py-element';
        py.setAttribute('data-element-id', 'py1');
        py.style.position = 'absolute';
        canvas.appendChild(py);

        const prompt = document.createElement('div');
        prompt.className = 'canvas-prompt-element';
        prompt.setAttribute('data-element-id', 'prompt1');
        prompt.style.position = 'absolute';
        canvas.appendChild(prompt);

        const axItem: Element = { id: 'ax1', title: 'AX', renderContent: () => ax };
        const pyItem: Element = { id: 'py1', title: 'Py', renderContent: () => py };

        const composition = performMeld(ax, py, axItem, pyItem, 'right');
        extendComposition(composition, prompt, 'prompt1', 'py1', 'right', 'to');

        // Detach the root (ax1)
        const result = detachElement('ax1', composition);

        expect(result).not.toBe(null);
        expect(result!.detachedElement).toBe(ax);
        expect(result!.remainingComposition).toBe(composition);
        expect(ax.parentElement).toBe(canvas);
        expect(composition.contains(py)).toBe(true);
        expect(composition.contains(prompt)).toBe(true);

        // Storage: remaining composition has 1 edge (py1→prompt1)
        const comps = uiState.getCanvasCompositions();
        expect(comps.length).toBe(1);
        expect(comps[0].edges.length).toBe(1);
        expect(comps[0].edges[0].from).toBe('py1');
        expect(comps[0].edges[0].to).toBe('prompt1');

        clearState();
    });

    test('Tim detaches middle element → full unmeld fallback', () => {
        clearState();
        const canvas = document.createElement('div');
        document.body.appendChild(canvas);

        const ax = document.createElement('div');
        ax.className = 'canvas-ax-element';
        ax.setAttribute('data-element-id', 'ax1');
        ax.style.position = 'absolute';
        ax.style.left = '100px';
        ax.style.top = '100px';
        canvas.appendChild(ax);

        const py = document.createElement('div');
        py.className = 'canvas-py-element';
        py.setAttribute('data-element-id', 'py1');
        py.style.position = 'absolute';
        canvas.appendChild(py);

        const prompt = document.createElement('div');
        prompt.className = 'canvas-prompt-element';
        prompt.setAttribute('data-element-id', 'prompt1');
        prompt.style.position = 'absolute';
        canvas.appendChild(prompt);

        const axItem: Element = { id: 'ax1', title: 'AX', renderContent: () => ax };
        const pyItem: Element = { id: 'py1', title: 'Py', renderContent: () => py };

        const composition = performMeld(ax, py, axItem, pyItem, 'right');
        extendComposition(composition, prompt, 'prompt1', 'py1', 'right', 'to');

        // Detach the middle (py1) → disconnects graph → full unmeld
        const result = detachElement('py1', composition);

        expect(result).not.toBe(null);
        expect(result!.detachedElement).toBe(py);
        expect(result!.remainingComposition).toBe(null); // full unmeld
        expect(ax.parentElement).toBe(canvas);
        expect(py.parentElement).toBe(canvas);
        expect(prompt.parentElement).toBe(canvas);

        // Storage: no compositions remain
        expect(uiState.getCanvasCompositions().length).toBe(0);

        clearState();
    });

    test('Tim detaches from 2-element composition → full unmeld', () => {
        clearState();
        const canvas = document.createElement('div');
        document.body.appendChild(canvas);

        const ax = document.createElement('div');
        ax.className = 'canvas-ax-element';
        ax.setAttribute('data-element-id', 'ax1');
        ax.style.position = 'absolute';
        ax.style.left = '100px';
        ax.style.top = '100px';
        canvas.appendChild(ax);

        const py = document.createElement('div');
        py.className = 'canvas-py-element';
        py.setAttribute('data-element-id', 'py1');
        py.style.position = 'absolute';
        canvas.appendChild(py);

        const axItem: Element = { id: 'ax1', title: 'AX', renderContent: () => ax };
        const pyItem: Element = { id: 'py1', title: 'Py', renderContent: () => py };

        const composition = performMeld(ax, py, axItem, pyItem, 'right');
        const result = detachElement('py1', composition);

        expect(result).not.toBe(null);
        expect(result!.remainingComposition).toBe(null); // full unmeld
        expect(ax.parentElement).toBe(canvas);
        expect(py.parentElement).toBe(canvas);
        expect(uiState.getCanvasCompositions().length).toBe(0);

        clearState();
    });

    test('Tim detaches bottom leaf from cross-axis composition', () => {
        clearState();
        const canvas = document.createElement('div');
        document.body.appendChild(canvas);

        const ax = document.createElement('div');
        ax.className = 'canvas-ax-element';
        ax.setAttribute('data-element-id', 'ax1');
        ax.style.position = 'absolute';
        ax.style.left = '100px';
        ax.style.top = '100px';
        canvas.appendChild(ax);

        const py = document.createElement('div');
        py.className = 'canvas-py-element';
        py.setAttribute('data-element-id', 'py1');
        py.style.position = 'absolute';
        canvas.appendChild(py);

        const result = document.createElement('div');
        result.className = 'canvas-result-element';
        result.setAttribute('data-element-id', 'result1');
        result.style.position = 'absolute';
        canvas.appendChild(result);

        const axItem: Element = { id: 'ax1', title: 'AX', renderContent: () => ax };
        const pyItem: Element = { id: 'py1', title: 'Py', renderContent: () => py };

        const composition = performMeld(ax, py, axItem, pyItem, 'right');
        extendComposition(composition, result, 'result1', 'py1', 'bottom', 'to');

        // Detach bottom leaf (result1) — ax→py stays connected
        const detachResult = detachElement('result1', composition);

        expect(detachResult).not.toBe(null);
        expect(detachResult!.detachedElement).toBe(result);
        expect(detachResult!.remainingComposition).toBe(composition);
        expect(result.parentElement).toBe(canvas);
        expect(composition.contains(ax)).toBe(true);
        expect(composition.contains(py)).toBe(true);

        const comps = uiState.getCanvasCompositions();
        expect(comps.length).toBe(1);
        expect(comps[0].edges.length).toBe(1);
        expect(comps[0].edges[0].from).toBe('ax1');
        expect(comps[0].edges[0].to).toBe('py1');

        clearState();
    });

    test('Storage correctness after detach: old composition removed, new one created', () => {
        clearState();
        const canvas = document.createElement('div');
        document.body.appendChild(canvas);

        const ax = document.createElement('div');
        ax.className = 'canvas-ax-element';
        ax.setAttribute('data-element-id', 'ax1');
        ax.style.position = 'absolute';
        ax.style.left = '100px';
        ax.style.top = '100px';
        canvas.appendChild(ax);

        const py = document.createElement('div');
        py.className = 'canvas-py-element';
        py.setAttribute('data-element-id', 'py1');
        py.style.position = 'absolute';
        canvas.appendChild(py);

        const prompt = document.createElement('div');
        prompt.className = 'canvas-prompt-element';
        prompt.setAttribute('data-element-id', 'prompt1');
        prompt.style.position = 'absolute';
        canvas.appendChild(prompt);

        const axItem: Element = { id: 'ax1', title: 'AX', renderContent: () => ax };
        const pyItem: Element = { id: 'py1', title: 'Py', renderContent: () => py };

        const composition = performMeld(ax, py, axItem, pyItem, 'right');
        extendComposition(composition, prompt, 'prompt1', 'py1', 'right', 'to');

        const oldId = composition.getAttribute('data-element-id');
        detachElement('prompt1', composition);

        const comps = uiState.getCanvasCompositions();
        // Old composition ID should be gone
        expect(comps.find(c => c.id === oldId)).toBeUndefined();
        // New composition exists with correct data
        const newComp = comps[0];
        expect(newComp).toBeDefined();
        expect(newComp.edges.length).toBe(1);
        expect(newComp.x).toBe(100);
        expect(newComp.y).toBe(100);

        clearState();
    });
});
