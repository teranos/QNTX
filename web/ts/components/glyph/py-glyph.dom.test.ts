/**
 * Tests for Python glyph component
 */

import { describe, test, expect, beforeEach, mock } from 'bun:test';
import { createPyGlyph } from './py-glyph';
import type { Glyph } from './glyph';

// Mock uiState to prevent API calls during tests
const mockCanvasGlyphs: any[] = [];
const mockCanvasCompositions: any[] = [];
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
    },
}));

// Only run these tests when USE_JSDOM=1 (CI environment)
const USE_JSDOM = process.env.USE_JSDOM === '1';

// Setup jsdom if enabled
if (USE_JSDOM) {
    const { JSDOM } = await import('jsdom');
    const dom = new JSDOM('<!DOCTYPE html><html><body></body></html>', {
        url: 'http://localhost'
    });
    const { window } = dom;
    const { document } = window;

    globalThis.document = document as any;
    globalThis.window = window as any;
    globalThis.localStorage = window.localStorage as any;
    globalThis.crypto = {
        randomUUID: () => 'test-uuid-' + Math.random()
    } as any;

    // Add MutationObserver polyfill for CodeMirror
    // Simple stub that doesn't actually observe
    globalThis.MutationObserver = class MutationObserver {
        constructor(callback: any) {}
        observe() {}
        disconnect() {}
        takeRecords() { return []; }
    } as any;

    // Add requestAnimationFrame polyfill for CodeMirror
    const rafPolyfill = ((callback: FrameRequestCallback) => {
        return setTimeout(callback, 0);
    }) as any;

    const cafPolyfill = ((id: number) => {
        clearTimeout(id);
    }) as any;

    globalThis.requestAnimationFrame = rafPolyfill;
    globalThis.cancelAnimationFrame = cafPolyfill;
    (window as any).requestAnimationFrame = rafPolyfill;
    (window as any).cancelAnimationFrame = cafPolyfill;

    // Add AbortController polyfill
    // jsdom has AbortController but it's not compatible with addEventListener signal option
    // Use the window's native implementations
    globalThis.AbortController = window.AbortController as any;
    globalThis.AbortSignal = window.AbortSignal as any;
}

describe('PyGlyph', () => {
    if (!USE_JSDOM) {
        test.skip('Skipped locally (run with USE_JSDOM=1 to enable)', () => {});
        return;
    }

    let glyph: Glyph;

    beforeEach(() => {
        localStorage.clear();
        mockCanvasGlyphs.length = 0;
        mockCanvasCompositions.length = 0;
        glyph = {
            id: 'py-test-123',
            title: 'Python',
            symbol: 'py',
            gridX: 5,
            gridY: 5,
            renderContent: () => document.createElement('div')
        };
    });

    describe('initialization', () => {
        test('sets data-glyph-id attribute', async () => {
            const element = await createPyGlyph(glyph);
            expect(element.dataset.glyphId).toBe('py-test-123');
        });

        test('has title bar with py label', async () => {
            const element = await createPyGlyph(glyph);
            const titleBar = element.querySelector('.canvas-glyph-title-bar');
            expect(titleBar).not.toBeNull();
            expect(titleBar?.textContent).toContain('py');
        });

        test('has run button', async () => {
            const element = await createPyGlyph(glyph);
            const runButton = element.querySelector('button');
            expect(runButton).not.toBeNull();
            expect(runButton?.title).toBe('Run Python code');
        });
    });

    describe('code persistence', () => {
        test('loads default code for new glyph', async () => {
            // Pre-populate uiState with glyph (simulates canvas spawn)
            mockCanvasGlyphs.push({
                id: 'py-test-123',
                symbol: 'py',
                x: 0,
                y: 0,
            });

            const element = await createPyGlyph(glyph);
            // Wait for CodeMirror to initialize and save
            await new Promise(resolve => setTimeout(resolve, 50));

            // Check that default code was saved to uiState
            const saved = mockCanvasGlyphs.find(g => g.id === 'py-test-123');
            expect(saved?.content).toContain('# Python editor');
        });

        test('loads saved code for existing glyph', async () => {
            // Pre-populate uiState with saved code
            mockCanvasGlyphs.push({
                id: 'py-test-123',
                symbol: 'py',
                content: 'print("saved code")',
                x: 0,
                y: 0,
            });

            const element = await createPyGlyph(glyph);
            // Wait a tick for CodeMirror to initialize
            await new Promise(resolve => setTimeout(resolve, 50));

            // Verify the saved code was loaded
            const saved = mockCanvasGlyphs.find(g => g.id === 'py-test-123');
            expect(saved?.content).toBe('print("saved code")');
        });
    });

    describe('editor', () => {
        test('stores editor reference on element', async () => {
            const element = await createPyGlyph(glyph) as any;
            expect(element.editor).toBeDefined();
        });

        test('has resize handle', async () => {
            const element = await createPyGlyph(glyph);
            const resizeHandle = element.querySelector('.glyph-resize-handle');
            expect(resizeHandle).not.toBeNull();
        });
    });

    describe('execution', () => {
        test('run button exists and is clickable', async () => {
            const element = await createPyGlyph(glyph);
            const runButton = element.querySelector('button');
            expect(runButton).not.toBeNull();
            expect(runButton?.textContent).toBe('▶');
        });

        // Note: Full execution tests require mocking fetch and would be integration tests
        // This would test the UI structure, not the full execution flow
    });
});
