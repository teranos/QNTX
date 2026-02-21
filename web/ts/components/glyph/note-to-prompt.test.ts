/**
 * Tests for glyph conversions
 *
 * Personas:
 * - Tim: Happy path user, normal workflows
 * - Spike: Tries to break things, edge cases
 * - Jenny: Power user, complex scenarios
 */

import { describe, test, expect, mock } from 'bun:test';
import { convertNoteToPrompt, convertResultToNote } from './conversions';
import { SO, Prose } from '@generated/sym.js';

// Mock ResizeObserver for tests
globalThis.ResizeObserver = class ResizeObserver {
    observe() {}
    unobserve() {}
    disconnect() {}
} as any;

// Mock uiState for conversions
const mockCanvasGlyphs: any[] = [];
const mockCanvasCompositions: any[] = [];
const mockCanvasPan: Record<string, any> = {};
mock.module('../../state/ui', () => ({
    uiState: {
        getCanvasGlyphs: () => mockCanvasGlyphs,
        setCanvasGlyphs: (glyphs: any[]) => {
            mockCanvasGlyphs.length = 0;
            mockCanvasGlyphs.push(...glyphs);
        },
        upsertCanvasGlyph: (glyph: any) => {
            const index = mockCanvasGlyphs.findIndex(g => g.id === glyph.id);
            if (index >= 0) {
                mockCanvasGlyphs[index] = glyph;
            } else {
                mockCanvasGlyphs.push(glyph);
            }
        },
        addCanvasGlyph: (glyph: any) => {
            const index = mockCanvasGlyphs.findIndex(g => g.id === glyph.id);
            if (index >= 0) {
                mockCanvasGlyphs[index] = glyph;
            } else {
                mockCanvasGlyphs.push(glyph);
            }
        },
        removeCanvasGlyph: (id: string) => {
            const index = mockCanvasGlyphs.findIndex(g => g.id === id);
            if (index >= 0) mockCanvasGlyphs.splice(index, 1);
        },
        getCanvasCompositions: () => mockCanvasCompositions,
        setCanvasCompositions: (comps: any[]) => {
            mockCanvasCompositions.length = 0;
            mockCanvasCompositions.push(...comps);
        },
        clearCanvasGlyphs: () => mockCanvasGlyphs.length = 0,
        clearCanvasCompositions: () => mockCanvasCompositions.length = 0,
        loadPersistedState: () => {},
        // Superset-complete stubs (mock.module is process-global, leaks into other test files)
        getCanvasPan: (id: string) => mockCanvasPan[id] ?? null,
        setCanvasPan: (id: string, pan: any) => { mockCanvasPan[id] = pan; },
        getMinimizedWindows: () => [],
        addMinimizedWindow: () => {},
        removeMinimizedWindow: () => {},
        setMinimizedWindows: () => {},
        isWindowMinimized: () => false,
        clearMinimizedWindows: () => {},
        isPanelVisible: () => false,
        setPanelVisible: () => {},
        togglePanel: () => false,
        closeAllPanels: () => {},
        getActiveModality: () => 'ax',
        setActiveModality: () => {},
        getBudgetWarnings: () => ({ daily: false, weekly: false, monthly: false }),
        setBudgetWarning: () => {},
        resetBudgetWarnings: () => {},
        getUsageView: () => 'week',
        setUsageView: () => {},
        getGraphSession: () => ({}),
        setGraphSession: () => {},
        setGraphQuery: () => {},
        setGraphVerbosity: () => {},
        clearGraphSession: () => {},
        subscribe: () => () => {},
        subscribeAll: () => () => {},
        getState: () => ({}),
        get: () => undefined,
        clearStorage: () => {},
        reset: () => {},
    },
}));

describe('Glyph Conversions - Tim (Happy Path)', () => {
    test('Tim converts note to prompt successfully', async () => {
        // Clear mock state
        mockCanvasGlyphs.length = 0;

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

        // Add glyph to mock uiState
        mockCanvasGlyphs.push({
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

        // It's now a prompt glyph
        expect(convertedElement.classList.contains('canvas-prompt-glyph')).toBe(true);
        expect(convertedElement.classList.contains('canvas-note-glyph')).toBe(false);
        expect(convertedElement.dataset.glyphSymbol).toBe(SO);
    });

    test('Tim converts result to note successfully', async () => {
        // Clear mock state
        mockCanvasGlyphs.length = 0;

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

        // Add glyph to mock uiState
        mockCanvasGlyphs.push({
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
        // Clear mock state
        mockCanvasGlyphs.length = 0;

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

describe('Glyph Conversions - Jenny (Complex Scenarios)', () => {
    test('Jenny cannot convert glyph inside melded composition', async () => {
        // Clear mock state
        mockCanvasGlyphs.length = 0;

        // Jenny has a canvas with a melded composition
        const container = document.createElement('div');
        container.className = 'canvas-workspace';

        // Create a composition wrapper (simulating melded state)
        const composition = document.createElement('div');
        composition.className = 'melded-composition';
        composition.dataset.compositionId = 'comp-123';

        // Add note glyph inside composition
        const noteElement = document.createElement('div');
        noteElement.className = 'canvas-note-glyph canvas-glyph';
        noteElement.dataset.glyphId = 'note-nested';
        noteElement.dataset.glyphSymbol = Prose;

        const textarea = document.createElement('textarea');
        textarea.value = 'Note inside composition';
        noteElement.appendChild(textarea);

        composition.appendChild(noteElement);
        container.appendChild(composition);

        // Add glyph to mock uiState
        mockCanvasGlyphs.push({
            id: 'note-nested',
            symbol: Prose,
            x: 0,
            y: 0,
            content: 'Note inside composition',
        });

        // Jenny tries to convert the note inside the composition
        const success = await convertNoteToPrompt(composition, 'note-nested');

        // Conversion is blocked - cannot convert glyphs inside compositions
        expect(success).toBe(false);

        // Composition structure is unchanged
        expect(composition.children.length).toBe(1);
        const unchangedElement = composition.firstElementChild as HTMLElement;

        // Glyph is still a note (not converted)
        expect(unchangedElement.classList.contains('canvas-note-glyph')).toBe(true);
        expect(unchangedElement.classList.contains('canvas-prompt-glyph')).toBe(false);
        expect(unchangedElement.dataset.glyphSymbol).toBe(Prose);
        expect(unchangedElement.dataset.glyphId).toBe('note-nested');

        // Composition is intact
        expect(composition.classList.contains('melded-composition')).toBe(true);
    });
});
