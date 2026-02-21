/**
 * Tests for per-canvas glyph isolation via canvas_id
 *
 * Located in ts/state/ to avoid mock.module leaks from glyph test files
 * that mock ../../state/ui (mock.module is process-global in bun).
 *
 * Personas:
 * - Tim: Happy path user, normal workflows
 */

import { describe, test, expect, beforeEach } from 'bun:test';

// Import from ui-impl.ts directly to bypass process-global mock.module
// that replaces ./ui in other test files (mock.module is process-global in Bun).
import { UIState } from './ui-impl';
const uiState = new UIState();

beforeEach(() => {
    uiState.setCanvasGlyphs([]);
});

describe('canvas_id Isolation - Tim (Happy Path)', () => {
    test('Tim spawns glyphs in a subcanvas and they stay isolated from root', () => {
        uiState.addCanvasGlyph({
            id: 'root-note-1', symbol: 'note', x: 50, y: 50, canvas_id: '',
        });
        uiState.addCanvasGlyph({
            id: 'inner-note-1', symbol: 'note', x: 100, y: 100, canvas_id: 'subcanvas-test-1',
        });

        // Root canvas should only see root glyphs
        const rootGlyphs = uiState.getCanvasGlyphs('canvas-workspace');
        expect(rootGlyphs.length).toBe(1);
        expect(rootGlyphs[0].id).toBe('root-note-1');

        // Subcanvas should only see its inner glyphs
        const innerGlyphs = uiState.getCanvasGlyphs('subcanvas-test-1');
        expect(innerGlyphs.length).toBe(1);
        expect(innerGlyphs[0].id).toBe('inner-note-1');
    });

    test('Tim drags a glyph in a subcanvas and canvas_id is preserved', () => {
        uiState.addCanvasGlyph({
            id: 'inner-note-1', symbol: 'note', x: 100, y: 100,
            canvas_id: 'subcanvas-test-1', content: 'my note',
        });

        // Simulate what drag handler does: spread existing + override position
        const existing = uiState.getCanvasGlyphs().find(g => g.id === 'inner-note-1');
        uiState.addCanvasGlyph({
            ...existing!,
            id: 'inner-note-1',
            symbol: 'note',
            x: 200,
            y: 250,
        });

        // canvas_id and content must survive the drag
        const updated = uiState.getCanvasGlyphs().find(g => g.id === 'inner-note-1');
        expect(updated?.canvas_id).toBe('subcanvas-test-1');
        expect(updated?.content).toBe('my note');
        expect(updated?.x).toBe(200);
        expect(updated?.y).toBe(250);
    });

    test('Tim resizes a glyph in a subcanvas and canvas_id is preserved', () => {
        uiState.addCanvasGlyph({
            id: 'inner-note-2', symbol: 'note', x: 50, y: 50,
            canvas_id: 'subcanvas-test-1', content: 'resize me',
        });

        // Simulate what resize handler does: spread existing + override size
        const existing = uiState.getCanvasGlyphs().find(g => g.id === 'inner-note-2');
        uiState.addCanvasGlyph({
            ...existing!,
            id: 'inner-note-2',
            symbol: 'note',
            x: 50,
            y: 50,
            width: 300,
            height: 200,
        });

        const updated = uiState.getCanvasGlyphs().find(g => g.id === 'inner-note-2');
        expect(updated?.canvas_id).toBe('subcanvas-test-1');
        expect(updated?.content).toBe('resize me');
        expect(updated?.width).toBe(300);
        expect(updated?.height).toBe(200);
    });
});
