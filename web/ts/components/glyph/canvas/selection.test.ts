/**
 * Tests for per-canvas selection isolation
 *
 * Personas:
 * - Tim: Happy path user, normal workflows
 */

import { describe, test, expect, beforeEach } from 'bun:test';
import {
    isGlyphSelected, getSelectedGlyphIds, hasSelection,
    addToSelection, replaceSelection, clearSelection, destroyCanvasSelection,
} from './selection';

beforeEach(() => {
    clearSelection('canvas-workspace');
    clearSelection('subcanvas-1');
});

describe('Per-Canvas Selection Isolation - Tim (Happy Path)', () => {
    test('Tim selects glyphs in root and subcanvas independently', () => {
        addToSelection('canvas-workspace', 'note-1');
        addToSelection('subcanvas-1', 'note-2');

        expect(getSelectedGlyphIds('canvas-workspace')).toEqual(['note-1']);
        expect(getSelectedGlyphIds('subcanvas-1')).toEqual(['note-2']);
        expect(isGlyphSelected('canvas-workspace', 'note-2')).toBe(false);
        expect(isGlyphSelected('subcanvas-1', 'note-1')).toBe(false);
    });

    test('Tim minimizes subcanvas and its selection is destroyed', () => {
        addToSelection('subcanvas-1', 'inner-note');
        expect(hasSelection('subcanvas-1')).toBe(true);

        destroyCanvasSelection('subcanvas-1');

        expect(hasSelection('subcanvas-1')).toBe(false);
        // Root canvas unaffected
        addToSelection('canvas-workspace', 'root-note');
        expect(hasSelection('canvas-workspace')).toBe(true);
    });
});
