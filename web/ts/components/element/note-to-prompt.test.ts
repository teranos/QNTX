/**
 * Tests for element conversions
 *
 * Personas:
 * - Tim: Happy path user, normal workflows
 * - Spike: Tries to break things, edge cases
 * - Jenny: Power user, complex scenarios
 */

import { describe, test, expect, mock } from 'bun:test';
import { convertNoteToPrompt, convertResultToNote } from './conversions';
import { SO, Prose } from '../../sym';

// Mock ResizeObserver for tests
globalThis.ResizeObserver = class ResizeObserver {
    observe() {}
    unobserve() {}
    disconnect() {}
} as any;

// Mock uiState — process-global, must be superset-complete (see test/mock-ui-state.ts)
import { createMockUiState } from '../../test/mock-ui-state';
const { uiState, elements: mockCanvasElements } = createMockUiState();
mock.module('../../state/ui', () => ({ uiState }));

describe('Element Conversions - Tim (Happy Path)', () => {
    test('Tim converts note to prompt successfully', async () => {
        // Clear mock state
        mockCanvasElements.length = 0;

        // Tim creates a canvas
        const container = document.createElement('div');
        container.className = 'canvas-workspace';

        // Tim creates a note element with some content
        const noteElement = document.createElement('div');
        noteElement.className = 'canvas-note-element canvas-element';
        noteElement.dataset.elementId = 'note-123';
        noteElement.dataset.symbol = Prose;

        const textarea = document.createElement('textarea');
        textarea.value = 'Write a haiku about canvas';
        noteElement.appendChild(textarea);

        container.appendChild(noteElement);

        // Add element to mock uiState
        mockCanvasElements.push({
            id: 'note-123',
            symbol: Prose,
            x: 0,
            y: 0,
            content: 'Write a haiku about canvas',
        });

        // Tim clicks "convert to prompt"
        const success = await convertNoteToPrompt(container, 'note-123');

        // Conversion succeeds
        expect(success).toBe(true);

        // Same element is still in container (single-element axiom)
        expect(container.children.length).toBe(1);
        const convertedElement = container.firstElementChild as HTMLElement;

        // It's now a prompt element
        expect(convertedElement.classList.contains('canvas-prompt-element')).toBe(true);
        expect(convertedElement.classList.contains('canvas-note-element')).toBe(false);
        expect(convertedElement.dataset.symbol).toBe(SO);
    });

    test('Tim converts result to note successfully', async () => {
        // Clear mock state
        mockCanvasElements.length = 0;

        // Tim creates a canvas
        const container = document.createElement('div');
        container.className = 'canvas-workspace';

        // Tim has a result element with execution output
        const resultElement = document.createElement('div');
        resultElement.className = 'canvas-result-element canvas-element';
        resultElement.dataset.elementId = 'result-456';
        resultElement.dataset.symbol = 'result';

        // Result has output content
        const outputDiv = document.createElement('div');
        outputDiv.className = 'result-element-output';
        outputDiv.textContent = 'Hello from Python!\n42';
        resultElement.appendChild(outputDiv);

        container.appendChild(resultElement);

        // Add element to mock uiState
        mockCanvasElements.push({
            id: 'result-456',
            symbol: 'result',
            x: 0,
            y: 0,
        });

        // Tim clicks "convert to note"
        const success = await convertResultToNote(container, 'result-456');

        // Conversion succeeds
        expect(success).toBe(true);

        // Same element is still in container (single-element axiom)
        expect(container.children.length).toBe(1);
        const convertedElement = container.firstElementChild as HTMLElement;

        // It's now a note element
        expect(convertedElement.classList.contains('canvas-note-element')).toBe(true);
        expect(convertedElement.classList.contains('canvas-result-element')).toBe(false);
        expect(convertedElement.dataset.symbol).toBe(Prose);

        // Note element structure exists (uses ProseMirror editor, not textarea)
        const editorContainer = convertedElement.querySelector('.note-editor-container');
        expect(editorContainer).toBeTruthy();
    });
});

describe('Element Conversions - Spike (Edge Cases)', () => {
    test('Spike tries to convert non-existent element', async () => {
        // Clear mock state
        mockCanvasElements.length = 0;

        // Spike creates a canvas
        const container = document.createElement('div');
        container.className = 'canvas-workspace';

        // Spike tries to convert an element that doesn't exist
        const success = await convertNoteToPrompt(container, 'nonexistent-element-id');

        // Conversion fails gracefully
        expect(success).toBe(false);
        expect(container.children.length).toBe(0);
    });
});

describe('Element Conversions - Jenny (Complex Scenarios)', () => {
    test('Jenny cannot convert element inside melded composition', async () => {
        // Clear mock state
        mockCanvasElements.length = 0;

        // Jenny has a canvas with a melded composition
        const container = document.createElement('div');
        container.className = 'canvas-workspace';

        // Create a composition wrapper (simulating melded state)
        const composition = document.createElement('div');
        composition.className = 'melded-composition';
        composition.dataset.compositionId = 'comp-123';

        // Add note element inside composition
        const noteElement = document.createElement('div');
        noteElement.className = 'canvas-note-element canvas-element';
        noteElement.dataset.elementId = 'note-nested';
        noteElement.dataset.symbol = Prose;

        const textarea = document.createElement('textarea');
        textarea.value = 'Note inside composition';
        noteElement.appendChild(textarea);

        composition.appendChild(noteElement);
        container.appendChild(composition);

        // Add element to mock uiState
        mockCanvasElements.push({
            id: 'note-nested',
            symbol: Prose,
            x: 0,
            y: 0,
            content: 'Note inside composition',
        });

        // Jenny tries to convert the note inside the composition
        const success = await convertNoteToPrompt(composition, 'note-nested');

        // Conversion is blocked - cannot convert elements inside compositions
        expect(success).toBe(false);

        // Composition structure is unchanged
        expect(composition.children.length).toBe(1);
        const unchangedElement = composition.firstElementChild as HTMLElement;

        // Element is still a note (not converted)
        expect(unchangedElement.classList.contains('canvas-note-element')).toBe(true);
        expect(unchangedElement.classList.contains('canvas-prompt-element')).toBe(false);
        expect(unchangedElement.dataset.symbol).toBe(Prose);
        expect(unchangedElement.dataset.elementId).toBe('note-nested');

        // Composition is intact
        expect(composition.classList.contains('melded-composition')).toBe(true);
    });
});
