/**
 * Tests for canvas keyboard shortcuts
 * Focus: callback invocation and cleanup behavior
 */

import { describe, test, expect } from 'bun:test';
import { setupKeyboardShortcuts } from './keyboard-shortcuts';

describe('Canvas Keyboard Shortcuts', () => {
    test('returns cleanup function', () => {
        const container = document.createElement('div');
        const cleanup = setupKeyboardShortcuts(
            container,
            () => true,
            () => { },
            () => { }
        );

        expect(typeof cleanup).toBe('function');
    });

    test('accepts all required callbacks without errors', () => {
        const container = document.createElement('div');
        let hasSelectionCalled = false;

        expect(() => {
            setupKeyboardShortcuts(
                container,
                () => { hasSelectionCalled = true; return true; },
                () => { },
                () => { }
            );
        }).not.toThrow();
    });
});
