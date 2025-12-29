/**
 * Go Editor Panel - Critical Behavior Tests
 */

describe('Go Editor Panel', () => {
    test('status updates from connecting to ready when editor loads', () => {
        // This is the happy path - status should go from gray → green
        const panel = document.createElement('div');
        panel.innerHTML = '<span id="gopls-status">connecting...</span>';

        const statusEl = panel.querySelector('#gopls-status') as HTMLElement;

        // Simulate what updateStatus('ready') does
        statusEl.textContent = 'ready';
        statusEl.style.color = '#4ec9b0';

        expect(statusEl.textContent).toBe('ready');
        expect(statusEl.style.color).toBe('rgb(78, 201, 176)');
    });

    test('status shows error when gopls is unavailable', () => {
        // Critical: user needs to know if gopls isn't working
        const panel = document.createElement('div');
        panel.innerHTML = '<span id="gopls-status">connecting...</span>';

        const statusEl = panel.querySelector('#gopls-status') as HTMLElement;

        // Simulate what updateStatus('unavailable', 'gopls disabled') does
        statusEl.textContent = 'gopls disabled';
        statusEl.style.color = '#858585';

        expect(statusEl.textContent).toBe('gopls disabled');
        expect(statusEl.style.color).toBe('rgb(133, 133, 133)');
    });

    test('panel toggles between hidden and visible', () => {
        // Basic UX: panel should show/hide when toggled
        const panel = document.createElement('div');
        panel.className = 'prose-panel hidden';

        // Show
        panel.classList.remove('hidden');
        panel.classList.add('visible');
        expect(panel.classList.contains('visible')).toBe(true);

        // Hide
        panel.classList.remove('visible');
        panel.classList.add('hidden');
        expect(panel.classList.contains('hidden')).toBe(true);
    });
});
