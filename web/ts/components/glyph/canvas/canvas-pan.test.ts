/**
 * Tests for canvas pan functionality
 * Tests pan behavior on desktop (trackpad/mouse) and mobile (touch)
 */

import { describe, test, expect, beforeEach } from 'bun:test';
import { setupCanvasPan, getPanOffset, resetCanvasState } from './canvas-pan';
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

        // Check transform was applied to content layer
        expect(contentLayer.style.transform).toBe('translate(100px, 200px)');
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
        expect(contentLayer.style.transform).toBe('translate(-10px, -20px)');

        // State should be persisted (skip check if method not available in CI)
        if (typeof uiState.getCanvasPan === 'function') {
            const saved = uiState.getCanvasPan('test-canvas');
            expect(saved).toEqual({ panX: -10, panY: -20 });
        }
    });

    test('wheel event with ctrlKey is ignored (pinch zoom, not pan)', () => {
        const container = document.createElement('div');
        const contentLayer = document.createElement('div');
        contentLayer.className = 'canvas-content-layer';
        container.appendChild(contentLayer);
        document.body.appendChild(container);

        setupCanvasPan(container, 'test-canvas');

        // Simulate pinch zoom (wheel with ctrlKey = true)
        const wheelEvent = createWheelEvent(10, 20, true);

        container.dispatchEvent(wheelEvent);

        // Pan should not change
        expect(contentLayer.style.transform).toBe('translate(0px, 0px)');
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
        expect(contentLayer.style.transform).toBe('translate(0px, 0px)');
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
        expect(contentLayer.style.transform).toBe('translate(50px, 30px)');

        // Simulate touchend
        const touchEnd = createTouchEvent('touchend', 150, 130);
        container.dispatchEvent(touchEnd);

        // State should be persisted (skip check if method not available in CI)
        if (typeof uiState.getCanvasPan === 'function') {
            const saved = uiState.getCanvasPan('test-canvas');
            expect(saved).toEqual({ panX: 50, panY: 30 });
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
        expect(contentLayer.style.transform).toBe('translate(50px, 30px)');

        const touchEnd = createTouchEvent('touchend', 150, 130);
        container.dispatchEvent(touchEnd);
    });
});
