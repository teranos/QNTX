/**
 * @jest-environment jsdom
 *
 * Critical path tests for Glyph morphing system
 * Focus: Single element axiom, state transitions, invariant enforcement
 *
 * These tests run only with USE_JSDOM=1 (CI environment)
 */

import { describe, test, expect, beforeEach } from 'bun:test';
import { glyphRun } from './glyph-run.ts';
import type { Glyph } from './glyph-morph.ts';

// Only run these tests when USE_JSDOM=1 (CI environment)
const USE_JSDOM = process.env.USE_JSDOM === '1';

// Setup jsdom if enabled
if (USE_JSDOM) {
    const { JSDOM } = await import('jsdom');
    const dom = new JSDOM('<!DOCTYPE html><html><body><div id="graph-container"></div></body></html>');
    const { window } = dom;
    const { document } = window;

    // Replace global document/window with jsdom's
    globalThis.document = document as any;
    globalThis.window = window as any;
    globalThis.navigator = window.navigator as any;
    globalThis.DOMParser = window.DOMParser as any;
}

describe('Glyph Single Element Axiom', () => {
    if (!USE_JSDOM) {
        test.skip('Skipped locally (run with USE_JSDOM=1 to enable)', () => {});
        return;
    }

    beforeEach(() => {
        // Clear the glyph run state
        document.body.innerHTML = '<div id="graph-container"></div>';
        // Reset the singleton (this is a bit hacky but needed for testing)
        (glyphRun as any).element = null;
        (glyphRun as any).indicatorContainer = null;
        (glyphRun as any).items.clear();
        (glyphRun as any).glyphElements.clear();
        (glyphRun as any).deferredItems = [];
    });

    test('Single element axiom: Each glyph is exactly ONE DOM element', () => {
        glyphRun.init();

        const testGlyph: Glyph = {
            id: 'test-glyph-1',
            title: 'Test Glyph',
            renderContent: () => {
                const content = document.createElement('div');
                content.textContent = 'Test Content';
                return content;
            }
        };

        // Add glyph
        glyphRun.add(testGlyph);

        // Verify exactly one element exists
        const elements = document.querySelectorAll('[data-glyph-id="test-glyph-1"]');
        expect(elements.length).toBe(1);

        // Verify it's tracked
        expect(glyphRun.has('test-glyph-1')).toBe(true);

        // Attempting to add the same glyph again should be a no-op
        glyphRun.add(testGlyph);
        const elementsAfter = document.querySelectorAll('[data-glyph-id="test-glyph-1"]');
        expect(elementsAfter.length).toBe(1); // Still exactly one

        // The invariant should pass
        expect(() => glyphRun.verifyInvariant()).not.toThrow();
    });

    test('Axiom violation: Creating duplicate elements throws error', () => {
        glyphRun.init();

        const testGlyph: Glyph = {
            id: 'test-glyph-2',
            title: 'Test Glyph 2',
            renderContent: () => document.createElement('div')
        };

        // Add glyph properly
        glyphRun.add(testGlyph);

        // Manually create a duplicate element (violating axiom)
        const duplicate = document.createElement('div');
        duplicate.setAttribute('data-glyph-id', 'test-glyph-2');
        document.body.appendChild(duplicate);

        // Verify invariant catches this violation
        expect(() => glyphRun.verifyInvariant()).toThrow(/INVARIANT VIOLATION.*2 elements/);
    });

    test('Axiom violation: Untracked elements are detected', () => {
        glyphRun.init();

        // Create an element outside the factory (violating axiom)
        const rogue = document.createElement('div');
        rogue.setAttribute('data-glyph-id', 'rogue-glyph');
        document.body.appendChild(rogue);

        // Verify invariant catches this violation
        expect(() => glyphRun.verifyInvariant()).toThrow(/INVARIANT VIOLATION.*not tracked/);
    });

    test('Element persistence: Same element through add/remove from tray', () => {
        glyphRun.init();

        const testGlyph: Glyph = {
            id: 'test-glyph-3',
            title: 'Test Glyph 3',
            renderContent: () => document.createElement('div')
        };

        // Add glyph
        glyphRun.add(testGlyph);
        const element = document.querySelector('[data-glyph-id="test-glyph-3"]');
        expect(element).not.toBeNull();

        // Store a reference to verify it's the same element later
        const elementRef = element;

        // The element should be in the indicator container
        const indicatorContainer = document.querySelector('.glyph-run-indicators');
        expect(indicatorContainer?.contains(element!)).toBe(true);

        // Remove the glyph
        glyphRun.remove('test-glyph-3');

        // Element should be removed from DOM
        const removedElement = document.querySelector('[data-glyph-id="test-glyph-3"]');
        expect(removedElement).toBeNull();

        // Adding again would create a new element (since we removed it)
        glyphRun.add(testGlyph);
        const newElement = document.querySelector('[data-glyph-id="test-glyph-3"]');
        expect(newElement).not.toBeNull();

        // Note: After removal, a new element is created - this is allowed
        // The axiom is about no duplicates existing simultaneously
    });

    test('Click handler persists with element', () => {
        glyphRun.init();

        const testGlyph: Glyph = {
            id: 'test-glyph-4',
            title: 'Test Glyph 4',
            renderContent: () => {
                const content = document.createElement('div');
                content.textContent = 'Content';
                return content;
            }
        };

        glyphRun.add(testGlyph);
        const element = document.querySelector('[data-glyph-id="test-glyph-4"]') as HTMLElement;
        expect(element).not.toBeNull();

        // Verify click handler is attached
        const handler = (element as any).__glyphClickHandler;
        expect(handler).toBeDefined();
        expect(typeof handler).toBe('function');
    });

    test('Deferred initialization: Glyphs added before DOM ready are handled', () => {
        // Remove graph-container to simulate DOM not ready
        const graphContainer = document.getElementById('graph-container');
        graphContainer?.remove();

        const testGlyph: Glyph = {
            id: 'deferred-glyph',
            title: 'Deferred',
            renderContent: () => document.createElement('div')
        };

        // Add glyph when DOM not ready (should be deferred)
        glyphRun.add(testGlyph);

        // Should be deferred, not in DOM yet
        const elementBefore = document.querySelector('[data-glyph-id="deferred-glyph"]');
        expect(elementBefore).toBeNull();

        // Recreate graph container
        const newContainer = document.createElement('div');
        newContainer.id = 'graph-container';
        document.body.appendChild(newContainer);

        // Now init with graph container present
        glyphRun.init();

        // Deferred glyph should now be in DOM
        const elementAfter = document.querySelector('[data-glyph-id="deferred-glyph"]');
        expect(elementAfter).not.toBeNull();

        // Verify invariant holds
        expect(() => glyphRun.verifyInvariant()).not.toThrow();
    });

    test('Element tracking: Tracked elements match DOM elements', () => {
        glyphRun.init();

        // Add multiple glyphs
        const glyphs: Glyph[] = [
            {
                id: 'track-1',
                title: 'Track 1',
                renderContent: () => document.createElement('div')
            },
            {
                id: 'track-2',
                title: 'Track 2',
                renderContent: () => document.createElement('div')
            },
            {
                id: 'track-3',
                title: 'Track 3',
                renderContent: () => document.createElement('div')
            }
        ];

        glyphs.forEach(g => glyphRun.add(g));

        // All should be tracked
        expect(glyphRun.has('track-1')).toBe(true);
        expect(glyphRun.has('track-2')).toBe(true);
        expect(glyphRun.has('track-3')).toBe(true);
        expect(glyphRun.count).toBe(3);

        // All should be in DOM
        const elements = document.querySelectorAll('[data-glyph-id]');
        expect(elements.length).toBe(3);

        // Invariant should pass
        expect(() => glyphRun.verifyInvariant()).not.toThrow();

        // Remove one
        glyphRun.remove('track-2');
        expect(glyphRun.count).toBe(2);
        const remainingElements = document.querySelectorAll('[data-glyph-id]');
        expect(remainingElements.length).toBe(2);

        // Invariant should still pass
        expect(() => glyphRun.verifyInvariant()).not.toThrow();
    });
});

describe('Glyph State Transitions', () => {
    if (!USE_JSDOM) {
        test.skip('Skipped locally (run with USE_JSDOM=1 to enable)', () => {});
        return;
    }

    beforeEach(() => {
        document.body.innerHTML = '<div id="graph-container"></div>';
        (glyphRun as any).element = null;
        (glyphRun as any).indicatorContainer = null;
        (glyphRun as any).items.clear();
        (glyphRun as any).glyphElements.clear();
        (glyphRun as any).deferredItems = [];
    });

    test('Glyph starts in dot state', () => {
        glyphRun.init();

        const testGlyph: Glyph = {
            id: 'state-test-1',
            title: 'State Test',
            renderContent: () => document.createElement('div')
        };

        glyphRun.add(testGlyph);
        const element = document.querySelector('[data-glyph-id="state-test-1"]') as HTMLElement;

        // Should have glyph class, not window state
        expect(element.className).toBe('glyph-run-glyph');
        expect(element.dataset.windowState).toBeUndefined();
        expect(element.dataset.hasText).toBeUndefined();
    });

    test('Window state flag is set/cleared correctly', () => {
        glyphRun.init();

        const testGlyph: Glyph = {
            id: 'state-test-2',
            title: 'Window State Test',
            renderContent: () => document.createElement('div')
        };

        glyphRun.add(testGlyph);
        const element = document.querySelector('[data-glyph-id="state-test-2"]') as HTMLElement;

        // Initially no window state
        expect(element.dataset.windowState).toBeUndefined();

        // Simulate setting window state (what morphToWindow does)
        element.dataset.windowState = 'true';
        expect(element.dataset.windowState).toBe('true');

        // Simulate clearing window state (what morphToGlyph does)
        delete element.dataset.windowState;
        expect(element.dataset.windowState).toBeUndefined();
    });
});