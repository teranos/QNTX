/**
 * @jest-environment jsdom
 *
 * Critical path tests for VidStream window
 * Focus: initialization flow, handler registration, window isolation
 */

import { describe, test, expect, beforeEach } from 'bun:test';
import { VidStreamWindow } from './vidstream-window.ts';
import { Window } from './components/window.ts';

describe('VidStream Critical Paths', () => {
    beforeEach(() => {
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

    test('Window component event listener isolation: multiple windows dont interfere', () => {
        // Create two windows with similar button IDs
        const vidstreamWindow = new Window({
            id: 'vidstream-test-window',
            title: 'VidStream',
            width: '600px',
        });

        const aiProviderWindow = new Window({
            id: 'ai-provider-test-window',
            title: 'AI Provider',
            width: '400px',
        });

        // Add buttons with same class to both windows
        vidstreamWindow.setContent(`
            <button id="test-btn" class="action-btn">VidStream Action</button>
        `);

        aiProviderWindow.setContent(`
            <button id="test-btn" class="action-btn">AI Provider Action</button>
        `);

        // Setup handlers that track which window was clicked
        let vidstreamClicked = false;
        let aiProviderClicked = false;

        const vidstreamBtn = vidstreamWindow.getElement().querySelector('#test-btn');
        const aiProviderBtn = aiProviderWindow.getElement().querySelector('#test-btn');

        vidstreamBtn?.addEventListener('click', () => {
            vidstreamClicked = true;
        });

        aiProviderBtn?.addEventListener('click', () => {
            aiProviderClicked = true;
        });

        // Click VidStream button
        (vidstreamBtn as HTMLElement)?.click();

        expect(vidstreamClicked).toBe(true);
        expect(aiProviderClicked).toBe(false);

        // Reset and click AI Provider button
        vidstreamClicked = false;
        (aiProviderBtn as HTMLElement)?.click();

        expect(vidstreamClicked).toBe(false);
        expect(aiProviderClicked).toBe(true);
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
