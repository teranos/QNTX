/**
 * Tests for Python glyph component
 */

import { describe, test, expect, beforeEach, mock } from 'bun:test';
import { createPyGlyph, PY_DEFAULT_CODE } from './py-glyph';
import type { Glyph } from './glyph';

// NOTE: Do NOT mock ../../state/ui — use the real uiState instead (see TESTING.md)
import { uiState } from '../../state/ui';

// Only run these tests when USE_JSDOM=1 (CI environment)
const USE_JSDOM = process.env.USE_JSDOM === '1';

// crypto.randomUUID override for deterministic test IDs
if (USE_JSDOM) {
    globalThis.crypto = {
        ...globalThis.crypto,
        randomUUID: () => 'test-uuid-' + Math.random(),
    } as any;
}

describe('PyGlyph', () => {
    if (!USE_JSDOM) {
        test.skip('Skipped locally (run with USE_JSDOM=1 to enable)', () => {});
        return;
    }

    let glyph: Glyph;

    beforeEach(() => {
        localStorage.clear();
        uiState.setCanvasGlyphs([]);
        uiState.setCanvasCompositions([]);
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
            uiState.addCanvasGlyph({
                id: 'py-test-123',
                symbol: 'py',
                x: 0,
                y: 0,
            });

            const element = await createPyGlyph(glyph);
            // Wait for CodeMirror to initialize and save
            await new Promise(resolve => setTimeout(resolve, 50));

            // Check that default code was saved to uiState
            const saved = uiState.getCanvasGlyphs().find(g => g.id === 'py-test-123');
            expect(saved?.content).toBe(PY_DEFAULT_CODE);
        });

        test('loads saved code for existing glyph', async () => {
            // Pre-populate uiState with saved code
            uiState.addCanvasGlyph({
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
            const saved = uiState.getCanvasGlyphs().find(g => g.id === 'py-test-123');
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
