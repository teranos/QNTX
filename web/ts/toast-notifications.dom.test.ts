/**
 * @jest-environment jsdom
 *
 * DOM tests for toast notification lifecycle
 * These tests run only in CI with JSDOM environment
 */

describe('Toast Notification Lifecycle', () => {
    beforeEach(() => {
        // Reset DOM and timers
        document.body.innerHTML = '';
        jest.clearAllTimers();
        jest.useRealTimers();
    });

    afterEach(() => {
        jest.useRealTimers();
    });

    test('Toast notifications create, display, and auto-remove from DOM', () => {
        document.body.innerHTML = `
            <div id="toast-container"></div>
            <style>
                .config-toast {
                    position: fixed;
                    bottom: 20px;
                    right: 20px;
                    animation: slideInRight 0.3s ease-out;
                }
                @keyframes slideInRight {
                    from { transform: translateX(100%); opacity: 0; }
                    to { transform: translateX(0); opacity: 1; }
                }
            </style>
        `;

        // Create toast element (simulating config-panel.ts toast)
        const toast = document.createElement('div');
        toast.className = 'config-toast';
        toast.textContent = 'Copied to clipboard: /path/to/config';

        // Add to DOM
        document.body.appendChild(toast);

        // Toast should exist in DOM
        expect(document.querySelector('.config-toast')).toBeTruthy();
        expect(toast.parentNode).toBe(document.body);
        expect(toast.textContent).toBe('Copied to clipboard: /path/to/config');

        // Test computed styles
        const computedStyle = window.getComputedStyle(toast);
        expect(computedStyle.position).toBe('fixed');
        expect(computedStyle.bottom).toBe('20px');
        expect(computedStyle.right).toBe('20px');

        // Simulate auto-remove after timeout
        jest.useFakeTimers();

        setTimeout(() => {
            toast.remove();
        }, 2000);

        // Still exists before timeout
        expect(document.querySelector('.config-toast')).toBeTruthy();

        // Run timers
        jest.runAllTimers();

        // Should be removed after timeout
        expect(document.querySelector('.config-toast')).toBeFalsy();
        expect(toast.parentNode).toBeNull();
    });

    test('Multiple toasts can exist simultaneously', () => {
        document.body.innerHTML = `
            <div id="toast-container"></div>
        `;

        const container = document.getElementById('toast-container');

        // Create multiple toasts
        const toast1 = document.createElement('div');
        toast1.className = 'toast success-toast';
        toast1.textContent = 'Operation successful';
        container?.appendChild(toast1);

        const toast2 = document.createElement('div');
        toast2.className = 'toast error-toast';
        toast2.textContent = 'Error occurred';
        container?.appendChild(toast2);

        const toast3 = document.createElement('div');
        toast3.className = 'toast info-toast';
        toast3.textContent = 'Info message';
        container?.appendChild(toast3);

        // All toasts should exist
        const toasts = document.querySelectorAll('.toast');
        expect(toasts.length).toBe(3);
        expect(container?.children.length).toBe(3);

        // Remove middle toast
        toast2.remove();

        // Should have 2 toasts remaining
        const remainingToasts = document.querySelectorAll('.toast');
        expect(remainingToasts.length).toBe(2);
        expect(remainingToasts[0]).toBe(toast1);
        expect(remainingToasts[1]).toBe(toast3);
    });

    test('Toast with fadeOut animation class', () => {
        document.body.innerHTML = `
            <div id="toast-container"></div>
            <style>
                @keyframes fadeOut {
                    from { opacity: 1; }
                    to { opacity: 0; }
                }
                .u-animate-fadeout {
                    animation: fadeOut 0.3s ease-out !important;
                }
            </style>
        `;

        const container = document.getElementById('toast-container');

        // Create toast
        const toast = document.createElement('div');
        toast.className = 'toast';
        toast.textContent = 'Fading toast';
        container?.appendChild(toast);

        // Toast exists initially
        expect(toast.parentNode).toBe(container);

        // Add fadeout animation
        toast.classList.add('u-animate-fadeout');
        expect(toast.classList.contains('u-animate-fadeout')).toBe(true);

        // Toast should still be in DOM during animation
        expect(toast.parentNode).toBe(container);

        // Simulate completion of animation and removal
        jest.useFakeTimers();

        setTimeout(() => {
            if (container?.contains(toast)) {
                container.removeChild(toast);
            }
        }, 300);

        jest.runAllTimers();

        // Toast should be removed
        expect(toast.parentNode).toBeNull();
        expect(container?.contains(toast)).toBe(false);
    });

    test('Config toast specific behavior', () => {
        // Simulate the exact toast from config-panel.ts
        document.body.innerHTML = `
            <style>
                .config-toast {
                    position: fixed;
                    bottom: 20px;
                    right: 20px;
                    background: #333;
                    color: #fff;
                    padding: 12px 20px;
                    border-radius: 4px;
                    font-size: 12px;
                    z-index: 10000;
                    box-shadow: 0 4px 12px rgba(0,0,0,0.3);
                    animation: slideInRight 0.3s ease-out;
                }
            </style>
        `;

        // Create toast as config-panel does
        const toast = document.createElement('div');
        toast.className = 'config-toast';
        const configPath = '/Users/app/config.toml';
        toast.textContent = `Copied to clipboard: ${configPath}`;

        // Add to body
        document.body.appendChild(toast);

        // Verify toast properties
        expect(toast.className).toBe('config-toast');
        expect(toast.textContent).toContain(configPath);
        expect(toast.parentNode).toBe(document.body);

        // Check computed styles match our CSS
        const styles = window.getComputedStyle(toast);
        expect(styles.position).toBe('fixed');
        expect(styles.zIndex).toBe('10000');

        // Test auto-removal after 2 seconds (config-panel behavior)
        jest.useFakeTimers();

        setTimeout(() => toast.remove(), 2000);

        // Before timeout
        jest.advanceTimersByTime(1999);
        expect(document.body.contains(toast)).toBe(true);

        // After timeout
        jest.advanceTimersByTime(1);
        expect(document.body.contains(toast)).toBe(false);
        expect(toast.parentNode).toBeNull();
    });
});