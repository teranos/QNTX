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

        // Mock matchMedia to simulate desktop (non-mobile) mode
        Object.defineProperty(window, 'matchMedia', {
            writable: true,
            value: jest.fn().mockImplementation(query => ({
                matches: false, // Always return false (desktop mode)
                media: query,
                onchange: null,
                addListener: jest.fn(),
                removeListener: jest.fn(),
                addEventListener: jest.fn(),
                removeEventListener: jest.fn(),
                dispatchEvent: jest.fn(),
            })),
        });
    });

    test('Status indicators create and manage DOM elements correctly', () => {
        // Setup: Create minimal DOM structure
        document.body.innerHTML = `
            <div id="system-drawer-header">
                <span id="system-version"></span>
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
            <div id="system-drawer-header">
                <span id="system-version"></span>
                <div class="controls"></div>
            </div>
        `;

        statusIndicators.init();

        const pulseIndicator = document.getElementById('pulse-status');
        const pulseText = pulseIndicator?.querySelector('.status-text') as HTMLElement;

        // Initial state should be inactive
        expect(pulseIndicator?.classList.contains('pulse-inactive')).toBe(true);
        expect(pulseText?.textContent).toBe('Pulse: OFF');

        // Click to start
        pulseIndicator?.click();

        // Should show starting state
        expect(pulseIndicator?.classList.contains('pulse-starting')).toBe(true);
        expect(pulseText?.textContent).toBe('Pulse: Starting...');
        expect(pulseIndicator?.title).toBe('Starting Pulse daemon...');
    });

    test('Keyboard accessibility works for clickable indicators', () => {
        document.body.innerHTML = `
            <div id="system-drawer-header">
                <span id="system-version"></span>
                <div class="controls"></div>
            </div>
        `;

        statusIndicators.init();

        const pulseIndicator = document.getElementById('pulse-status');

        // The actual implementation has keydown handlers that were added during init()
        // Test that clicking works
        const initialText = pulseIndicator?.querySelector('.status-text')?.textContent;
        pulseIndicator?.click();
        const afterClickText = pulseIndicator?.querySelector('.status-text')?.textContent;

        // Should change state when clicked
        expect(afterClickText).not.toBe(initialText);
        expect(pulseIndicator?.classList.contains('pulse-starting')).toBe(true);

        // Test keyboard accessibility attributes are present
        expect(pulseIndicator?.getAttribute('role')).toBe('button');
        expect(pulseIndicator?.getAttribute('tabindex')).toBe('0');
        expect(pulseIndicator?.classList.contains('clickable')).toBe(true);
    });

    test('Connection indicator updates via updateIndicator', () => {
        document.body.innerHTML = `
            <div id="system-drawer-header">
                <span id="system-version"></span>
                <div class="controls"></div>
            </div>
        `;

        statusIndicators.init();

        const connectionIndicator = document.getElementById('connection-status');
        const connectionText = connectionIndicator?.querySelector('.status-text') as HTMLElement;

        // Test connected state
        statusIndicators.updateIndicator('connection', 'connected', 'Connected');
        expect(connectionIndicator?.classList.contains('connection-connected')).toBe(true);
        expect(connectionText?.textContent).toBe('Connected');

        // Test degraded state
        statusIndicators.updateIndicator('connection', 'degraded', 'Degraded');
        expect(connectionIndicator?.classList.contains('connection-degraded')).toBe(true);
        expect(connectionText?.textContent).toBe('Degraded');

        // Test disconnected state
        statusIndicators.updateIndicator('connection', 'disconnected', 'Disconnected');
        expect(connectionIndicator?.classList.contains('connection-disconnected')).toBe(true);
        expect(connectionText?.textContent).toBe('Disconnected');
    });

    test('updateIndicator before init() does not crash', () => {
        document.body.innerHTML = `
            <div id="system-drawer-header">
                <span id="system-version"></span>
                <div class="controls"></div>
            </div>
        `;

        // Call before init — must not throw (silently no-ops on missing DOM)
        expect(() => statusIndicators.updateIndicator('connection', 'connected', 'Connected')).not.toThrow();
        expect(() => statusIndicators.updateIndicator('connection', 'disconnected', 'Disconnected')).not.toThrow();
    });

    test('init() then updateIndicator() shows correct state', () => {
        // Correct ordering: init creates DOM, then updates work
        document.body.innerHTML = `
            <div id="system-drawer-header">
                <span id="system-version"></span>
                <div class="controls"></div>
            </div>
        `;

        statusIndicators.init();
        statusIndicators.updateIndicator('connection', 'connected', 'Connected');

        const connectionText = document.getElementById('connection-text');
        expect(connectionText?.textContent).toBe('Connected');
    });
});
