/**
 * Tests for canvas keyboard shortcuts
 * Focus: AbortController pattern and callback invocation
 *
 * Personas:
 * - Tim: Happy path user, normal workflows
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
            () => { },
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
            () => { },
            () => { },
            () => { }
        );

        // Verify abort method exists and can be called
        expect(typeof controller.abort).toBe('function');
        expect(() => controller.abort()).not.toThrow();
    });

    test('Tim presses u to unmeld a composition', () => {
        const container = document.createElement('div');
        document.body.appendChild(container);

        let unmeldCalled = false;
        const onUnmeld = () => { unmeldCalled = true; };

        setupKeyboardShortcuts(
            container,
            () => true, // has selection
            () => { },
            () => { },
            onUnmeld
        );

        // Simulate 'u' key press
        const event = new window.KeyboardEvent('keydown', {
            key: 'u',
            bubbles: true,
            cancelable: true
        });
        container.dispatchEvent(event);

        expect(unmeldCalled).toBe(true);
    });

    test('Tim presses 0 to reset zoom and pan', () => {
        const container = document.createElement('div');
        document.body.appendChild(container);

        let resetViewCalled = false;
        const onResetView = () => { resetViewCalled = true; };

        setupKeyboardShortcuts(
            container,
            () => false, // no selection
            () => { },
            () => { },
            () => { },
            onResetView
        );

        // Simulate '0' key press
        const event = new window.KeyboardEvent('keydown', {
            key: '0',
            bubbles: true,
            cancelable: true
        });
        container.dispatchEvent(event);

        expect(resetViewCalled).toBe(true);
    });
});
