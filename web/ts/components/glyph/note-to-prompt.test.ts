/**
 * Tests for glyph conversions
 *
 * Personas:
 * - Tim: Happy path user, normal workflows
 * - Spike: Tries to break things, edge cases
 * - Jenny: Power user, complex scenarios
 */

import { describe, test, expect } from 'bun:test';
import { Window } from 'happy-dom';
import { convertNoteToPrompt, convertResultToNote } from './conversions';
import { SO, Prose } from '@generated/sym.js';

// Setup happy-dom
const window = new Window();
const document = window.document;
globalThis.document = document as any;
globalThis.window = window as any;
globalThis.localStorage = window.localStorage;

// Mock ResizeObserver for tests
globalThis.ResizeObserver = class ResizeObserver {
    observe() {}
    unobserve() {}
    disconnect() {}
} as any;

describe('Glyph Conversions - Tim (Happy Path)', () => {
    test('Tim converts note to prompt successfully', async () => {
        // Tim creates a canvas
        const container = document.createElement('div');
        container.className = 'canvas-workspace';

        // Tim creates a note glyph with some content
        const noteElement = document.createElement('div');
        noteElement.className = 'canvas-note-glyph canvas-glyph';
        noteElement.dataset.glyphId = 'note-123';
        noteElement.dataset.glyphSymbol = Prose;

        const textarea = document.createElement('textarea');
        textarea.value = 'Write a haiku about canvas';
        noteElement.appendChild(textarea);

        container.appendChild(noteElement);

        // Tim clicks "convert to prompt"
        const success = await convertNoteToPrompt(container, 'note-123');

        // Conversion succeeds
        expect(success).toBe(true);

        // Same element is still in container (single-element axiom)
        expect(container.children.length).toBe(1);
        const convertedElement = container.firstElementChild as HTMLElement;

        // It's now a prompt glyph
        expect(convertedElement.classList.contains('canvas-prompt-glyph')).toBe(true);
        expect(convertedElement.classList.contains('canvas-note-glyph')).toBe(false);
        expect(convertedElement.dataset.glyphSymbol).toBe(SO);
    });

    test('Tim converts result to note successfully', async () => {
        // Tim creates a canvas
        const container = document.createElement('div');
        container.className = 'canvas-workspace';

        // Tim has a result glyph with execution output
        const resultElement = document.createElement('div');
        resultElement.className = 'canvas-result-glyph canvas-glyph';
        resultElement.dataset.glyphId = 'result-456';
        resultElement.dataset.glyphSymbol = 'result';

        // Result has output content
        const outputDiv = document.createElement('div');
        outputDiv.className = 'result-glyph-output';
        outputDiv.textContent = 'Hello from Python!\n42';
        resultElement.appendChild(outputDiv);

        container.appendChild(resultElement);

        // Tim clicks "convert to note"
        const success = await convertResultToNote(container, 'result-456');

        // Conversion succeeds
        expect(success).toBe(true);

        // Same element is still in container (single-element axiom)
        expect(container.children.length).toBe(1);
        const convertedElement = container.firstElementChild as HTMLElement;

        // It's now a note glyph
        expect(convertedElement.classList.contains('canvas-note-glyph')).toBe(true);
        expect(convertedElement.classList.contains('canvas-result-glyph')).toBe(false);
        expect(convertedElement.dataset.glyphSymbol).toBe(Prose);

        // Note glyph structure exists (uses ProseMirror editor, not textarea)
        const editorContainer = convertedElement.querySelector('.note-editor-container');
        expect(editorContainer).toBeTruthy();
    });
});

describe('Glyph Conversions - Spike (Edge Cases)', () => {
    test('Spike tries to convert non-existent glyph', async () => {
        // Spike creates a canvas
        const container = document.createElement('div');
        container.className = 'canvas-workspace';

        // Spike tries to convert a glyph that doesn't exist
        const success = await convertNoteToPrompt(container, 'nonexistent-glyph-id');

        // Conversion fails gracefully
        expect(success).toBe(false);
        expect(container.children.length).toBe(0);
    });
});
