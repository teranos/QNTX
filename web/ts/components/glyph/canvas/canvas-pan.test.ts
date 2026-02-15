/**
 * Tests for canvas pan and zoom functionality
 * Tests pan behavior on desktop (trackpad/mouse) and mobile (touch)
 * Tests zoom behavior (Ctrl+wheel, pinch zoom)
 */

import { describe, test, expect, beforeEach } from 'bun:test';
import {
    setupCanvasPan,
    getPanOffset,
    getTransform,
    setZoom,
    resetTransform,
    screenToCanvas,
    canvasToScreen,
    resetCanvasState
} from './canvas-pan';
import { makeDraggable, makeResizable } from '../glyph-interaction';
import type { Glyph } from '../glyph';
import { uiState } from '../../../state/ui';

// Helper to create wheel event in test environment
function createWheelEvent(deltaX: number, deltaY: number, ctrlKey: boolean): Event {
    const Win = globalThis.window as any;
    const event = new Win.Event('wheel', { bubbles: true, cancelable: true });
    event.deltaX = deltaX;
    event.deltaY = deltaY;
    event.ctrlKey = ctrlKey;
    return event;
}

// Helper to create touch event in test environment
function createTouchEvent(type: string, clientX: number, clientY: number, identifier: number = 0): TouchEvent {
    const Win = globalThis.window as any;
    const touch = {
        identifier,
        clientX,
        clientY,
        screenX: clientX,
        screenY: clientY,
        pageX: clientX,
        pageY: clientY,
        target: null,
    };

    const event = new Win.Event(type, { bubbles: true, cancelable: true }) as TouchEvent;
    Object.defineProperty(event, 'touches', { value: type === 'touchend' ? [] : [touch], writable: false });
    Object.defineProperty(event, 'changedTouches', { value: [touch], writable: false });
    Object.defineProperty(event, 'targetTouches', { value: type === 'touchend' ? [] : [touch], writable: false });

    return event;
}

// Helper to create two-finger touch event for pinch zoom
function createPinchEvent(type: string, touch1X: number, touch1Y: number, touch2X: number, touch2Y: number): TouchEvent {
    const Win = globalThis.window as any;
    const touch1 = {
        identifier: 0,
        clientX: touch1X,
        clientY: touch1Y,
        screenX: touch1X,
        screenY: touch1Y,
        pageX: touch1X,
        pageY: touch1Y,
        target: null,
    };
    const touch2 = {
        identifier: 1,
        clientX: touch2X,
        clientY: touch2Y,
        screenX: touch2X,
        screenY: touch2Y,
        pageX: touch2X,
        pageY: touch2Y,
        target: null,
    };

    const event = new Win.Event(type, { bubbles: true, cancelable: true }) as TouchEvent;
    const touches = type === 'touchend' ? [] : [touch1, touch2];
    Object.defineProperty(event, 'touches', { value: touches, writable: false });
    Object.defineProperty(event, 'changedTouches', { value: [touch1, touch2], writable: false });
    Object.defineProperty(event, 'targetTouches', { value: touches, writable: false });

    return event;
}

describe('Canvas Pan', () => {
    beforeEach(() => {
        // Reset DOM
        document.body.innerHTML = '';

        // Reset canvas state
        resetCanvasState('test-canvas');

        // Clear persisted pan state if method available
        if (typeof uiState.setCanvasPan === 'function') {
            uiState.setCanvasPan('test-canvas', { panX: 0, panY: 0 });
        }

        // Mock matchMedia for desktop mode
        Object.defineProperty(window, 'matchMedia', {
            writable: true,
            value: (query: string) => ({
                matches: false, // Desktop mode
                media: query,
                onchange: null,
                addListener: () => { },
                removeListener: () => { },
                addEventListener: () => { },
                removeEventListener: () => { },
                dispatchEvent: () => true,
            }),
        });
    });

    test('setupCanvasPan returns AbortController', () => {
        const container = document.createElement('div');
        const contentLayer = document.createElement('div');
        contentLayer.className = 'canvas-content-layer';
        container.appendChild(contentLayer);

        const controller = setupCanvasPan(container, 'test-canvas');

        expect(controller).toBeInstanceOf(AbortController);
        expect(controller.signal).toBeDefined();
    });

    test('loads persisted pan state on setup', () => {
        // Skip if uiState methods not available (CI environment issue)
        if (typeof uiState.setCanvasPan !== 'function') {
            return;
        }

        // Set persisted state
        uiState.setCanvasPan('test-canvas', { panX: 100, panY: 200 });

        const container = document.createElement('div');
        const contentLayer = document.createElement('div');
        contentLayer.className = 'canvas-content-layer';
        container.appendChild(contentLayer);

        setupCanvasPan(container, 'test-canvas');

        // Check transform was applied to content layer (no zoom, defaults to scale 1)
        expect(contentLayer.style.transform).toBe('translate(100px, 200px) scale(1)');
    });

    test('wheel event updates pan offset (desktop trackpad scroll)', () => {
        const container = document.createElement('div');
        const contentLayer = document.createElement('div');
        contentLayer.className = 'canvas-content-layer';
        container.appendChild(contentLayer);
        document.body.appendChild(container);

        setupCanvasPan(container, 'test-canvas');

        // Simulate two-finger trackpad scroll (wheel with ctrlKey = false)
        const wheelEvent = createWheelEvent(10, 20, false);

        container.dispatchEvent(wheelEvent);

        // Pan should move opposite to scroll direction
        expect(contentLayer.style.transform).toBe('translate(-10px, -20px) scale(1)');

        // State should be persisted (skip check if method not available in CI)
        if (typeof uiState.getCanvasPan === 'function') {
            const saved = uiState.getCanvasPan('test-canvas');
            expect(saved?.panX).toBe(-10);
            expect(saved?.panY).toBe(-20);
            expect(saved?.scale).toBe(1);
        }
    });

    test('wheel event with ctrlKey zooms (Ctrl+wheel zoom)', () => {
        const container = document.createElement('div');
        const contentLayer = document.createElement('div');
        contentLayer.className = 'canvas-content-layer';
        container.appendChild(contentLayer);
        document.body.appendChild(container);

        // Mock getBoundingClientRect for zoom origin calculation
        container.getBoundingClientRect = () => ({
            left: 0,
            top: 0,
            width: 800,
            height: 600,
            right: 800,
            bottom: 600,
            x: 0,
            y: 0,
            toJSON: () => { },
        });

        setupCanvasPan(container, 'test-canvas');

        // Simulate zoom in (negative deltaY zooms in)
        const wheelEvent = createWheelEvent(0, -100, true);
        Object.defineProperty(wheelEvent, 'clientX', { value: 400 });
        Object.defineProperty(wheelEvent, 'clientY', { value: 300 });

        container.dispatchEvent(wheelEvent);

        // Scale should increase (zoom in)
        const transform = getTransform('test-canvas');
        expect(transform.scale).toBeGreaterThan(1.0);

        // State should be persisted (skip check if method not available in CI)
        if (typeof uiState.getCanvasPan === 'function') {
            const saved = uiState.getCanvasPan('test-canvas');
            expect(saved?.scale).toBeGreaterThan(1.0);
        }
    });

    test('abort() cleans up event listeners', () => {
        const container = document.createElement('div');
        const contentLayer = document.createElement('div');
        contentLayer.className = 'canvas-content-layer';
        container.appendChild(contentLayer);
        document.body.appendChild(container);

        const controller = setupCanvasPan(container, 'test-canvas');

        // Abort
        controller.abort();

        // Wheel event should no longer update pan
        const wheelEvent = createWheelEvent(10, 20, false);

        container.dispatchEvent(wheelEvent);

        // Pan should remain at 0
        expect(contentLayer.style.transform).toBe('translate(0px, 0px) scale(1)');
    });

    test('touch events update pan offset (mobile/responsive mode)', () => {
        const container = document.createElement('div');
        const contentLayer = document.createElement('div');
        contentLayer.className = 'canvas-content-layer';
        container.appendChild(contentLayer);
        document.body.appendChild(container);

        setupCanvasPan(container, 'test-canvas');

        // Simulate touchstart
        const touchStart = createTouchEvent('touchstart', 100, 100);
        container.dispatchEvent(touchStart);

        // Simulate touchmove - drag 50px right, 30px down
        const touchMove = createTouchEvent('touchmove', 150, 130);
        container.dispatchEvent(touchMove);

        // Pan should update
        expect(contentLayer.style.transform).toBe('translate(50px, 30px) scale(1)');

        // Simulate touchend
        const touchEnd = createTouchEvent('touchend', 150, 130);
        container.dispatchEvent(touchEnd);

        // State should be persisted (skip check if method not available in CI)
        if (typeof uiState.getCanvasPan === 'function') {
            const saved = uiState.getCanvasPan('test-canvas');
            expect(saved?.panX).toBe(50);
            expect(saved?.panY).toBe(30);
            expect(saved?.scale).toBe(1);
        }
    });

    test('touch pan works regardless of target (no glyph check)', () => {
        const container = document.createElement('div');
        const contentLayer = document.createElement('div');
        contentLayer.className = 'canvas-content-layer';
        container.appendChild(contentLayer);

        // Create a mock glyph inside the content layer
        const glyph = document.createElement('div');
        glyph.setAttribute('data-glyph-id', 'test-glyph');
        glyph.className = 'canvas-py-glyph';
        contentLayer.appendChild(glyph);

        document.body.appendChild(container);

        setupCanvasPan(container, 'test-canvas');

        // Touch events on container should work even with glyph present
        const touchStart = createTouchEvent('touchstart', 100, 100);
        container.dispatchEvent(touchStart);

        const touchMove = createTouchEvent('touchmove', 150, 130);
        container.dispatchEvent(touchMove);

        // Pan should work
        expect(contentLayer.style.transform).toBe('translate(50px, 30px) scale(1)');

        const touchEnd = createTouchEvent('touchend', 150, 130);
        container.dispatchEvent(touchEnd);
    });

    test('zoom respects min/max limits', () => {
        const container = document.createElement('div');
        const contentLayer = document.createElement('div');
        contentLayer.className = 'canvas-content-layer';
        container.appendChild(contentLayer);
        document.body.appendChild(container);

        setupCanvasPan(container, 'test-canvas');

        // Try to zoom beyond max (4.0)
        setZoom(container, 'test-canvas', 10.0);
        let transform = getTransform('test-canvas');
        expect(transform.scale).toBe(4.0);

        // Try to zoom below min (0.25)
        setZoom(container, 'test-canvas', 0.1);
        transform = getTransform('test-canvas');
        expect(transform.scale).toBe(0.25);
    });

    test('transform combination: pan and zoom work together', () => {
        const container = document.createElement('div');
        const contentLayer = document.createElement('div');
        contentLayer.className = 'canvas-content-layer';
        container.appendChild(contentLayer);
        document.body.appendChild(container);

        setupCanvasPan(container, 'test-canvas');

        // Pan first
        const wheelPan = createWheelEvent(50, 100, false);
        container.dispatchEvent(wheelPan);

        let transform = getTransform('test-canvas');
        const initialPanX = transform.panX;
        const initialPanY = transform.panY;

        // Then zoom toward origin (0, 0)
        setZoom(container, 'test-canvas', 2.0);

        // State should have zoom applied, pan adjusted for zoom origin
        transform = getTransform('test-canvas');
        expect(transform.scale).toBe(2.0);
        // Pan adjusts when zooming toward origin: panX = 0 - (0 - oldPanX) * 2 = -2 * oldPanX
        expect(transform.panX).toBe(initialPanX * 2);
        expect(transform.panY).toBe(initialPanY * 2);

        // State should be persisted (skip check if method not available in CI)
        if (typeof uiState.getCanvasPan === 'function') {
            const saved = uiState.getCanvasPan('test-canvas');
            expect(saved?.scale).toBe(2.0);
        }
    });

    test('coordinate conversion: screenToCanvas and canvasToScreen', () => {
        const container = document.createElement('div');
        const contentLayer = document.createElement('div');
        contentLayer.className = 'canvas-content-layer';
        container.appendChild(contentLayer);
        document.body.appendChild(container);

        setupCanvasPan(container, 'test-canvas');

        // Set known transform: pan (100, 50), zoom 2x
        setZoom(container, 'test-canvas', 2.0, 100, 50);

        // Screen point (200, 100) should map to canvas coordinates
        const canvasPoint = screenToCanvas('test-canvas', 200, 100);

        // Verify round-trip conversion
        const screenPoint = canvasToScreen('test-canvas', canvasPoint.x, canvasPoint.y);
        expect(screenPoint.x).toBeCloseTo(200, 1);
        expect(screenPoint.y).toBeCloseTo(100, 1);
    });

    test('resetTransform clears pan and zoom', () => {
        const container = document.createElement('div');
        const contentLayer = document.createElement('div');
        contentLayer.className = 'canvas-content-layer';
        container.appendChild(contentLayer);
        document.body.appendChild(container);

        setupCanvasPan(container, 'test-canvas');

        // Apply some transforms
        const wheelPan = createWheelEvent(50, 100, false);
        container.dispatchEvent(wheelPan);
        setZoom(container, 'test-canvas', 2.0);

        // Reset
        resetTransform(container, 'test-canvas');

        const transform = getTransform('test-canvas');
        expect(transform.panX).toBe(0);
        expect(transform.panY).toBe(0);
        expect(transform.scale).toBe(1.0);
    });

    test('zoom state persists across setup', () => {
        // Skip if uiState methods not available (CI environment issue)
        if (typeof uiState.setCanvasPan !== 'function') {
            return;
        }

        // Set persisted state with zoom
        uiState.setCanvasPan('test-canvas', { panX: 100, panY: 200, scale: 1.5 });

        const container = document.createElement('div');
        const contentLayer = document.createElement('div');
        contentLayer.className = 'canvas-content-layer';
        container.appendChild(contentLayer);

        setupCanvasPan(container, 'test-canvas');

        // Check transform includes zoom
        const transform = getTransform('test-canvas');
        expect(transform.panX).toBe(100);
        expect(transform.panY).toBe(200);
        expect(transform.scale).toBe(1.5);
        expect(contentLayer.style.transform).toBe('translate(100px, 200px) scale(1.5)');
    });

    test('zoom state backward compatible: missing scale defaults to 1.0', () => {
        // Skip if uiState methods not available (CI environment issue)
        if (typeof uiState.setCanvasPan !== 'function') {
            return;
        }

        // Set old-format persisted state (no scale property)
        uiState.setCanvasPan('test-canvas', { panX: 100, panY: 200 });

        const container = document.createElement('div');
        const contentLayer = document.createElement('div');
        contentLayer.className = 'canvas-content-layer';
        container.appendChild(contentLayer);

        setupCanvasPan(container, 'test-canvas');

        // Scale should default to 1.0
        const transform = getTransform('test-canvas');
        expect(transform.scale).toBe(1.0);
    });

    test('two-finger pinch zooms (mobile)', () => {
        const container = document.createElement('div');
        const contentLayer = document.createElement('div');
        contentLayer.className = 'canvas-content-layer';
        container.appendChild(contentLayer);
        document.body.appendChild(container);

        // Mock getBoundingClientRect for zoom origin calculation
        container.getBoundingClientRect = () => ({
            left: 0,
            top: 0,
            width: 800,
            height: 600,
            right: 800,
            bottom: 600,
            x: 0,
            y: 0,
            toJSON: () => { },
        });

        setupCanvasPan(container, 'test-canvas');

        // Simulate pinch start: two touches 100px apart (center at 400, 300)
        const touchStart = createPinchEvent('touchstart', 350, 300, 450, 300);
        container.dispatchEvent(touchStart);

        // Simulate pinch move: spread to 200px apart (zoom in 2x)
        const touchMove = createPinchEvent('touchmove', 300, 300, 500, 300);
        container.dispatchEvent(touchMove);

        // Scale should increase (zoom in)
        const transform = getTransform('test-canvas');
        expect(transform.scale).toBeGreaterThan(1.0);
        expect(transform.scale).toBeCloseTo(2.0, 0);

        // Simulate pinch end
        const touchEnd = createPinchEvent('touchend', 300, 300, 500, 300);
        container.dispatchEvent(touchEnd);

        // State should be persisted (skip check if method not available in CI)
        if (typeof uiState.getCanvasPan === 'function') {
            const saved = uiState.getCanvasPan('test-canvas');
            expect(saved?.scale).toBeGreaterThan(1.0);
        }
    });
});

// ── Drag/resize respect canvas zoom ─────────────────────────────────

// Mock ResizeObserver (used by some glyph code paths)
globalThis.ResizeObserver = class ResizeObserver {
    observe() {}
    unobserve() {}
    disconnect() {}
} as any;

function buildCanvasWithGlyph(canvasId: string) {
    const container = document.createElement('div');
    container.setAttribute('data-canvas-id', canvasId);

    const contentLayer = document.createElement('div');
    contentLayer.className = 'canvas-content-layer';
    container.appendChild(contentLayer);

    const el = document.createElement('div');
    el.setAttribute('data-glyph-id', 'g1');
    el.style.position = 'absolute';
    el.style.left = '100px';
    el.style.top = '100px';
    el.style.width = '200px';
    el.style.height = '150px';
    // happy-dom doesn't compute layout from CSS; mock offset* so drag/resize starts from real values
    Object.defineProperty(el, 'offsetLeft', { value: 100, configurable: true });
    Object.defineProperty(el, 'offsetTop', { value: 100, configurable: true });
    Object.defineProperty(el, 'offsetWidth', { value: 200, configurable: true });
    Object.defineProperty(el, 'offsetHeight', { value: 150, configurable: true });
    contentLayer.appendChild(el);

    document.body.appendChild(container);

    const glyph: Glyph = {
        id: 'g1',
        title: 'Test',
        symbol: '⊨',
        x: 100,
        y: 100,
        width: 200,
        height: 150,
        renderContent: () => el,
    };

    return { container, contentLayer, el, glyph };
}

function mouseEvent(type: string, clientX: number, clientY: number): MouseEvent {
    const Win = globalThis.window as any;
    const e = new Win.Event(type, { bubbles: true, cancelable: true }) as MouseEvent;
    Object.defineProperty(e, 'clientX', { value: clientX });
    Object.defineProperty(e, 'clientY', { value: clientY });
    return e;
}

describe('Drag respects canvas zoom - Tim (Happy Path)', () => {
    beforeEach(() => {
        document.body.innerHTML = '';
        resetCanvasState('drag-zoom');
    });

    test('Tim drags glyph at scale 1.0 — delta matches mouse', () => {
        const { container, el, glyph } = buildCanvasWithGlyph('drag-zoom');
        setupCanvasPan(container, 'drag-zoom');

        const handle = el;
        makeDraggable(el, handle, glyph);

        handle.dispatchEvent(mouseEvent('mousedown', 200, 200));
        document.dispatchEvent(mouseEvent('mousemove', 300, 250));

        expect(el.style.left).toBe('200px'); // 100 + 100/1
        expect(el.style.top).toBe('150px');  // 100 + 50/1

        document.dispatchEvent(mouseEvent('mouseup', 300, 250));
    });

    test('Tim drags glyph at scale 0.5 — glyph moves 2x mouse delta in CSS space', () => {
        const { container, el, glyph } = buildCanvasWithGlyph('drag-zoom');
        setupCanvasPan(container, 'drag-zoom');
        setZoom(container, 'drag-zoom', 0.5);

        const handle = el;
        makeDraggable(el, handle, glyph);

        handle.dispatchEvent(mouseEvent('mousedown', 200, 200));
        document.dispatchEvent(mouseEvent('mousemove', 300, 250));

        // 100px mouse delta / 0.5 scale = 200px CSS delta
        expect(el.style.left).toBe('300px'); // 100 + 200
        expect(el.style.top).toBe('200px');  // 100 + 100

        document.dispatchEvent(mouseEvent('mouseup', 300, 250));
    });

    test('Tim drags glyph at scale 2.0 — glyph moves 0.5x mouse delta in CSS space', () => {
        const { container, el, glyph } = buildCanvasWithGlyph('drag-zoom');
        setupCanvasPan(container, 'drag-zoom');
        setZoom(container, 'drag-zoom', 2.0);

        const handle = el;
        makeDraggable(el, handle, glyph);

        handle.dispatchEvent(mouseEvent('mousedown', 200, 200));
        document.dispatchEvent(mouseEvent('mousemove', 300, 250));

        // 100px mouse delta / 2.0 scale = 50px CSS delta
        expect(el.style.left).toBe('150px'); // 100 + 50
        expect(el.style.top).toBe('125px');  // 100 + 25

        document.dispatchEvent(mouseEvent('mouseup', 300, 250));
    });
});

describe('Resize respects canvas zoom - Tim (Happy Path)', () => {
    beforeEach(() => {
        document.body.innerHTML = '';
        resetCanvasState('resize-zoom');
    });

    test('Tim resizes glyph at scale 0.5 — size grows 2x mouse delta', () => {
        const { container, el, glyph } = buildCanvasWithGlyph('resize-zoom');
        setupCanvasPan(container, 'resize-zoom');
        setZoom(container, 'resize-zoom', 0.5);

        const resizeHandle = document.createElement('div');
        el.appendChild(resizeHandle);

        makeResizable(el, resizeHandle, glyph);

        resizeHandle.dispatchEvent(mouseEvent('mousedown', 300, 250));
        document.dispatchEvent(mouseEvent('mousemove', 350, 300));

        // 50px mouse delta / 0.5 scale = 100px CSS delta
        expect(el.style.width).toBe('300px');  // 200 + 100
        expect(el.style.height).toBe('250px'); // 150 + 100

        document.dispatchEvent(mouseEvent('mouseup', 350, 300));
    });
});
