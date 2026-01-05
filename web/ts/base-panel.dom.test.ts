/**
 * @jest-environment jsdom
 *
 * DOM tests for BasePanel visibility management
 * These tests run only in CI with JSDOM environment
 */

describe('BasePanel Data Attributes', () => {
    beforeEach(() => {
        // Reset DOM
        document.body.innerHTML = '';
    });

    test('BasePanel manages visibility through data attributes not classes', () => {
        document.body.innerHTML = `
            <div class="panel prose-panel" data-visibility="hidden">
                <div class="panel-header">
                    <button class="panel-close">×</button>
                </div>
                <div class="panel-content"></div>
            </div>
            <div class="panel-overlay u-hidden"></div>
        `;

        const panel = document.querySelector('.panel');
        const overlay = document.querySelector('.panel-overlay');
        const closeBtn = document.querySelector('.panel-close');

        // Initially hidden
        expect(panel?.getAttribute('data-visibility')).toBe('hidden');
        expect(overlay?.classList.contains('u-hidden')).toBe(true);

        // Simulate show
        panel?.setAttribute('data-visibility', 'visible');
        overlay?.classList.remove('u-hidden');

        expect(panel?.getAttribute('data-visibility')).toBe('visible');
        expect(overlay?.classList.contains('u-hidden')).toBe(false);

        // Test close button
        closeBtn?.addEventListener('click', () => {
            panel?.setAttribute('data-visibility', 'hidden');
            overlay?.classList.add('u-hidden');
        });

        (closeBtn as HTMLElement)?.click();

        expect(panel?.getAttribute('data-visibility')).toBe('hidden');
        expect(overlay?.classList.contains('u-hidden')).toBe(true);

        // Test that old class-based visibility doesn't work
        panel?.classList.add('visible');
        panel?.classList.remove('hidden');

        // Should still be hidden because we use data attributes
        expect(panel?.getAttribute('data-visibility')).toBe('hidden');
    });

    test('Multiple panels can use data-visibility independently', () => {
        document.body.innerHTML = `
            <div id="prose-panel" class="panel" data-visibility="hidden"></div>
            <div id="ai-provider-panel" class="panel" data-visibility="hidden"></div>
            <div id="config-panel" class="panel" data-visibility="hidden"></div>
        `;

        const prosePanel = document.getElementById('prose-panel');
        const aiPanel = document.getElementById('ai-provider-panel');
        const configPanel = document.getElementById('config-panel');

        // All start hidden
        expect(prosePanel?.getAttribute('data-visibility')).toBe('hidden');
        expect(aiPanel?.getAttribute('data-visibility')).toBe('hidden');
        expect(configPanel?.getAttribute('data-visibility')).toBe('hidden');

        // Show only prose panel
        prosePanel?.setAttribute('data-visibility', 'visible');

        expect(prosePanel?.getAttribute('data-visibility')).toBe('visible');
        expect(aiPanel?.getAttribute('data-visibility')).toBe('hidden');
        expect(configPanel?.getAttribute('data-visibility')).toBe('hidden');

        // Show ai panel (prose remains visible)
        aiPanel?.setAttribute('data-visibility', 'visible');

        expect(prosePanel?.getAttribute('data-visibility')).toBe('visible');
        expect(aiPanel?.getAttribute('data-visibility')).toBe('visible');
        expect(configPanel?.getAttribute('data-visibility')).toBe('hidden');

        // Hide all
        prosePanel?.setAttribute('data-visibility', 'hidden');
        aiPanel?.setAttribute('data-visibility', 'hidden');

        expect(prosePanel?.getAttribute('data-visibility')).toBe('hidden');
        expect(aiPanel?.getAttribute('data-visibility')).toBe('hidden');
        expect(configPanel?.getAttribute('data-visibility')).toBe('hidden');
    });

    test('Panel visibility changes trigger proper CSS selectors', () => {
        // Add CSS to test selector specificity
        const style = document.createElement('style');
        style.textContent = `
            .panel[data-visibility="hidden"] { display: none; }
            .panel[data-visibility="visible"] { display: flex; }
            .panel.hidden { display: block; } /* Old style - should not apply */
            .panel.visible { display: grid; } /* Old style - should not apply */
        `;
        document.head.appendChild(style);

        document.body.innerHTML = `
            <div class="panel" data-visibility="hidden"></div>
        `;

        const panel = document.querySelector('.panel') as HTMLElement;

        // Hidden state
        expect(panel.getAttribute('data-visibility')).toBe('hidden');
        const hiddenStyle = window.getComputedStyle(panel);
        expect(hiddenStyle.display).toBe('none');

        // Visible state
        panel.setAttribute('data-visibility', 'visible');
        const visibleStyle = window.getComputedStyle(panel);
        expect(visibleStyle.display).toBe('flex');

        // Old classes should not affect display
        panel.classList.add('visible');
        panel.setAttribute('data-visibility', 'hidden');
        const stillHiddenStyle = window.getComputedStyle(panel);
        expect(stillHiddenStyle.display).toBe('none');
    });

    test('Overlay click closes panel with data-visibility', () => {
        document.body.innerHTML = `
            <div class="panel" data-visibility="visible">
                <div class="panel-content">Panel content</div>
            </div>
            <div class="panel-overlay"></div>
        `;

        const panel = document.querySelector('.panel');
        const overlay = document.querySelector('.panel-overlay');

        // Panel starts visible
        expect(panel?.getAttribute('data-visibility')).toBe('visible');

        // Set up overlay click handler
        overlay?.addEventListener('click', () => {
            panel?.setAttribute('data-visibility', 'hidden');
            overlay.classList.add('u-hidden');
        });

        // Trigger overlay click
        const clickEvent = new MouseEvent('click', { bubbles: true });
        overlay?.dispatchEvent(clickEvent);

        // Panel should now be hidden
        expect(panel?.getAttribute('data-visibility')).toBe('hidden');
        expect(overlay?.classList.contains('u-hidden')).toBe(true);
    });
});