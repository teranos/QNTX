/**
 * @jest-environment jsdom
 *
 * Critical path tests for canvas element ↔ window morphing (#440)
 *
 * Tim (happy path):
 * - Maximize: canvas element pops out to floating window, children preserved
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

// Mock uiState — process-global, must be superset-complete (see test/mock-ui-state.ts)
import { mock } from 'bun:test';
import { createMockUiState } from '../../test/mock-ui-state';
const { uiState, elements: mockCanvasElements } = createMockUiState();
mock.module('../../state/ui', () => ({ uiState }));

// Import after mocks
const { morphCanvasPlacedToWindow, getForm, getCanvasOrigin, getLastPosition } = await import('@teranos/elements');
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

/** Build a minimal canvas workspace with an element element inside */
function buildCanvas(): { canvas: HTMLElement; contentLayer: HTMLElement; element: HTMLElement } {
    const canvas = document.createElement('div');
    canvas.className = 'canvas-workspace';
    canvas.dataset.canvasId = 'test-canvas';
    document.body.appendChild(canvas);

    const contentLayer = document.createElement('div');
    contentLayer.className = 'canvas-content-layer';
    canvas.appendChild(contentLayer);

    // Create a canvas-placed element with children (simulating a result element)
    const item = document.createElement('div');
    item.className = 'canvas-result-element canvas-element';
    item.dataset.elementId = 'result-42';
    item.style.position = 'absolute';
    item.style.left = '100px';
    item.style.top = '200px';
    item.style.width = '400px';
    item.style.height = '250px';

    // Children: header + output (simulating real result element DOM)
    const header = document.createElement('div');
    header.className = 'result-element-header';
    header.textContent = 'prompt text';
    item.appendChild(header);

    const output = document.createElement('div');
    output.className = 'result-element-output';
    output.textContent = 'Hello World';
    item.appendChild(output);

    contentLayer.appendChild(item);

    // Mock geometry: canvas at viewport (0, 48), element at (100, 248) on screen
    mockRect(canvas, { left: 0, top: 48, width: 1200, height: 700 });
    mockRect(item, { left: 100, top: 248, width: 400, height: 250 });

    // Mock offsetLeft/offsetTop (JSDOM returns 0 by default)
    Object.defineProperty(item, 'offsetLeft', { value: 100, configurable: true });
    Object.defineProperty(item, 'offsetTop', { value: 200, configurable: true });
    Object.defineProperty(item, 'offsetWidth', { value: 400, configurable: true });
    Object.defineProperty(item, 'offsetHeight', { value: 250, configurable: true });

    return { canvas, contentLayer, element: item };
}

describe('Canvas → Window Morph', () => {
    if (!USE_JSDOM) {
        test.skip('Skipped locally (run with USE_JSDOM=1 to enable)', () => {});
        return;
    }

    beforeEach(() => {
        document.body.innerHTML = '';
        resetCanvasState('test-canvas');
        mockCanvasElements.length = 0;
    });

    // ── Tim: maximize happy path ────────────────────────────────────

    test('maximize: element moves to document.body as fixed', async () => {
        const { canvas, element: item } = buildCanvas();
        let restoreCalled = false;

        morphCanvasPlacedToWindow(item, {
            title: 'Test Result',
            canvasId: 'test-canvas',
            onRestoreComplete: () => { restoreCalled = true; },
        });

        // Let morph animation finish (microtask)
        await new Promise(r => queueMicrotask(r));
        await new Promise(r => queueMicrotask(r));

        // Element should now be on document.body, not inside canvas
        expect(canvas.contains(item)).toBe(false);
        expect(document.body.contains(item)).toBe(true);
        expect(item.style.position).toBe('fixed');
    });

    test('maximize: window state flag is set', async () => {
        const { element: item } = buildCanvas();

        morphCanvasPlacedToWindow(item, {
            title: 'Test',
            canvasId: 'test-canvas',
            onRestoreComplete: () => {},
        });

        await new Promise(r => queueMicrotask(r));

        expect(getForm(item)).toBe('window');
    });

    test('maximize: canvas origin is stored for return trip', async () => {
        const { element: item } = buildCanvas();

        morphCanvasPlacedToWindow(item, {
            title: 'Test',
            canvasId: 'test-canvas',
            onRestoreComplete: () => {},
        });

        // Canvas origin is set synchronously before animation
        const origin = getCanvasOrigin(item);
        expect(origin).not.toBeNull();
        expect(origin!.x).toBe(100);
        expect(origin!.y).toBe(200);
        expect(origin!.width).toBe(400);
        expect(origin!.height).toBe(250);
        expect(origin!.canvasId).toBe('test-canvas');
    });

    test('maximize: existing children are wrapped, not destroyed', async () => {
        const { element: item } = buildCanvas();

        morphCanvasPlacedToWindow(item, {
            title: 'Test',
            canvasId: 'test-canvas',
            onRestoreComplete: () => {},
        });

        await new Promise(r => queueMicrotask(r));
        await new Promise(r => queueMicrotask(r));

        // Children should be inside a .canvas-window-content wrapper
        const contentDiv = item.querySelector('.canvas-window-content');
        expect(contentDiv).not.toBeNull();
        expect(contentDiv!.querySelector('.result-element-header')).not.toBeNull();
        expect(contentDiv!.querySelector('.result-element-output')).not.toBeNull();
        // Output text preserved
        expect(contentDiv!.textContent).toContain('Hello World');
    });

    test('maximize: window title bar is prepended', async () => {
        const { element: item } = buildCanvas();

        morphCanvasPlacedToWindow(item, {
            title: 'My Result',
            canvasId: 'test-canvas',
            onRestoreComplete: () => {},
        });

        await new Promise(r => queueMicrotask(r));
        await new Promise(r => queueMicrotask(r));

        const titleBar = item.querySelector('.title-bar');
        expect(titleBar).not.toBeNull();
        expect(titleBar!.querySelector('span')!.textContent).toBe('My Result');

        // Has minimize button
        const buttons = titleBar!.querySelectorAll('button');
        expect(buttons.length).toBeGreaterThanOrEqual(1);
        expect(buttons[0].textContent).toBe('−');
    });

    test('maximize: double-call is a no-op', async () => {
        const { element: item } = buildCanvas();
        const config = {
            title: 'Test',
            canvasId: 'test-canvas',
            onRestoreComplete: () => {},
        };

        morphCanvasPlacedToWindow(item, config);
        await new Promise(r => queueMicrotask(r));
        await new Promise(r => queueMicrotask(r));

        // Second call should be a no-op (the already-a-window guard)
        const bodyChildCount = document.body.children.length;
        morphCanvasPlacedToWindow(item, config);
        expect(document.body.children.length).toBe(bodyChildCount);
    });

    // ── Tim: minimize happy path (TDD — tests describe correct behavior) ─

    test('minimize: element returns to original canvas parent', async () => {
        const { canvas, contentLayer, element: item } = buildCanvas();
        let restoreCalled = false;

        morphCanvasPlacedToWindow(item, {
            title: 'Test',
            canvasId: 'test-canvas',
            onRestoreComplete: () => { restoreCalled = true; },
        });

        // Wait for maximize to commit
        await new Promise(r => queueMicrotask(r));
        await new Promise(r => queueMicrotask(r));

        expect(getForm(item)).toBe('window');
        // Element should be detached from content layer
        expect(contentLayer.contains(item)).toBe(false);

        // Mock window rect for the minimize animation source
        mockRect(item, { left: 300, top: 100, width: 520, height: 420 });

        // Click minimize
        const minimizeBtn = item.querySelector('.title-bar button') as HTMLElement;
        expect(minimizeBtn).not.toBeNull();
        minimizeBtn.click();

        // Wait for minimize animation to commit
        await new Promise(r => queueMicrotask(r));
        await new Promise(r => queueMicrotask(r));

        expect(restoreCalled).toBe(true);

        // Element should be back in the content layer (its original parent)
        expect(contentLayer.contains(item)).toBe(true);
    });

    test('minimize: window state is cleared', async () => {
        const { contentLayer, element: item } = buildCanvas();

        morphCanvasPlacedToWindow(item, {
            title: 'Test',
            canvasId: 'test-canvas',
            onRestoreComplete: () => {},
        });

        await new Promise(r => queueMicrotask(r));
        await new Promise(r => queueMicrotask(r));

        mockRect(item, { left: 300, top: 100, width: 520, height: 420 });
        const btn = item.querySelector('.title-bar button') as HTMLElement;
        btn.click();

        await new Promise(r => queueMicrotask(r));
        await new Promise(r => queueMicrotask(r));

        expect(getForm(item)).toBe('canvasPlaced');
    });

    test('minimize: children are unwrapped from content div', async () => {
        const { contentLayer, element: item } = buildCanvas();

        morphCanvasPlacedToWindow(item, {
            title: 'Test',
            canvasId: 'test-canvas',
            onRestoreComplete: () => {},
        });

        await new Promise(r => queueMicrotask(r));
        await new Promise(r => queueMicrotask(r));

        mockRect(item, { left: 300, top: 100, width: 520, height: 420 });
        const btn = item.querySelector('.title-bar button') as HTMLElement;
        btn.click();

        await new Promise(r => queueMicrotask(r));
        await new Promise(r => queueMicrotask(r));

        // Content div and title bar should be removed
        expect(item.querySelector('.canvas-window-content')).toBeNull();
        expect(item.querySelector('.title-bar')).toBeNull();

        // Original children should be direct children again
        expect(item.querySelector('.result-element-header')).not.toBeNull();
        expect(item.querySelector('.result-element-output')).not.toBeNull();
        expect(item.textContent).toContain('Hello World');
    });

    test('minimize: canvas origin is cleared', async () => {
        const { contentLayer, element: item } = buildCanvas();

        morphCanvasPlacedToWindow(item, {
            title: 'Test',
            canvasId: 'test-canvas',
            onRestoreComplete: () => {},
        });

        await new Promise(r => queueMicrotask(r));
        await new Promise(r => queueMicrotask(r));

        // Origin should exist while in window state
        expect(getCanvasOrigin(item)).not.toBeNull();

        mockRect(item, { left: 300, top: 100, width: 520, height: 420 });
        const btn = item.querySelector('.title-bar button') as HTMLElement;
        btn.click();

        await new Promise(r => queueMicrotask(r));
        await new Promise(r => queueMicrotask(r));

        expect(getCanvasOrigin(item)).toBeNull();
    });

    test('minimize: canvas-local position is restored on element', async () => {
        const { contentLayer, element: item } = buildCanvas();

        morphCanvasPlacedToWindow(item, {
            title: 'Test',
            canvasId: 'test-canvas',
            onRestoreComplete: () => {},
        });

        await new Promise(r => queueMicrotask(r));
        await new Promise(r => queueMicrotask(r));

        mockRect(item, { left: 300, top: 100, width: 520, height: 420 });
        const btn = item.querySelector('.title-bar button') as HTMLElement;
        btn.click();

        await new Promise(r => queueMicrotask(r));
        await new Promise(r => queueMicrotask(r));

        // Element should have its original canvas-local coords restored
        expect(item.style.position).toBe('absolute');
        expect(item.style.left).toBe('100px');
        expect(item.style.top).toBe('200px');
        expect(item.style.width).toBe('400px');
        expect(item.style.height).toBe('250px');
    });

    test('minimize: window position is remembered for next expand', async () => {
        const { contentLayer, element: item } = buildCanvas();

        morphCanvasPlacedToWindow(item, {
            title: 'Test',
            canvasId: 'test-canvas',
            onRestoreComplete: () => {},
        });

        await new Promise(r => queueMicrotask(r));
        await new Promise(r => queueMicrotask(r));

        // Simulate window at a custom position
        mockRect(item, { left: 450, top: 200, width: 520, height: 420 });
        const btn = item.querySelector('.title-bar button') as HTMLElement;
        btn.click();

        await new Promise(r => queueMicrotask(r));
        await new Promise(r => queueMicrotask(r));

        // Last window position should be saved
        const lastPos = getLastPosition(item);
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

        const item = document.createElement('div');
        item.className = 'canvas-result-element canvas-element';
        item.dataset.elementId = 'result-offset';
        const child = document.createElement('div');
        child.className = 'test-child';
        item.appendChild(child);
        contentLayer.appendChild(item);

        mockRect(canvas, { left: 50, top: 80, width: 1000, height: 600 });
        mockRect(item, { left: 250, top: 380, width: 400, height: 200 });
        Object.defineProperty(item, 'offsetLeft', { value: 200, configurable: true });
        Object.defineProperty(item, 'offsetTop', { value: 300, configurable: true });
        Object.defineProperty(item, 'offsetWidth', { value: 400, configurable: true });
        Object.defineProperty(item, 'offsetHeight', { value: 200, configurable: true });

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

        morphCanvasPlacedToWindow(item, {
            title: 'Test',
            canvasId: 'offset-canvas',
            onRestoreComplete: () => {},
        });

        await new Promise(r => queueMicrotask(r));
        await new Promise(r => queueMicrotask(r));

        mockRect(item, { left: 300, top: 100, width: 520, height: 420 });
        const btn = item.querySelector('.title-bar button') as HTMLElement;
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

    // ── Element-owned title bar preservation ──────────────────────────

    test('minimize: element-owned title bar is preserved, only window controls removed', async () => {
        const { contentLayer, element: item } = buildCanvas();

        // Replace the .result-element-header with a proper .title-bar
        const oldHeader = item.querySelector('.result-element-header')!;
        const elementTitleBar = document.createElement('div');
        elementTitleBar.className = 'title-bar';
        const elementBtn = document.createElement('button');
        elementBtn.textContent = 'Copy';
        elementTitleBar.appendChild(elementBtn);
        item.replaceChild(elementTitleBar, oldHeader);

        morphCanvasPlacedToWindow(item, {
            title: 'Test',
            canvasId: 'test-canvas',
            onRestoreComplete: () => {},
        });

        await new Promise(r => queueMicrotask(r));
        await new Promise(r => queueMicrotask(r));

        // Window controls should have been added to existing title bar
        expect(elementTitleBar.querySelector('.window-controls')).not.toBeNull();

        mockRect(item, { left: 300, top: 100, width: 520, height: 420 });
        const minimizeBtn = elementTitleBar.querySelector('.window-controls button') as HTMLElement;
        minimizeBtn.click();

        await new Promise(r => queueMicrotask(r));
        await new Promise(r => queueMicrotask(r));

        // Title bar should still exist (element-owned)
        expect(item.querySelector('.title-bar')).not.toBeNull();
        // But window controls should be stripped
        expect(item.querySelector('.window-controls')).toBeNull();
        // Element's own button should survive
        expect(item.querySelector('.title-bar button')!.textContent).toBe('Copy');
    });

    // ── Close button ────────────────────────────────────────────────

    test('close button calls onClose callback', async () => {
        const { element: item } = buildCanvas();
        let closeCalled = false;

        morphCanvasPlacedToWindow(item, {
            title: 'Test',
            canvasId: 'test-canvas',
            onClose: () => { closeCalled = true; },
            onRestoreComplete: () => {},
        });

        await new Promise(r => queueMicrotask(r));
        await new Promise(r => queueMicrotask(r));

        // Close button is the second button (after minimize)
        const buttons = item.querySelectorAll('.title-bar button');
        expect(buttons.length).toBe(2);
        const closeBtn = buttons[1] as HTMLElement;
        expect(closeBtn.textContent).toBe('×');
        closeBtn.click();

        expect(closeCalled).toBe(true);
    });
});
