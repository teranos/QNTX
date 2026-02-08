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
import { performMeld, unmeldComposition, isMeldedComposition, MELD_THRESHOLD } from './meld-system';
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
        expect(result?.glyphIds).toEqual(['ax-test', 'prompt-test']);

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

// TODO(#441): Phase 2-5 - Multi-glyph chain functionality tests
describe.skip('3-Glyph Chain Creation - Tim (Happy Path)', () => {
    test('Tim creates 3-glyph chain (ax|py|prompt) by dragging onto composition', () => {
        // Setup: Create canvas with ax-py composition and standalone prompt
        const canvas = document.createElement('div');
        canvas.className = 'canvas';
        document.body.appendChild(canvas);

        // Create ax-py composition
        const axElement = document.createElement('div');
        axElement.className = 'canvas-ax-glyph';
        axElement.setAttribute('data-glyph-id', 'ax1');

        const pyElement = document.createElement('div');
        pyElement.className = 'canvas-py-glyph';
        pyElement.setAttribute('data-glyph-id', 'py1');

        const axPyComposition = document.createElement('div');
        axPyComposition.className = 'melded-composition';
        axPyComposition.setAttribute('data-glyph-id', 'melded-ax1-py1');
        axPyComposition.style.position = 'absolute';
        axPyComposition.style.left = '100px';
        axPyComposition.style.top = '100px';
        axPyComposition.appendChild(axElement);
        axPyComposition.appendChild(pyElement);
        canvas.appendChild(axPyComposition);

        // Create standalone prompt
        const promptElement = document.createElement('div');
        promptElement.className = 'canvas-prompt-glyph';
        promptElement.setAttribute('data-glyph-id', 'prompt1');
        promptElement.style.position = 'absolute';
        promptElement.style.left = `${100 + MELD_THRESHOLD - 5}px`;
        promptElement.style.top = '100px';
        canvas.appendChild(promptElement);

        // Action: Tim drags prompt near ax-py composition
        // Implementation needed: findMeldTarget should detect compositions as targets
        // Implementation needed: performMeld should handle composition-to-glyph melding

        // Assert: Single 3-glyph composition exists
        const compositions = canvas.querySelectorAll('.melded-composition');
        expect(compositions.length).toBe(1);

        const composition = compositions[0];
        const glyphs = composition.querySelectorAll('[data-glyph-id]');
        expect(glyphs.length).toBe(3);

        // Assert: Glyphs in correct left-to-right order
        expect(glyphs[0]).toBe(axElement);
        expect(glyphs[1]).toBe(pyElement);
        expect(glyphs[2]).toBe(promptElement);

        // Assert: Element identity preserved (no clones)
        expect(promptElement.parentElement).toBe(composition);

        // Cleanup
        document.body.innerHTML = '';
    });

    test('Tim sees proximity feedback when dragging glyph toward composition', () => {
        // Setup: Create canvas with ax-py composition
        const canvas = document.createElement('div');
        canvas.className = 'canvas';
        document.body.appendChild(canvas);

        const axPyComposition = document.createElement('div');
        axPyComposition.className = 'melded-composition';
        axPyComposition.style.position = 'absolute';
        axPyComposition.style.left = '100px';
        axPyComposition.style.top = '100px';
        canvas.appendChild(axPyComposition);

        const promptElement = document.createElement('div');
        promptElement.className = 'canvas-prompt-glyph';
        promptElement.style.position = 'absolute';
        promptElement.style.left = '150px';
        promptElement.style.top = '100px';
        canvas.appendChild(promptElement);

        // Action: Call findMeldTarget with prompt as initiator
        // Implementation needed: findMeldTarget should detect compositions as valid targets
        // const { target, distance } = findMeldTarget(promptElement);

        // Assert: Composition identified as valid target
        // expect(target).toBe(axPyComposition);
        // expect(distance).toBeLessThan(PROXIMITY_THRESHOLD);

        // Assert: Visual feedback applied
        // Implementation needed: applyMeldFeedback should handle composition targets
        // applyMeldFeedback(promptElement, target, distance);
        // expect(promptElement.style.boxShadow).not.toBe('');
        // expect(axPyComposition.style.boxShadow).not.toBe('');

        // Cleanup
        document.body.innerHTML = '';
    });
});

describe.skip('Chain Extension - Tim (Happy Path)', () => {
    test('Tim extends ax|py composition by dragging prompt onto it', () => {
        // Setup: Create canvas with 2-glyph composition (ax|py)
        const canvas = document.createElement('div');
        canvas.className = 'canvas';
        document.body.appendChild(canvas);

        const axElement = document.createElement('div');
        axElement.className = 'canvas-ax-glyph';
        axElement.setAttribute('data-glyph-id', 'ax1');
        axElement.style.position = 'absolute';
        axElement.style.left = '100px';
        axElement.style.top = '100px';
        canvas.appendChild(axElement);

        const pyElement = document.createElement('div');
        pyElement.className = 'canvas-py-glyph';
        pyElement.setAttribute('data-glyph-id', 'py1');
        pyElement.style.position = 'absolute';
        pyElement.style.left = `${100 + MELD_THRESHOLD - 5}px`;
        pyElement.style.top = '100px';
        canvas.appendChild(pyElement);

        const axGlyph: Glyph = {
            id: 'ax1',
            title: 'AX',
            renderContent: () => axElement
        };

        const pyGlyph: Glyph = {
            id: 'py1',
            title: 'Python',
            renderContent: () => pyElement
        };

        // Create initial 2-glyph composition
        const composition = performMeld(axElement, pyElement, axGlyph, pyGlyph);
        const compositionId = composition.getAttribute('data-glyph-id');

        // Create standalone prompt
        const promptElement = document.createElement('div');
        promptElement.className = 'canvas-prompt-glyph';
        promptElement.setAttribute('data-glyph-id', 'prompt1');
        canvas.appendChild(promptElement);

        const promptGlyph: Glyph = {
            id: 'prompt1',
            title: 'Prompt',
            renderContent: () => promptElement
        };

        // Action: Tim drops prompt onto existing composition
        // Implementation needed: extendComposition function in Phase 4
        // const extended = extendComposition(composition, promptElement, promptGlyph);

        // Assert: Same composition ID (not new composition)
        // expect(extended.getAttribute('data-glyph-id')).toBe(compositionId);

        // Assert: Now contains 3 glyphs in order
        const glyphs = composition.querySelectorAll('[data-glyph-id]');
        // expect(glyphs.length).toBe(3);
        // expect(glyphs[0]).toBe(axElement);
        // expect(glyphs[1]).toBe(pyElement);
        // expect(glyphs[2]).toBe(promptElement);

        // Assert: Element identity preserved (prompt not cloned)
        // expect(promptElement.parentElement).toBe(composition);

        // Assert: State updated with 3 glyphs
        // Implementation needed: findCompositionByGlyph should return extended composition
        // const comp = findCompositionByGlyph('prompt1');
        // expect(comp?.glyphIds).toEqual(['ax1', 'py1', 'prompt1']);

        // Cleanup
        document.body.innerHTML = '';
    });

    test('Tim extends 3-glyph chain into 4-glyph chain', () => {
        // Setup: 3-glyph composition (ax|py|prompt) already exists
        const canvas = document.createElement('div');
        canvas.className = 'canvas';
        document.body.appendChild(canvas);

        const axElement = document.createElement('div');
        axElement.setAttribute('data-glyph-id', 'ax1');
        const pyElement = document.createElement('div');
        pyElement.setAttribute('data-glyph-id', 'py1');
        const promptElement = document.createElement('div');
        promptElement.setAttribute('data-glyph-id', 'prompt1');

        const composition = document.createElement('div');
        composition.className = 'melded-composition';
        composition.setAttribute('data-glyph-id', 'melded-ax1-py1-prompt1');
        composition.appendChild(axElement);
        composition.appendChild(pyElement);
        composition.appendChild(promptElement);
        canvas.appendChild(composition);

        // Create another prompt to add
        const prompt2Element = document.createElement('div');
        prompt2Element.className = 'canvas-prompt-glyph';
        prompt2Element.setAttribute('data-glyph-id', 'prompt2');
        canvas.appendChild(prompt2Element);

        // Action: Tim adds prompt2 to existing 3-glyph chain
        // Implementation needed: extendComposition should handle N-glyph chains

        // Assert: 4 glyphs in composition
        // const glyphs = composition.querySelectorAll('[data-glyph-id]');
        // expect(glyphs.length).toBe(4);
        // expect(glyphs[3]).toBe(prompt2Element);

        // Cleanup
        document.body.innerHTML = '';
    });
});
