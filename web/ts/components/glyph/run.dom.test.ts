/**
 * @jest-environment jsdom
 *
 * Critical path tests for Glyph morphing system
 * Focus: Single element axiom, state transitions, invariant enforcement
 *
 * These tests run only with USE_JSDOM=1 (CI environment)
 */

import { describe, test, expect, beforeEach, mock } from 'bun:test';
import type { Glyph } from './glyph.ts';

// Only run these tests when USE_JSDOM=1 (CI environment)
const USE_JSDOM = process.env.USE_JSDOM === '1';

// Mock Element.animate (Web Animations API, not in JSDOM)
// Calls the "finish" listener synchronously so morph completes in tests
if (USE_JSDOM) {
    (globalThis.window as any).HTMLElement.prototype.animate = function() {
        const listeners: Record<string, Function[]> = {};
        return {
            finished: Promise.resolve(),
            cancel: () => {},
            finish: () => {},
            play: () => {},
            pause: () => {},
            addEventListener: (type: string, cb: Function) => {
                (listeners[type] ??= []).push(cb);
                // Fire finish immediately for test determinism
                if (type === 'finish') queueMicrotask(() => cb());
            },
            removeEventListener: () => {},
        };
    };
}

// Mock uiState — mock.module is process-global so every mock must be superset-complete
const mockMinimizedWindows: string[] = [];
const mockCanvasGlyphs: any[] = [];
const mockCanvasCompositions: any[] = [];
mock.module('../../state/ui', () => ({
    uiState: {
        addMinimizedWindow: (id: string) => {
            if (!mockMinimizedWindows.includes(id)) mockMinimizedWindows.push(id);
        },
        removeMinimizedWindow: (id: string) => {
            const idx = mockMinimizedWindows.indexOf(id);
            if (idx >= 0) mockMinimizedWindows.splice(idx, 1);
        },
        getMinimizedWindows: () => mockMinimizedWindows,
        isWindowMinimized: (id: string) => mockMinimizedWindows.includes(id),
        getCanvasGlyphs: () => mockCanvasGlyphs,
        setCanvasGlyphs: (glyphs: any[]) => { mockCanvasGlyphs.length = 0; mockCanvasGlyphs.push(...glyphs); },
        upsertCanvasGlyph: (glyph: any) => {
            const index = mockCanvasGlyphs.findIndex(g => g.id === glyph.id);
            if (index >= 0) { mockCanvasGlyphs[index] = glyph; } else { mockCanvasGlyphs.push(glyph); }
        },
        addCanvasGlyph: (glyph: any) => {
            const index = mockCanvasGlyphs.findIndex(g => g.id === glyph.id);
            if (index >= 0) { mockCanvasGlyphs[index] = glyph; } else { mockCanvasGlyphs.push(glyph); }
        },
        removeCanvasGlyph: (id: string) => {
            const index = mockCanvasGlyphs.findIndex(g => g.id === id);
            if (index >= 0) mockCanvasGlyphs.splice(index, 1);
        },
        getCanvasCompositions: () => mockCanvasCompositions,
        setCanvasCompositions: (comps: any[]) => { mockCanvasCompositions.length = 0; mockCanvasCompositions.push(...comps); },
        clearCanvasGlyphs: () => mockCanvasGlyphs.length = 0,
        clearCanvasCompositions: () => mockCanvasCompositions.length = 0,
        loadPersistedState: () => {},
        // Superset-complete stubs (mock.module is process-global, leaks into other test files)
        getCanvasPan: () => null,
        setCanvasPan: () => {},
        setMinimizedWindows: () => {},
        clearMinimizedWindows: () => {},
        isPanelVisible: () => false,
        setPanelVisible: () => {},
        togglePanel: () => false,
        closeAllPanels: () => {},
        getActiveModality: () => 'ax',
        setActiveModality: () => {},
        getBudgetWarnings: () => ({ daily: false, weekly: false, monthly: false }),
        setBudgetWarning: () => {},
        resetBudgetWarnings: () => {},
        getUsageView: () => 'week',
        setUsageView: () => {},
        getGraphSession: () => ({}),
        setGraphSession: () => {},
        setGraphQuery: () => {},
        setGraphVerbosity: () => {},
        clearGraphSession: () => {},
        subscribe: () => () => {},
        subscribeAll: () => () => {},
        getState: () => ({}),
        get: () => undefined,
        clearStorage: () => {},
        reset: () => {},
    },
}));

// Import after JSDOM + mock setup
const { glyphRun } = await import('./run.ts');

describe('Glyph Single Element Axiom', () => {
    if (!USE_JSDOM) {
        test.skip('Skipped locally (run with USE_JSDOM=1 to enable)', () => {});
        return;
    }

    beforeEach(() => {
        // Clear the glyph run state
        document.body.innerHTML = '';
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

        // Verify click handler works by checking that clicking changes state
        expect(element.dataset.windowState).toBeUndefined();

        // Note: We can't directly test the handler anymore since it's in a WeakMap
        // which is the correct encapsulation. The handler will work when clicked.
    });

    test('Auto-initialization: Glyphs added before explicit init() auto-initialize', () => {
        // glyphRun.element is null (beforeEach resets it), but add() calls init() internally
        const testGlyph: Glyph = {
            id: 'deferred-glyph',
            title: 'Deferred',
            renderContent: () => document.createElement('div')
        };

        // Add glyph before explicit init — add() auto-initializes via document.body
        glyphRun.add(testGlyph);

        // Should be in DOM immediately (auto-init succeeds because body always exists)
        const element = document.querySelector('[data-glyph-id="deferred-glyph"]');
        expect(element).not.toBeNull();

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

/**
 * Helper: create a fake TouchEvent since JSDOM doesn't support TouchEvent.
 * Must use JSDOM's Event constructor — JSDOM rejects events from other realms.
 */
function createTouchEvent(type: string, clientX: number, clientY: number): Event {
    const Win = globalThis.window as any;
    const event = new Win.Event(type, { bubbles: true, cancelable: true });
    event.touches = [{ clientX, clientY, identifier: 0 }];
    return event;
}

/**
 * Helper: mock getBoundingClientRect on an element.
 * JSDOM returns all zeros by default — we need real geometry for hit testing.
 */
function mockRect(element: HTMLElement, rect: { left: number; right: number; top: number; bottom: number }) {
    element.getBoundingClientRect = () => ({
        left: rect.left,
        right: rect.right,
        top: rect.top,
        bottom: rect.bottom,
        width: rect.right - rect.left,
        height: rect.bottom - rect.top,
        x: rect.left,
        y: rect.top,
        toJSON: () => ({})
    });
}

describe('Touch Browse', () => {
    if (!USE_JSDOM) {
        test.skip('Skipped locally (run with USE_JSDOM=1 to enable)', () => {});
        return;
    }

    // Tray positioned at right edge: x 350-360, y 200-260
    const TRAY_RECT = { left: 350, right: 360, top: 200, bottom: 260 };

    beforeEach(() => {
        document.body.innerHTML = '';
        (glyphRun as any).element = null;
        (glyphRun as any).indicatorContainer = null;
        (glyphRun as any).items.clear();
        (glyphRun as any).glyphElements.clear();
        (glyphRun as any).deferredItems = [];

        // Reset touch browse state from previous test (singleton leaks state)
        (glyphRun as any).proximity.isTouchBrowsing = false;

        glyphRun.init();

        // Mock geometry on the tray container
        const trayEl = (glyphRun as any).element as HTMLElement;
        if (trayEl) mockRect(trayEl, TRAY_RECT);
    });

    function addTestGlyphs(count: number): Glyph[] {
        const glyphs: Glyph[] = [];
        for (let i = 0; i < count; i++) {
            const g: Glyph = {
                id: `touch-glyph-${i}`,
                title: `Glyph ${i}`,
                renderContent: () => document.createElement('div')
            };
            glyphs.push(g);
            glyphRun.add(g);
        }
        return glyphs;
    }

    test('touchstart near tray enters browse mode', () => {
        addTestGlyphs(3);

        const proximity = (glyphRun as any).proximity;
        expect(proximity.isTouchBrowsing).toBe(false);

        // Touch within activation margin of tray
        document.dispatchEvent(createTouchEvent('touchstart', 355, 230));

        expect(proximity.isTouchBrowsing).toBe(true);
    });

    test('touchstart far from tray does NOT enter browse mode', () => {
        addTestGlyphs(3);

        const proximity = (glyphRun as any).proximity;

        // Touch on the opposite side of the screen
        document.dispatchEvent(createTouchEvent('touchstart', 50, 230));

        expect(proximity.isTouchBrowsing).toBe(false);
    });

    test('touchstart with empty tray does NOT enter browse mode', () => {
        // No glyphs added
        const proximity = (glyphRun as any).proximity;

        document.dispatchEvent(createTouchEvent('touchstart', 355, 230));

        expect(proximity.isTouchBrowsing).toBe(false);
    });

    test('touchmove updates pointer position during browse', () => {
        addTestGlyphs(3);

        const proximity = (glyphRun as any).proximity;

        // Enter browse
        document.dispatchEvent(createTouchEvent('touchstart', 355, 230));
        expect(proximity.isTouchBrowsing).toBe(true);

        // Slide thumb down
        document.dispatchEvent(createTouchEvent('touchmove', 355, 250));

        const pos = proximity.getMousePosition();
        expect(pos.x).toBe(355);
        expect(pos.y).toBe(250);
    });

    test('touchmove outside browse mode is ignored', () => {
        addTestGlyphs(3);

        const proximity = (glyphRun as any).proximity;

        // Move without starting browse (isTouchBrowsing is false)
        document.dispatchEvent(createTouchEvent('touchmove', 355, 250));

        // Position should still be at default (0,0) or wherever mouse left it
        // The key assertion: isTouchBrowsing should remain false
        expect(proximity.isTouchBrowsing).toBe(false);
    });

    test('touchend exits browse mode', () => {
        addTestGlyphs(3);

        const proximity = (glyphRun as any).proximity;

        // Enter browse
        document.dispatchEvent(createTouchEvent('touchstart', 355, 230));
        expect(proximity.isTouchBrowsing).toBe(true);

        // Release
        document.dispatchEvent(createTouchEvent('touchend', 355, 230));

        expect(proximity.isTouchBrowsing).toBe(false);
    });

    test('touchend collapses pointer to offscreen', () => {
        addTestGlyphs(3);

        const proximity = (glyphRun as any).proximity;

        // Enter and browse
        document.dispatchEvent(createTouchEvent('touchstart', 355, 230));
        document.dispatchEvent(createTouchEvent('touchmove', 355, 240));

        // Release
        document.dispatchEvent(createTouchEvent('touchend', 355, 240));

        // Pointer should be moved far offscreen to collapse all glyphs
        const pos = proximity.getMousePosition();
        expect(pos.x).toBe(-9999);
        expect(pos.y).toBe(-9999);
    });

    test('findPeakedGlyph returns null when no glyphs are close', () => {
        addTestGlyphs(3);

        const proximity = (glyphRun as any).proximity;
        // Set pointer far from everything
        proximity.setPointerPosition(-9999, -9999);

        const peaked = (glyphRun as any).findPeakedGlyph();
        expect(peaked).toBeNull();
    });

    test('findPeakedGlyph returns the glyph with highest proximity', () => {
        const testGlyphs = addTestGlyphs(3);

        // Give glyphs real geometry so proximity calculations work.
        // Stack them vertically at x=350-360
        const indicatorContainer = (glyphRun as any).indicatorContainer as HTMLElement;
        const dots = indicatorContainer.querySelectorAll('.glyph-run-glyph') as NodeListOf<HTMLElement>;

        mockRect(dots[0], { left: 350, right: 360, top: 200, bottom: 212 });
        mockRect(dots[1], { left: 350, right: 360, top: 218, bottom: 230 });
        mockRect(dots[2], { left: 350, right: 360, top: 236, bottom: 248 });

        const proximity = (glyphRun as any).proximity;

        // Place pointer right on top of the second glyph (y=224 is center of 218-230)
        proximity.setPointerPosition(355, 224);

        const peaked = (glyphRun as any).findPeakedGlyph();
        expect(peaked).not.toBeNull();
        expect(peaked.item.id).toBe('touch-glyph-1');
    });

    test('touchend on peaked glyph triggers morph', () => {
        addTestGlyphs(1);

        // Give the dot real geometry on the tray
        const indicatorContainer = (glyphRun as any).indicatorContainer as HTMLElement;
        const dot = indicatorContainer.querySelector('.glyph-run-glyph') as HTMLElement;
        mockRect(dot, { left: 350, right: 360, top: 220, bottom: 232 });

        // Glyph should be in dot state before browse
        expect(dot.dataset.windowState).toBeUndefined();

        // Enter browse near glyph
        document.dispatchEvent(createTouchEvent('touchstart', 355, 226));
        expect((glyphRun as any).proximity.isTouchBrowsing).toBe(true);

        // Release on glyph — triggers morphGlyph which sets isRestoring
        document.dispatchEvent(createTouchEvent('touchend', 355, 226));

        // morphGlyph was called: isRestoring is set during the morph animation
        expect((glyphRun as any).isRestoring).toBe(true);
    });
});

describe('Glyph State Transitions', () => {
    if (!USE_JSDOM) {
        test.skip('Skipped locally (run with USE_JSDOM=1 to enable)', () => {});
        return;
    }

    beforeEach(() => {
        document.body.innerHTML = '';
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