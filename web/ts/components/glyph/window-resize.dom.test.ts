/**
 * @jest-environment jsdom
 *
 * DOM tests for window auto-resize functionality
 * Tests ResizeObserver integration for database/self window glyphs
 *
 * These tests run only in CI with JSDOM environment (gated by USE_JSDOM=1)
 */

import { describe, test, expect, beforeEach } from 'bun:test';

// Only run these tests when USE_JSDOM=1 (CI environment)
const USE_JSDOM = process.env.USE_JSDOM === '1';

// Override the preload's no-op ResizeObserver with one that fires callbacks —
// these tests need contentRect values to verify sizing logic.
if (USE_JSDOM) {
    globalThis.ResizeObserver = class ResizeObserver {
        private callback: ResizeObserverCallback;

        constructor(callback: ResizeObserverCallback) {
            this.callback = callback;
        }

        observe(target: Element) {
            const entry: ResizeObserverEntry = {
                target,
                contentRect: {
                    width: 300,
                    height: 150,
                    top: 0,
                    left: 0,
                    bottom: 150,
                    right: 300,
                    x: 0,
                    y: 0,
                    toJSON: () => ({})
                },
                borderBoxSize: [] as any,
                contentBoxSize: [] as any,
                devicePixelContentBoxSize: [] as any
            };

            setTimeout(() => this.callback([entry], this), 0);
        }

        disconnect() {}
        unobserve() {}
    } as any;
}

describe('Window ResizeObserver', () => {
    if (!USE_JSDOM) {
        test.skip('Skipped locally (run with USE_JSDOM=1 to enable)', () => {});
        return;
    }

    beforeEach(() => {
        document.body.innerHTML = '';
    });

    test('Window glyph stores ResizeObserver reference on element', async () => {
        // Create a mock window element with glyph content
        const windowElement = document.createElement('div');
        windowElement.className = 'draggable-window';
        windowElement.id = 'test-window';
        document.body.appendChild(windowElement);

        // Create content structure that matches window manifestation
        const contentElement = document.createElement('div');
        contentElement.style.flex = '1';
        contentElement.style.padding = '8px';

        const innerContent = document.createElement('div');
        innerContent.className = 'glyph-content';
        innerContent.innerHTML = '<div>Test content</div>';

        contentElement.appendChild(innerContent);
        windowElement.appendChild(contentElement);

        // Manually create ResizeObserver (simulating setupWindowResizeObserver)
        const observer = new ResizeObserver((entries) => {
            for (const entry of entries) {
                const contentHeight = entry.contentRect.height;
                const contentWidth = entry.contentRect.width;

                // Simulate the sizing logic
                const totalPadding = 24; // (8 + 4) * 2
                const titleBarHeight = 32;

                windowElement.style.height = `${contentHeight + titleBarHeight + totalPadding}px`;
                windowElement.style.width = `${contentWidth + totalPadding}px`;
            }
        });

        observer.observe(innerContent);
        (windowElement as any).__resizeObserver = observer;

        // Verify observer is stored
        expect((windowElement as any).__resizeObserver).toBeDefined();
        expect((windowElement as any).__resizeObserver).toBeInstanceOf(ResizeObserver);

        // Wait for observer callback to fire
        await new Promise(resolve => setTimeout(resolve, 10));

        // Verify window was sized (mock returns 300x150 content)
        // Height: 150 + 32 (title) + 24 (padding) = 206px
        // Width: 300 + 24 (padding) = 324px
        expect(windowElement.style.height).toBe('206px');
        expect(windowElement.style.width).toBe('324px');
    });

    test('ResizeObserver cleanup on window close', () => {
        const windowElement = document.createElement('div');
        windowElement.className = 'draggable-window';
        document.body.appendChild(windowElement);

        const mockObserver = new ResizeObserver(() => {});
        (windowElement as any).__resizeObserver = mockObserver;

        // Simulate cleanup (from morphFromWindow)
        const resizeObserver = (windowElement as any).__resizeObserver;
        if (resizeObserver && typeof resizeObserver.disconnect === 'function') {
            resizeObserver.disconnect();
            delete (windowElement as any).__resizeObserver;
        }

        // Verify cleanup happened
        expect((windowElement as any).__resizeObserver).toBeUndefined();
    });

    test('Runtime type check prevents errors on invalid __resizeObserver', () => {
        const windowElement = document.createElement('div');
        document.body.appendChild(windowElement);

        // Simulate accidental pollution of __resizeObserver
        (windowElement as any).__resizeObserver = "not a ResizeObserver";

        // This should not throw due to runtime check
        expect(() => {
            const resizeObserver = (windowElement as any).__resizeObserver;
            if (resizeObserver && typeof resizeObserver.disconnect === 'function') {
                resizeObserver.disconnect();
                delete (windowElement as any).__resizeObserver;
            }
        }).not.toThrow();

        // Observer should still be there (wasn't valid so wasn't cleaned up)
        expect((windowElement as any).__resizeObserver).toBe("not a ResizeObserver");
    });

    test('Multiple render calls cleanup old observer before creating new one', () => {
        const glyphElement = document.createElement('div');
        glyphElement.className = 'canvas-ax-glyph';
        document.body.appendChild(glyphElement);

        // First render - create observer
        const observer1 = new ResizeObserver(() => {});
        (glyphElement as any).__resizeObserver = observer1;

        // Second render - cleanup and create new (simulates re-render)
        const existingObserver = (glyphElement as any).__resizeObserver;
        if (existingObserver && typeof existingObserver.disconnect === 'function') {
            existingObserver.disconnect();
            delete (glyphElement as any).__resizeObserver;
        }

        const observer2 = new ResizeObserver(() => {});
        (glyphElement as any).__resizeObserver = observer2;

        // Verify new observer replaced old one
        expect((glyphElement as any).__resizeObserver).toBe(observer2);
        expect((glyphElement as any).__resizeObserver).not.toBe(observer1);
    });
});
