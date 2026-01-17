/**
 * @jest-environment jsdom
 *
 * Critical path tests for VidStream window
 * Focus: initialization flow, handler registration, window isolation
 *
 * These tests run only in CI with JSDOM environment (gated by USE_JSDOM=1)
 */

import { describe, test, expect, beforeEach } from 'bun:test';
import { VidStreamWindow } from './vidstream-window.ts';
import { Window } from './components/window';

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
}

describe('VidStream Critical Paths', () => {
    if (!USE_JSDOM) {
        test.skip('Skipped locally (run with USE_JSDOM=1 to enable)', () => {});
        return;
    }

    beforeEach(() => {
        Window.closeAll(); // Properly cleanup Window instances
        document.body.innerHTML = '';
    });

    test('Initialization flow: VidStream window creates and manages state', () => {
        // Create VidStreamWindow (registers handlers and creates UI)
        const vidstream = new VidStreamWindow();

        // Verify window created in DOM
        const windowEl = document.getElementById('vidstream-window');
        expect(windowEl).not.toBeNull();

        // Verify key UI elements exist
        const modelPathSpan = windowEl?.querySelector('#vs-model-path') as HTMLSpanElement;
        const configBtn = windowEl?.querySelector('#vs-config-btn');
        const initBtn = windowEl?.querySelector('#vs-init-btn');
        const startBtn = windowEl?.querySelector('#vs-start-btn');
        const canvas = windowEl?.querySelector('#vs-canvas');

        expect(modelPathSpan).not.toBeNull();
        expect(configBtn).not.toBeNull();
        expect(initBtn).not.toBeNull();
        expect(startBtn).not.toBeNull();
        expect(canvas).not.toBeNull();

        // Verify default model path is displayed
        expect(modelPathSpan?.textContent).toBe('ats/vidstream/models/yolo11n.onnx');

        // Verify window starts not visible (no data-visible attribute set initially)
        const initialVisible = windowEl?.getAttribute('data-visible');
        expect(initialVisible === null || initialVisible === 'false').toBe(true);

        // Show window
        vidstream.show();
        expect(windowEl?.getAttribute('data-visible')).toBe('true');

        // Hide window
        vidstream.hide();
        expect(windowEl?.getAttribute('data-visible')).toBe('false');
    });

    test('Window component creates elements in DOM', () => {
        // Simple test: verify Window component creates DOM element
        const testWindow = new Window({
            id: 'test-window',
            title: 'Test',
            width: '400px',
        });

        const windowEl = document.getElementById('test-window');
        expect(windowEl).not.toBeNull();
        expect(windowEl?.className).toBe('draggable-window');

        // Verify setContent works
        testWindow.setContent('<div id="test-content">Hello</div>');
        const content = windowEl?.querySelector('#test-content');
        expect(content).not.toBeNull();
        expect(content?.textContent).toBe('Hello');
    });

    test('Handler registration after WebSocket: VidStreamWindow can be created after init', () => {
        // Simulate WebSocket connection already established
        // (In real app, connectWebSocket() is called in app.ts before components load)

        // Create VidStreamWindow late (simulates lazy initialization)
        const vidstream = new VidStreamWindow();

        // Verify window was created successfully
        const windowEl = document.getElementById('vidstream-window');
        expect(windowEl).not.toBeNull();

        // Verify UI is functional
        const initBtn = windowEl?.querySelector('#vs-init-btn');
        expect(initBtn).not.toBeNull();
        expect((initBtn as HTMLButtonElement)?.disabled).toBe(false);

        // Verify we can show the window (handlers are set up)
        vidstream.show();
        expect(windowEl?.getAttribute('data-visible')).toBe('true');

        // Cleanup
        vidstream.hide();
    });
});
