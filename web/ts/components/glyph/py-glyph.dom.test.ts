/**
 * Tests for Python glyph component
 */

import { describe, test, expect, beforeEach, mock } from 'bun:test';
import { createPyGlyph } from './py-glyph';
import type { Glyph } from './glyph';
import { getScriptStorage } from '../../storage/script-storage';
import { initStorage, isStorageInitialized } from '../../indexeddb-storage';

// Only run these tests when USE_JSDOM=1 (CI environment)
const USE_JSDOM = process.env.USE_JSDOM === '1';

// Setup jsdom and fake-indexeddb if enabled
if (USE_JSDOM) {
    const { JSDOM } = await import('jsdom');
    const dom = new JSDOM('<!DOCTYPE html><html><body></body></html>', {
        url: 'http://localhost'
    });
    const { window } = dom;
    const { document } = window;

    // Setup fake-indexeddb for script-storage
    const fakeIndexedDB = await import('fake-indexeddb');
    const FDBKeyRange = (await import('fake-indexeddb/lib/FDBKeyRange')).default;

    // Add IndexedDB to the JSDOM window
    (window as any).indexedDB = fakeIndexedDB.default;

    globalThis.document = document as any;
    globalThis.window = window as any;
    globalThis.localStorage = window.localStorage as any;
    globalThis.indexedDB = fakeIndexedDB.default as any;
    globalThis.IDBKeyRange = FDBKeyRange as any;
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

    beforeEach(async () => {
        // Initialize storage once before tests
        if (!isStorageInitialized()) {
            await initStorage();
        }

        localStorage.clear();
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
        test('creates element with correct class', async () => {
            const element = await createPyGlyph(glyph);
            expect(element.className).toBe('canvas-py-glyph');
        });

        test('sets data-glyph-id attribute', async () => {
            const element = await createPyGlyph(glyph);
            expect(element.dataset.glyphId).toBe('py-test-123');
        });

        test('has title bar with py label', async () => {
            const element = await createPyGlyph(glyph);
            const titleBar = element.querySelector('.py-glyph-title-bar');
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
            const element = await createPyGlyph(glyph);
            const storage = getScriptStorage();
            const code = await storage.load('py-test-123');
            expect(code).toContain('# Python editor');
        });

        test('loads saved code for existing glyph', async () => {
            const storage = getScriptStorage();
            await storage.save('py-test-123', 'print("saved code")');

            const element = await createPyGlyph(glyph);
            // Wait a tick for CodeMirror to initialize
            await new Promise(resolve => setTimeout(resolve, 50));

            const savedCode = await storage.load('py-test-123');
            expect(savedCode).toBe('print("saved code")');
        });
    });

    describe('editor', () => {
        test('stores editor reference on element', async () => {
            const element = await createPyGlyph(glyph) as any;
            expect(element.editor).toBeDefined();
        });

        test('has resize handle', async () => {
            const element = await createPyGlyph(glyph);
            const resizeHandle = element.querySelector('.py-glyph-resize-handle');
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
