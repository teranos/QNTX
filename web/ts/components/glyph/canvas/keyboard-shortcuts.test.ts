/**
 * Tests for canvas keyboard shortcuts
 * Focus: AbortController pattern and callback invocation
 *
 * Personas:
 * - Tim: Happy path user, normal workflows
 */

import { describe, test, expect } from 'bun:test';
import { setupKeyboardShortcuts, type Direction } from './keyboard-shortcuts';

function noop() { }

describe('Canvas Keyboard Shortcuts', () => {
    test('returns AbortController for cleanup', () => {
        const container = document.createElement('div');
        const controller = setupKeyboardShortcuts(
            container,
            () => true,
            noop, noop, noop, noop, noop, noop
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
                noop, noop, noop, noop, noop, noop
            );
        }).not.toThrow();
    });

    test('abort() method is available for cleanup', () => {
        const container = document.createElement('div');

        const controller = setupKeyboardShortcuts(
            container,
            () => true,
            noop, noop, noop, noop, noop, noop
        );

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
            noop, noop, onUnmeld, noop, noop, noop
        );

        const event = new window.KeyboardEvent('keydown', {
            key: 'u',
            bubbles: true,
            cancelable: true
        });
        document.dispatchEvent(event);

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
            noop, noop, noop, onResetView, noop, noop
        );

        const event = new window.KeyboardEvent('keydown', {
            key: '0',
            bubbles: true,
            cancelable: true
        });
        document.dispatchEvent(event);

        expect(resetViewCalled).toBe(true);
    });

    test('Tim presses hjkl for directional navigation', () => {
        const container = document.createElement('div');
        document.body.appendChild(container);

        const directions: Direction[] = [];
        const onNavigate = (dir: Direction) => { directions.push(dir); };

        setupKeyboardShortcuts(
            container,
            () => false,
            noop, noop, noop, noop, onNavigate, noop
        );

        for (const [key, expected] of [['h', 'left'], ['j', 'down'], ['k', 'up'], ['l', 'right']] as const) {
            document.dispatchEvent(new window.KeyboardEvent('keydown', {
                key,
                bubbles: true,
                cancelable: true
            }));
        }

        expect(directions).toEqual(['left', 'down', 'up', 'right']);
    });

    test('Tim presses Enter to focus selected glyph', () => {
        const container = document.createElement('div');
        document.body.appendChild(container);

        let enterCalled = false;
        const onEnter = () => { enterCalled = true; };

        setupKeyboardShortcuts(
            container,
            () => true, // has selection
            noop, noop, noop, noop, noop, onEnter
        );

        document.dispatchEvent(new window.KeyboardEvent('keydown', {
            key: 'Enter',
            bubbles: true,
            cancelable: true
        }));

        expect(enterCalled).toBe(true);
    });
});
