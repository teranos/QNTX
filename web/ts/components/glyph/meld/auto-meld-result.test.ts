/**
 * Tests for auto-meld result helper
 *
 * Validates that result glyphs are automatically melded below their parent glyphs,
 * with composition-aware behavior (extend existing or create new).
 *
 * Persona: Tim (Happy Path)
 */

import { describe, test, expect, beforeEach } from 'bun:test';
import { autoMeldResultBelow } from './auto-meld-result';
import { performMeld, configureElements } from '@teranos/elements';
import type { Element } from '@teranos/elements';
import { uiState } from '../../../state/ui';
import { addComposition, removeComposition, findCompositionByGlyph } from '../../../state/compositions';

// Wire CanvasHost so package meld code can access composition state
beforeEach(() => {
    configureElements({
        canvasHost: {
            saveCanvasElement: (glyph) => uiState.addCanvasGlyph(glyph),
            getCanvasElements: () => uiState.getCanvasGlyphs(),
            getTransform: () => ({ panX: 0, panY: 0, scale: 1 }),
            getSelectedElementIds: () => [],
            isElementSelected: () => false,
            saveComposition: (composition) => addComposition(composition),
            removeComposition: (id) => removeComposition(id),
            findCompositionByElement: (glyphId) => findCompositionByGlyph(glyphId),
            flushSync() {},
        },
    });
});

describe('Auto-Meld Result Below - Tim (Happy Path)', () => {
    function clearState() {
        uiState.setCanvasCompositions([]);
        document.body.innerHTML = '';
    }

    test('Tim: standalone py glyph auto-melds with result below', () => {
        clearState();
        const canvas = document.createElement('div');
        canvas.className = 'canvas-workspace';
        document.body.appendChild(canvas);

        // Create py glyph
        const pyElement = document.createElement('div');
        pyElement.className = 'canvas-py-element';
        pyElement.setAttribute('data-element-id', 'py-1');
        pyElement.style.position = 'absolute';
        pyElement.style.left = '100px';
        pyElement.style.top = '100px';
        canvas.appendChild(pyElement);

        // Create result glyph
        const resultElement = document.createElement('div');
        resultElement.className = 'canvas-result-element';
        resultElement.setAttribute('data-element-id', 'result-1');
        resultElement.style.position = 'absolute';
        resultElement.style.left = '100px';
        resultElement.style.top = '200px';
        canvas.appendChild(resultElement);

        // Auto-meld result below py
        autoMeldResultBelow(pyElement, 'py-1', 'py', 'Python', resultElement, 'result-1', 'PyGlyph');

        // Verify composition was created
        const composition = canvas.querySelector('.melded-composition');
        expect(composition).not.toBeNull();
        expect(composition?.getAttribute('data-element-id')).toBe('melded-py-1-result-1');

        // Verify both glyphs are in the composition
        expect(composition?.contains(pyElement)).toBe(true);
        expect(composition?.contains(resultElement)).toBe(true);

        // Both elements are direct children (absolute positioning)
        const children = Array.from(composition!.querySelectorAll('[data-element-id]'));
        expect(children).toContain(pyElement);
        expect(children).toContain(resultElement);
        expect(children.length).toBe(2);

        clearState();
    });

    test('Tim: py glyph in composition extends with result below', () => {
        clearState();
        const canvas = document.createElement('div');
        canvas.className = 'canvas-workspace';
        document.body.appendChild(canvas);

        // Create ax and py glyphs in a composition
        const axElement = document.createElement('div');
        axElement.className = 'canvas-ax-element';
        axElement.setAttribute('data-element-id', 'ax-1');
        axElement.style.position = 'absolute';
        axElement.style.left = '100px';
        axElement.style.top = '100px';
        canvas.appendChild(axElement);

        const pyElement = document.createElement('div');
        pyElement.className = 'canvas-py-element';
        pyElement.setAttribute('data-element-id', 'py-1');
        pyElement.style.position = 'absolute';
        pyElement.style.left = '200px';
        pyElement.style.top = '100px';
        canvas.appendChild(pyElement);

        const axGlyph: Element = { id: 'ax-1', title: 'AX', renderContent: () => axElement };
        const pyGlyph: Element = { id: 'py-1', title: 'Python', renderContent: () => pyElement };

        // Create initial composition (ax → py)
        const composition = performMeld(axElement, pyElement, axGlyph, pyGlyph, 'right');
        const oldId = composition.getAttribute('data-element-id');

        // Create result glyph
        const resultElement = document.createElement('div');
        resultElement.className = 'canvas-result-element';
        resultElement.setAttribute('data-element-id', 'result-1');
        canvas.appendChild(resultElement);

        // Auto-meld result below py (which is inside composition)
        autoMeldResultBelow(pyElement, 'py-1', 'py', 'Python', resultElement, 'result-1', 'PyGlyph');

        // Verify composition was extended (ID changed)
        expect(composition.getAttribute('data-element-id')).toBe('melded-py-1-result-1');
        expect(composition.getAttribute('data-element-id')).not.toBe(oldId);

        // Verify result is in the composition
        expect(composition.contains(resultElement)).toBe(true);

        // Verify composition still contains original glyphs
        expect(composition.contains(axElement)).toBe(true);
        expect(composition.contains(pyElement)).toBe(true);

        // All 3 elements are direct children (absolute positioning)
        const children = Array.from(composition.querySelectorAll('[data-element-id]'));
        expect(children).toContain(axElement);
        expect(children).toContain(pyElement);
        expect(children).toContain(resultElement);
        expect(children.length).toBe(3);

        clearState();
    });
});
