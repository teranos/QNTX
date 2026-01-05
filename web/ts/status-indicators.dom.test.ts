/**
 * @jest-environment jsdom
 *
 * DOM tests for status indicators system
 * These tests run only in CI with JSDOM environment
 */

import { statusIndicators } from './status-indicators';

describe('Status Indicators DOM Lifecycle', () => {
    beforeEach(() => {
        // Reset DOM
        document.body.innerHTML = '';
        // Reset singleton state if needed
        jest.clearAllMocks();
    });

    test('Status indicators create and manage DOM elements correctly', () => {
        // Setup: Create minimal DOM structure
        document.body.innerHTML = `
            <div id="log-header">
                <span id="log-version"></span>
                <div class="controls"></div>
            </div>
        `;

        // Initialize status indicators
        statusIndicators.init();

        // Assert container was created
        const container = document.getElementById('status-indicators');
        expect(container).toBeTruthy();
        expect(container?.className).toBe('status-indicators');

        // Assert both default indicators exist
        const connectionStatus = document.getElementById('connection-status');
        const pulseStatus = document.getElementById('pulse-status');

        expect(connectionStatus).toBeTruthy();
        expect(pulseStatus).toBeTruthy();

        // Verify structure of connection indicator
        expect(connectionStatus?.querySelector('.status-dot')).toBeTruthy();
        expect(connectionStatus?.querySelector('.status-text')).toBeTruthy();
        expect(connectionStatus?.getAttribute('role')).toBe('status');

        // Verify structure of pulse indicator (clickable)
        expect(pulseStatus?.querySelector('.status-dot')).toBeTruthy();
        expect(pulseStatus?.querySelector('.status-text')).toBeTruthy();
        expect(pulseStatus?.getAttribute('role')).toBe('button');
        expect(pulseStatus?.getAttribute('tabindex')).toBe('0');
    });

    test('Pulse indicator changes state on click', () => {
        document.body.innerHTML = `
            <div id="log-header">
                <span id="log-version"></span>
                <div class="controls"></div>
            </div>
        `;

        statusIndicators.init();

        const pulseIndicator = document.getElementById('pulse-status');
        const pulseText = pulseIndicator?.querySelector('.status-text') as HTMLElement;

        // Initial state should be inactive
        expect(pulseIndicator?.classList.contains('pulse-inactive')).toBe(true);
        expect(pulseText?.textContent).toBe('Pulse: OFF');

        // Mock sendMessage to prevent actual WebSocket calls
        const sendMessageMock = jest.fn();
        jest.mock('./websocket', () => ({
            sendMessage: sendMessageMock
        }));

        // Click to start
        pulseIndicator?.click();

        // Should show starting state
        expect(pulseIndicator?.classList.contains('pulse-starting')).toBe(true);
        expect(pulseText?.textContent).toBe('Pulse: Starting...');
        expect(pulseIndicator?.title).toBe('Starting Pulse daemon...');
    });

    test('Keyboard accessibility works for clickable indicators', () => {
        document.body.innerHTML = `
            <div id="log-header">
                <span id="log-version"></span>
                <div class="controls"></div>
            </div>
        `;

        statusIndicators.init();

        const pulseIndicator = document.getElementById('pulse-status');
        const clickHandler = jest.fn();

        // Add a spy on the click handler
        pulseIndicator?.addEventListener('click', clickHandler);

        // Test Enter key
        const enterEvent = new KeyboardEvent('keydown', { key: 'Enter' });
        pulseIndicator?.dispatchEvent(enterEvent);
        expect(clickHandler).toHaveBeenCalledTimes(1);

        // Test Space key
        const spaceEvent = new KeyboardEvent('keydown', { key: ' ' });
        pulseIndicator?.dispatchEvent(spaceEvent);
        expect(clickHandler).toHaveBeenCalledTimes(2);

        // Test that other keys don't trigger
        const escapeEvent = new KeyboardEvent('keydown', { key: 'Escape' });
        pulseIndicator?.dispatchEvent(escapeEvent);
        expect(clickHandler).toHaveBeenCalledTimes(2);
    });

    test('Connection indicator updates with status changes', () => {
        document.body.innerHTML = `
            <div id="log-header">
                <span id="log-version"></span>
                <div class="controls"></div>
            </div>
        `;

        statusIndicators.init();

        const connectionIndicator = document.getElementById('connection-status');
        const connectionText = connectionIndicator?.querySelector('.status-text') as HTMLElement;

        // Test connected state
        statusIndicators.handleConnectionStatus(true);
        expect(connectionIndicator?.classList.contains('connection-connected')).toBe(true);
        expect(connectionText?.textContent).toBe('Connected');

        // Test disconnected state
        statusIndicators.handleConnectionStatus(false);
        expect(connectionIndicator?.classList.contains('connection-disconnected')).toBe(true);
        expect(connectionText?.textContent).toBe('Disconnected');

        // Also check body class updates
        expect(document.body.classList.contains('disconnected')).toBe(true);
    });
});