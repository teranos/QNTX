/**
 * Tests for canvas action bar
 *
 * Personas:
 * - Tim: Happy path user, normal workflows
 * - Spike: Tries to break things, edge cases
 * - Jenny: Power user, complex scenarios
 */

import { describe, test, expect } from 'bun:test';
import { Window } from 'happy-dom';
import { showActionBar, hideActionBar } from './action-bar';

// Setup happy-dom
const window = new Window();
const document = window.document;
globalThis.document = document as any;
globalThis.window = window as any;

// Mock animate for tests
(window as any).Element.prototype.animate = function() {
    return { finished: Promise.resolve() } as any;
};

describe('Canvas Action Bar - Tim (Happy Path)', () => {
    test('Tim selects glyphs and sees action bar', () => {
        const container = document.createElement('div');
        container.style.position = 'relative';
        document.body.appendChild(container);

        const selectedIds = ['glyph-1', 'glyph-2'];
        let onDeleteCalled = false;
        let onUnmeldCalled = false;

        // Tim selects multiple glyphs and action bar appears
        expect(() => {
            showActionBar(
                selectedIds,
                container,
                () => { onDeleteCalled = true; },
                (comp) => { onUnmeldCalled = true; }
            );
        }).not.toThrow();

        // Cleanup
        document.body.innerHTML = '';
    });
});
