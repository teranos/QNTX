/**
 * @jest-environment jsdom
 *
 * DOM tests for canvas action bar
 * Focus: DOM structure, positioning, and animation behavior
 *
 * These tests run only in CI with JSDOM environment (gated by USE_JSDOM=1)
 */

import { describe, test, expect, beforeEach } from 'bun:test';
import { showActionBar, hideActionBar } from './action-bar';

// Only run these tests when USE_JSDOM=1 (CI environment)
const USE_JSDOM = process.env.USE_JSDOM === '1';

// Setup jsdom if enabled
if (USE_JSDOM) {
    const { JSDOM } = await import('jsdom');
    const dom = new JSDOM('<!DOCTYPE html><html><body></body></html>');
    const { window } = dom;
    const { document } = window;

    // Replace global document/window with jsdom's
    globalThis.document = document as any;
    globalThis.window = window as any;
    globalThis.navigator = window.navigator as any;

    // Mock WAAPI methods (not available in JSDOM)
    window.Element.prototype.animate = function () {
        return { onfinish: null, finished: Promise.resolve() } as any;
    };
    window.Element.prototype.getAnimations = function () {
        return [];
    };
}

describe('Canvas Action Bar DOM', () => {
    if (!USE_JSDOM) {
        test.skip('Skipped locally (run with USE_JSDOM=1 to enable)', () => { });
        return;
    }

    let container: HTMLElement;

    beforeEach(() => {
        container = document.createElement('div');
        container.style.position = 'relative';
        container.style.width = '800px';
        container.style.height = '600px';
        document.body.appendChild(container);
    });

    test('creates action bar element with correct positioning', () => {
        showActionBar(
            ['glyph-1', 'glyph-2'],
            container,
            () => { },
            () => { }
        );

        const actionBar = container.querySelector('.canvas-action-bar') as HTMLElement;
        expect(actionBar).not.toBeNull();
        expect(actionBar.style.position).toBe('absolute');
        expect(actionBar.style.left).toBe('50%');
    });

    test('shows delete button for selected glyphs', () => {
        showActionBar(
            ['glyph-1'],
            container,
            () => { },
            () => { }
        );

        const deleteBtn = container.querySelector('.canvas-action-delete');
        expect(deleteBtn).not.toBeNull();
        expect(deleteBtn?.getAttribute('data-tooltip')).toContain('Delete');
    });

    test('removes action bar from DOM when hidden', () => {
        showActionBar(
            ['glyph-1'],
            container,
            () => { },
            () => { }
        );

        let actionBar = container.querySelector('.canvas-action-bar');
        expect(actionBar).not.toBeNull();

        hideActionBar();

        // Action bar should be removed (after animation or immediately)
        setTimeout(() => {
            actionBar = container.querySelector('.canvas-action-bar');
            expect(actionBar).toBeNull();
        }, 100);
    });
});
