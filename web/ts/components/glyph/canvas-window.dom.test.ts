/**
 * @jest-environment jsdom
 *
 * Critical path tests for canvas glyph ↔ window morphing (#440)
 *
 * Tim (happy path):
 * - Maximize: canvas glyph pops out to floating window, children preserved
 * - Minimize: window morphs back to canvas position, children unwrapped
 */

import { describe, test, expect, beforeEach } from 'bun:test';

const USE_JSDOM = process.env.USE_JSDOM === '1';

// Mock Element.animate — fires finish synchronously via microtask
if (USE_JSDOM) {
    (globalThis.window as any).HTMLElement.prototype.animate = function () {
        const listeners: Record<string, Function[]> = {};
        return {
            finished: Promise.resolve(),
            cancel: () => {},
            finish: () => {},
            play: () => {},
            pause: () => {},
            addEventListener: (type: string, cb: Function) => {
                (listeners[type] ??= []).push(cb);
                if (type === 'finish') queueMicrotask(() => cb());
            },
            removeEventListener: () => {},
        };
    };
}

// Mock uiState (process-global, superset-complete)
const mockCanvasGlyphs: any[] = [];
const mockCanvasCompositions: any[] = [];
const mockCanvasPan: Record<string, any> = {};
import { mock } from 'bun:test';
mock.module('../../state/ui', () => ({
    uiState: {
        getCanvasGlyphs: () => mockCanvasGlyphs,
        setCanvasGlyphs: (g: any[]) => { mockCanvasGlyphs.length = 0; mockCanvasGlyphs.push(...g); },
        addCanvasGlyph: (g: any) => {
            const i = mockCanvasGlyphs.findIndex(x => x.id === g.id);
            if (i >= 0) mockCanvasGlyphs[i] = g; else mockCanvasGlyphs.push(g);
        },
        removeCanvasGlyph: (id: string) => {
            const i = mockCanvasGlyphs.findIndex(g => g.id === id);
            if (i >= 0) mockCanvasGlyphs.splice(i, 1);
        },
        upsertCanvasGlyph: (g: any) => {
            const i = mockCanvasGlyphs.findIndex(x => x.id === g.id);
            if (i >= 0) mockCanvasGlyphs[i] = g; else mockCanvasGlyphs.push(g);
        },
        clearCanvasGlyphs: () => mockCanvasGlyphs.length = 0,
        getCanvasCompositions: () => mockCanvasCompositions,
        setCanvasCompositions: (c: any[]) => { mockCanvasCompositions.length = 0; mockCanvasCompositions.push(...c); },
        clearCanvasCompositions: () => mockCanvasCompositions.length = 0,
        addMinimizedWindow: () => {},
        removeMinimizedWindow: () => {},
        getMinimizedWindows: () => [],
        isWindowMinimized: () => false,
        loadPersistedState: () => {},
        getCanvasPan: (id: string) => mockCanvasPan[id] ?? null,
        setCanvasPan: (id: string, pan: any) => { mockCanvasPan[id] = pan; },
        // Superset-complete stubs (mock.module is process-global, leaks into other test files)
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

// Import after mocks
const { morphCanvasPlacedToWindow } = await import('./manifestations/canvas-window');
const { setWindowState, isInWindowState, getCanvasOrigin, getLastPosition } = await import('./dataset');
const { resetCanvasState } = await import('./canvas/canvas-pan');

/** Mock getBoundingClientRect on an element */
function mockRect(el: HTMLElement, rect: { left: number; top: number; width: number; height: number }) {
    el.getBoundingClientRect = () => ({
        left: rect.left,
        top: rect.top,
        right: rect.left + rect.width,
        bottom: rect.top + rect.height,
        width: rect.width,
        height: rect.height,
        x: rect.left,
        y: rect.top,
        toJSON: () => ({}),
    });
}

/** Build a minimal canvas workspace with a glyph element inside */
function buildCanvas(): { canvas: HTMLElement; contentLayer: HTMLElement; glyph: HTMLElement } {
    const canvas = document.createElement('div');
    canvas.className = 'canvas-workspace';
    canvas.dataset.canvasId = 'test-canvas';
    document.body.appendChild(canvas);

    const contentLayer = document.createElement('div');
    contentLayer.className = 'canvas-content-layer';
    canvas.appendChild(contentLayer);

    // Create a canvas-placed glyph with children (simulating a result glyph)
    const glyph = document.createElement('div');
    glyph.className = 'canvas-result-glyph canvas-glyph';
    glyph.dataset.glyphId = 'result-42';
    glyph.style.position = 'absolute';
    glyph.style.left = '100px';
    glyph.style.top = '200px';
    glyph.style.width = '400px';
    glyph.style.height = '250px';

    // Children: header + output (simulating real result glyph DOM)
    const header = document.createElement('div');
    header.className = 'result-glyph-header';
    header.textContent = 'prompt text';
    glyph.appendChild(header);

    const output = document.createElement('div');
    output.className = 'result-glyph-output';
    output.textContent = 'Hello World';
    glyph.appendChild(output);

    contentLayer.appendChild(glyph);

    // Mock geometry: canvas at viewport (0, 48), glyph at (100, 248) on screen
    mockRect(canvas, { left: 0, top: 48, width: 1200, height: 700 });
    mockRect(glyph, { left: 100, top: 248, width: 400, height: 250 });

    // Mock offsetLeft/offsetTop (JSDOM returns 0 by default)
    Object.defineProperty(glyph, 'offsetLeft', { value: 100, configurable: true });
    Object.defineProperty(glyph, 'offsetTop', { value: 200, configurable: true });
    Object.defineProperty(glyph, 'offsetWidth', { value: 400, configurable: true });
    Object.defineProperty(glyph, 'offsetHeight', { value: 250, configurable: true });

    return { canvas, contentLayer, glyph };
}

describe('Canvas → Window Morph', () => {
    if (!USE_JSDOM) {
        test.skip('Skipped locally (run with USE_JSDOM=1 to enable)', () => {});
        return;
    }

    beforeEach(() => {
        document.body.innerHTML = '';
        resetCanvasState('test-canvas');
        mockCanvasGlyphs.length = 0;
    });

    // ── Tim: maximize happy path ────────────────────────────────────

    test('maximize: element moves to document.body as fixed', async () => {
        const { canvas, glyph } = buildCanvas();
        let restoreCalled = false;

        morphCanvasPlacedToWindow(glyph, {
            title: 'Test Result',
            canvasId: 'test-canvas',
            onRestoreComplete: () => { restoreCalled = true; },
        });

        // Let morph animation finish (microtask)
        await new Promise(r => queueMicrotask(r));
        await new Promise(r => queueMicrotask(r));

        // Element should now be on document.body, not inside canvas
        expect(canvas.contains(glyph)).toBe(false);
        expect(document.body.contains(glyph)).toBe(true);
        expect(glyph.style.position).toBe('fixed');
    });

    test('maximize: window state flag is set', async () => {
        const { glyph } = buildCanvas();

        morphCanvasPlacedToWindow(glyph, {
            title: 'Test',
            canvasId: 'test-canvas',
            onRestoreComplete: () => {},
        });

        await new Promise(r => queueMicrotask(r));

        expect(isInWindowState(glyph)).toBe(true);
    });

    test('maximize: canvas origin is stored for return trip', async () => {
        const { glyph } = buildCanvas();

        morphCanvasPlacedToWindow(glyph, {
            title: 'Test',
            canvasId: 'test-canvas',
            onRestoreComplete: () => {},
        });

        // Canvas origin is set synchronously before animation
        const origin = getCanvasOrigin(glyph);
        expect(origin).not.toBeNull();
        expect(origin!.x).toBe(100);
        expect(origin!.y).toBe(200);
        expect(origin!.width).toBe(400);
        expect(origin!.height).toBe(250);
        expect(origin!.canvasId).toBe('test-canvas');
    });

    test('maximize: existing children are wrapped, not destroyed', async () => {
        const { glyph } = buildCanvas();

        morphCanvasPlacedToWindow(glyph, {
            title: 'Test',
            canvasId: 'test-canvas',
            onRestoreComplete: () => {},
        });

        await new Promise(r => queueMicrotask(r));
        await new Promise(r => queueMicrotask(r));

        // Children should be inside a .canvas-window-content wrapper
        const contentDiv = glyph.querySelector('.canvas-window-content');
        expect(contentDiv).not.toBeNull();
        expect(contentDiv!.querySelector('.result-glyph-header')).not.toBeNull();
        expect(contentDiv!.querySelector('.result-glyph-output')).not.toBeNull();
        // Output text preserved
        expect(contentDiv!.textContent).toContain('Hello World');
    });

    test('maximize: window title bar is prepended', async () => {
        const { glyph } = buildCanvas();

        morphCanvasPlacedToWindow(glyph, {
            title: 'My Result',
            canvasId: 'test-canvas',
            onRestoreComplete: () => {},
        });

        await new Promise(r => queueMicrotask(r));
        await new Promise(r => queueMicrotask(r));

        const titleBar = glyph.querySelector('.window-title-bar');
        expect(titleBar).not.toBeNull();
        expect(titleBar!.querySelector('span')!.textContent).toBe('My Result');

        // Has minimize button
        const buttons = titleBar!.querySelectorAll('button');
        expect(buttons.length).toBeGreaterThanOrEqual(1);
        expect(buttons[0].textContent).toBe('−');
    });

    test('maximize: double-call is a no-op', async () => {
        const { glyph } = buildCanvas();
        const config = {
            title: 'Test',
            canvasId: 'test-canvas',
            onRestoreComplete: () => {},
        };

        morphCanvasPlacedToWindow(glyph, config);
        await new Promise(r => queueMicrotask(r));
        await new Promise(r => queueMicrotask(r));

        // Second call should be a no-op (isInWindowState guard)
        const bodyChildCount = document.body.children.length;
        morphCanvasPlacedToWindow(glyph, config);
        expect(document.body.children.length).toBe(bodyChildCount);
    });

    // ── Tim: minimize happy path (TDD — tests describe correct behavior) ─

    test('minimize: element returns to original canvas parent', async () => {
        const { canvas, contentLayer, glyph } = buildCanvas();
        let restoreCalled = false;

        morphCanvasPlacedToWindow(glyph, {
            title: 'Test',
            canvasId: 'test-canvas',
            onRestoreComplete: () => { restoreCalled = true; },
        });

        // Wait for maximize to commit
        await new Promise(r => queueMicrotask(r));
        await new Promise(r => queueMicrotask(r));

        expect(isInWindowState(glyph)).toBe(true);
        // Glyph should be detached from content layer
        expect(contentLayer.contains(glyph)).toBe(false);

        // Mock window rect for the minimize animation source
        mockRect(glyph, { left: 300, top: 100, width: 520, height: 420 });

        // Click minimize
        const minimizeBtn = glyph.querySelector('.window-title-bar button') as HTMLElement;
        expect(minimizeBtn).not.toBeNull();
        minimizeBtn.click();

        // Wait for minimize animation to commit
        await new Promise(r => queueMicrotask(r));
        await new Promise(r => queueMicrotask(r));

        expect(restoreCalled).toBe(true);

        // Element should be back in the content layer (its original parent)
        expect(contentLayer.contains(glyph)).toBe(true);
    });

    test('minimize: window state is cleared', async () => {
        const { contentLayer, glyph } = buildCanvas();

        morphCanvasPlacedToWindow(glyph, {
            title: 'Test',
            canvasId: 'test-canvas',
            onRestoreComplete: () => {},
        });

        await new Promise(r => queueMicrotask(r));
        await new Promise(r => queueMicrotask(r));

        mockRect(glyph, { left: 300, top: 100, width: 520, height: 420 });
        const btn = glyph.querySelector('.window-title-bar button') as HTMLElement;
        btn.click();

        await new Promise(r => queueMicrotask(r));
        await new Promise(r => queueMicrotask(r));

        expect(isInWindowState(glyph)).toBe(false);
    });

    test('minimize: children are unwrapped from content div', async () => {
        const { contentLayer, glyph } = buildCanvas();

        morphCanvasPlacedToWindow(glyph, {
            title: 'Test',
            canvasId: 'test-canvas',
            onRestoreComplete: () => {},
        });

        await new Promise(r => queueMicrotask(r));
        await new Promise(r => queueMicrotask(r));

        mockRect(glyph, { left: 300, top: 100, width: 520, height: 420 });
        const btn = glyph.querySelector('.window-title-bar button') as HTMLElement;
        btn.click();

        await new Promise(r => queueMicrotask(r));
        await new Promise(r => queueMicrotask(r));

        // Content div and title bar should be removed
        expect(glyph.querySelector('.canvas-window-content')).toBeNull();
        expect(glyph.querySelector('.window-title-bar')).toBeNull();

        // Original children should be direct children again
        expect(glyph.querySelector('.result-glyph-header')).not.toBeNull();
        expect(glyph.querySelector('.result-glyph-output')).not.toBeNull();
        expect(glyph.textContent).toContain('Hello World');
    });

    test('minimize: canvas origin is cleared', async () => {
        const { contentLayer, glyph } = buildCanvas();

        morphCanvasPlacedToWindow(glyph, {
            title: 'Test',
            canvasId: 'test-canvas',
            onRestoreComplete: () => {},
        });

        await new Promise(r => queueMicrotask(r));
        await new Promise(r => queueMicrotask(r));

        // Origin should exist while in window state
        expect(getCanvasOrigin(glyph)).not.toBeNull();

        mockRect(glyph, { left: 300, top: 100, width: 520, height: 420 });
        const btn = glyph.querySelector('.window-title-bar button') as HTMLElement;
        btn.click();

        await new Promise(r => queueMicrotask(r));
        await new Promise(r => queueMicrotask(r));

        expect(getCanvasOrigin(glyph)).toBeNull();
    });

    test('minimize: canvas-local position is restored on element', async () => {
        const { contentLayer, glyph } = buildCanvas();

        morphCanvasPlacedToWindow(glyph, {
            title: 'Test',
            canvasId: 'test-canvas',
            onRestoreComplete: () => {},
        });

        await new Promise(r => queueMicrotask(r));
        await new Promise(r => queueMicrotask(r));

        mockRect(glyph, { left: 300, top: 100, width: 520, height: 420 });
        const btn = glyph.querySelector('.window-title-bar button') as HTMLElement;
        btn.click();

        await new Promise(r => queueMicrotask(r));
        await new Promise(r => queueMicrotask(r));

        // Element should have its original canvas-local coords restored
        expect(glyph.style.position).toBe('absolute');
        expect(glyph.style.left).toBe('100px');
        expect(glyph.style.top).toBe('200px');
        expect(glyph.style.width).toBe('400px');
        expect(glyph.style.height).toBe('250px');
    });

    test('minimize: window position is remembered for next expand', async () => {
        const { contentLayer, glyph } = buildCanvas();

        morphCanvasPlacedToWindow(glyph, {
            title: 'Test',
            canvasId: 'test-canvas',
            onRestoreComplete: () => {},
        });

        await new Promise(r => queueMicrotask(r));
        await new Promise(r => queueMicrotask(r));

        // Simulate window at a custom position
        mockRect(glyph, { left: 450, top: 200, width: 520, height: 420 });
        const btn = glyph.querySelector('.window-title-bar button') as HTMLElement;
        btn.click();

        await new Promise(r => queueMicrotask(r));
        await new Promise(r => queueMicrotask(r));

        // Last window position should be saved
        const lastPos = getLastPosition(glyph);
        expect(lastPos).not.toBeNull();
        expect(lastPos!.x).toBe(450);
        expect(lastPos!.y).toBe(200);
    });

    test('minimize: restore animation targets viewport coords (not canvas-relative)', async () => {
        // Canvas at viewport (50, 80) — NOT at (0,0)
        const canvas = document.createElement('div');
        canvas.className = 'canvas-workspace';
        canvas.dataset.canvasId = 'offset-canvas';
        document.body.appendChild(canvas);

        const contentLayer = document.createElement('div');
        contentLayer.className = 'canvas-content-layer';
        canvas.appendChild(contentLayer);

        const glyph = document.createElement('div');
        glyph.className = 'canvas-result-glyph canvas-glyph';
        glyph.dataset.glyphId = 'result-offset';
        const child = document.createElement('div');
        child.className = 'test-child';
        glyph.appendChild(child);
        contentLayer.appendChild(glyph);

        mockRect(canvas, { left: 50, top: 80, width: 1000, height: 600 });
        mockRect(glyph, { left: 250, top: 380, width: 400, height: 200 });
        Object.defineProperty(glyph, 'offsetLeft', { value: 200, configurable: true });
        Object.defineProperty(glyph, 'offsetTop', { value: 300, configurable: true });
        Object.defineProperty(glyph, 'offsetWidth', { value: 400, configurable: true });
        Object.defineProperty(glyph, 'offsetHeight', { value: 200, configurable: true });

        // Track the animation target rect
        let animToRect: any = null;
        const origAnimate = (globalThis.window as any).HTMLElement.prototype.animate;
        (globalThis.window as any).HTMLElement.prototype.animate = function (keyframes: any[], opts: any) {
            // Capture the "to" keyframe for the Restore animation
            if (Array.isArray(keyframes) && keyframes.length === 2) {
                animToRect = keyframes[1];
            }
            return origAnimate.call(this, keyframes, opts);
        };

        morphCanvasPlacedToWindow(glyph, {
            title: 'Test',
            canvasId: 'offset-canvas',
            onRestoreComplete: () => {},
        });

        await new Promise(r => queueMicrotask(r));
        await new Promise(r => queueMicrotask(r));

        mockRect(glyph, { left: 300, top: 100, width: 520, height: 420 });
        const btn = glyph.querySelector('.window-title-bar button') as HTMLElement;
        btn.click();

        await new Promise(r => queueMicrotask(r));
        await new Promise(r => queueMicrotask(r));

        // The restore animation target should include canvas container offset:
        // canvas-local (200, 300) + canvas container viewport offset (50, 80) = (250, 380)
        expect(animToRect).not.toBeNull();
        expect(animToRect.left).toBe('250px'); // 200 + 50
        expect(animToRect.top).toBe('380px');  // 300 + 80

        // Restore original animate
        (globalThis.window as any).HTMLElement.prototype.animate = origAnimate;

        // Clean up canvas state
        resetCanvasState('offset-canvas');
    });

    // ── Close button ────────────────────────────────────────────────

    test('close button calls onClose callback', async () => {
        const { glyph } = buildCanvas();
        let closeCalled = false;

        morphCanvasPlacedToWindow(glyph, {
            title: 'Test',
            canvasId: 'test-canvas',
            onClose: () => { closeCalled = true; },
            onRestoreComplete: () => {},
        });

        await new Promise(r => queueMicrotask(r));
        await new Promise(r => queueMicrotask(r));

        // Close button is the second button (after minimize)
        const buttons = glyph.querySelectorAll('.window-title-bar button');
        expect(buttons.length).toBe(2);
        const closeBtn = buttons[1] as HTMLElement;
        expect(closeBtn.textContent).toBe('×');
        closeBtn.click();

        expect(closeCalled).toBe(true);
    });
});
