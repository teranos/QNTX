/**
 * Tests for auto-meld result helper
 *
 * Validates that result glyphs are automatically melded below their parent glyphs,
 * with composition-aware behavior (extend existing or create new).
 *
 * Persona: Tim (Happy Path)
 */

import { describe, test, expect } from 'bun:test';
import { Window } from 'happy-dom';
import { autoMeldResultBelow } from './auto-meld-result';
import { performMeld } from './meld-composition';
import type { Glyph } from '../glyph';
import { uiState } from '../../../state/ui';

// Setup happy-dom
const window = new Window();
const document = window.document;
globalThis.document = document as any;
globalThis.window = window as any;

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
        pyElement.className = 'canvas-py-glyph';
        pyElement.setAttribute('data-glyph-id', 'py-1');
        pyElement.style.position = 'absolute';
        pyElement.style.left = '100px';
        pyElement.style.top = '100px';
        canvas.appendChild(pyElement);

        // Create result glyph
        const resultElement = document.createElement('div');
        resultElement.className = 'canvas-result-glyph';
        resultElement.setAttribute('data-glyph-id', 'result-1');
        resultElement.style.position = 'absolute';
        resultElement.style.left = '100px';
        resultElement.style.top = '200px';
        canvas.appendChild(resultElement);

        // Auto-meld result below py
        autoMeldResultBelow(pyElement, 'py-1', 'py', 'Python', resultElement, 'result-1', 'PyGlyph');

        // Verify composition was created
        const composition = canvas.querySelector('.melded-composition');
        expect(composition).not.toBeNull();
        expect(composition?.getAttribute('data-glyph-id')).toBe('melded-py-1-result-1');

        // Verify both glyphs are in the composition
        expect(composition?.contains(pyElement)).toBe(true);
        expect(composition?.contains(resultElement)).toBe(true);

        // Verify composition layout is vertical (bottom direction)
        expect((composition as HTMLElement)?.style.flexDirection).toBe('column');

        clearState();
    });

    test('Tim: py glyph in composition extends with result below', () => {
        clearState();
        const canvas = document.createElement('div');
        canvas.className = 'canvas-workspace';
        document.body.appendChild(canvas);

        // Create ax and py glyphs in a composition
        const axElement = document.createElement('div');
        axElement.className = 'canvas-ax-glyph';
        axElement.setAttribute('data-glyph-id', 'ax-1');
        axElement.style.position = 'absolute';
        axElement.style.left = '100px';
        axElement.style.top = '100px';
        canvas.appendChild(axElement);

        const pyElement = document.createElement('div');
        pyElement.className = 'canvas-py-glyph';
        pyElement.setAttribute('data-glyph-id', 'py-1');
        pyElement.style.position = 'absolute';
        pyElement.style.left = '200px';
        pyElement.style.top = '100px';
        canvas.appendChild(pyElement);

        const axGlyph: Glyph = { id: 'ax-1', title: 'AX', renderContent: () => axElement };
        const pyGlyph: Glyph = { id: 'py-1', title: 'Python', renderContent: () => pyElement };

        // Create initial composition (ax → py)
        const composition = performMeld(axElement, pyElement, axGlyph, pyGlyph, 'right');
        const oldId = composition.getAttribute('data-glyph-id');

        // Create result glyph
        const resultElement = document.createElement('div');
        resultElement.className = 'canvas-result-glyph';
        resultElement.setAttribute('data-glyph-id', 'result-1');
        canvas.appendChild(resultElement);

        // Auto-meld result below py (which is inside composition)
        autoMeldResultBelow(pyElement, 'py-1', 'py', 'Python', resultElement, 'result-1', 'PyGlyph');

        // Verify composition was extended (ID changed)
        expect(composition.getAttribute('data-glyph-id')).toBe('melded-py-1-result-1');
        expect(composition.getAttribute('data-glyph-id')).not.toBe(oldId);

        // Verify result is in the composition
        expect(composition.contains(resultElement)).toBe(true);

        // Verify composition still contains original glyphs
        expect(composition.contains(axElement)).toBe(true);
        expect(composition.contains(pyElement)).toBe(true);

        // Verify sub-container was created for cross-axis (py → result is bottom, but ax → py is right)
        const subContainer = pyElement.parentElement;
        expect(subContainer?.classList.contains('meld-sub-container')).toBe(true);
        expect(subContainer?.contains(pyElement)).toBe(true);
        expect(subContainer?.contains(resultElement)).toBe(true);

        clearState();
    });
});
