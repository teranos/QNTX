/**
 * Critical minimal test for glyph melding behavior
 *
 * Validates the core axiom: proximity-based melding preserves element identity
 */

import { describe, test, expect } from 'bun:test';
import { Window } from 'happy-dom';
import { performMeld, isMeldedComposition, MELD_THRESHOLD } from './meld-system';
import type { Glyph } from './glyph';

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
});
