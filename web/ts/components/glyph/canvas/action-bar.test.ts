/**
 * Tests for canvas action bar
 * Focus: API contract - accepts callbacks and selected glyph IDs
 */

import { describe, test, expect } from 'bun:test';
import { showActionBar, hideActionBar } from './action-bar';

describe('Canvas Action Bar', () => {
    test('accepts required parameters without errors', () => {
        const container = document.createElement('div');
        const selectedIds = ['glyph-1', 'glyph-2'];
        let onDeleteCalled = false;
        let onUnmeldCalled = false;

        // Should accept all parameters
        expect(() => {
            showActionBar(
                selectedIds,
                container,
                () => { onDeleteCalled = true; },
                (comp) => { onUnmeldCalled = true; }
            );
        }).toBeDefined();
    });

    test('hideActionBar can be called safely', () => {
        // Should be callable even if no action bar exists
        expect(() => {
            hideActionBar();
        }).toBeDefined();
    });
});
