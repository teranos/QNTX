/**
 * Tests for per-canvas selection isolation
 *
 * Personas:
 * - Tim: Happy path user, normal workflows
 */

import { describe, test, expect, beforeEach } from 'bun:test';
import {
    isElementSelected, getSelectedElementIds, hasSelection,
    addToSelection, replaceSelection, clearSelection, destroyCanvasSelection,
} from './selection';

beforeEach(() => {
    clearSelection('canvas-workspace');
    clearSelection('subcanvas-1');
});

describe('Per-Canvas Selection Isolation - Tim (Happy Path)', () => {
    test('Tim selects elements in root and subcanvas independently', () => {
        addToSelection('canvas-workspace', 'note-1');
        addToSelection('subcanvas-1', 'note-2');

        expect(getSelectedElementIds('canvas-workspace')).toEqual(['note-1']);
        expect(getSelectedElementIds('subcanvas-1')).toEqual(['note-2']);
        expect(isElementSelected('canvas-workspace', 'note-2')).toBe(false);
        expect(isElementSelected('subcanvas-1', 'note-1')).toBe(false);
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
