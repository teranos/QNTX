/**
 * Tests for canvas keyboard shortcuts
 * Focus: AbortController pattern and callback invocation
 */

import { describe, test, expect } from 'bun:test';
import { setupKeyboardShortcuts } from './keyboard-shortcuts';

describe('Canvas Keyboard Shortcuts', () => {
    test('returns AbortController for cleanup', () => {
        const container = document.createElement('div');
        const controller = setupKeyboardShortcuts(
            container,
            () => true,
            () => { },
            () => { }
        );

        expect(controller).toBeInstanceOf(AbortController);
        expect(controller.signal).toBeDefined();
    });

    test('accepts all required callbacks without errors', () => {
        const container = document.createElement('div');

        expect(() => {
            setupKeyboardShortcuts(
                container,
                () => true,
                () => { },
                () => { }
            );
        }).not.toThrow();
    });

    test('abort() method is available for cleanup', () => {
        const container = document.createElement('div');

        const controller = setupKeyboardShortcuts(
            container,
            () => true,
            () => { },
            () => { }
        );

        // Verify abort method exists and can be called
        expect(typeof controller.abort).toBe('function');
        expect(() => controller.abort()).not.toThrow();
    });
});
