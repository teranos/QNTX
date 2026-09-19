import { describe, test, expect } from 'bun:test';
import { setResponseState } from './response-state';

function elementWithTitleBar(): { element: HTMLElement; titleBar: HTMLElement } {
    const element = document.createElement('div');
    element.style.backgroundColor = 'rgb(10, 20, 30)';
    const titleBar = document.createElement('div');
    titleBar.className = 'title-bar';
    titleBar.style.backgroundColor = 'rgb(40, 50, 60)';
    element.appendChild(titleBar);
    return { element, titleBar };
}

describe('setResponseState', () => {
    test('error tints element, title bar, and outline, and stamps the attribute', () => {
        const { element, titleBar } = elementWithTitleBar();
        setResponseState(element, 'error');

        expect(element.dataset.responseState).toBe('error');
        expect(element.style.backgroundColor).toBe('var(--element-status-error-bg)');
        expect(element.style.outlineColor).toBe('var(--element-status-error-text)');
        expect(titleBar.style.backgroundColor).toBe('var(--element-status-error-section-bg)');
    });

    test('clear restores the identity colors the tint replaced', () => {
        const { element, titleBar } = elementWithTitleBar();
        setResponseState(element, 'error');
        setResponseState(element, null);

        expect(element.dataset.responseState).toBeUndefined();
        expect(element.style.backgroundColor).toBe('rgb(10, 20, 30)');
        expect(titleBar.style.backgroundColor).toBe('rgb(40, 50, 60)');
    });

    test('error over warning keeps the original identity for the eventual clear', () => {
        const { element } = elementWithTitleBar();
        setResponseState(element, 'warning');
        setResponseState(element, 'error');

        expect(element.style.backgroundColor).toBe('var(--element-status-error-bg)');
        setResponseState(element, null);
        expect(element.style.backgroundColor).toBe('rgb(10, 20, 30)');
    });

    test('clear with no tint standing changes nothing', () => {
        const { element } = elementWithTitleBar();
        setResponseState(element, null);
        expect(element.style.backgroundColor).toBe('rgb(10, 20, 30)');
    });

    test('an element without a title bar tints and restores', () => {
        const element = document.createElement('div');
        element.style.backgroundColor = 'rgb(1, 2, 3)';
        setResponseState(element, 'error');
        expect(element.style.backgroundColor).toBe('var(--element-status-error-bg)');
        setResponseState(element, null);
        expect(element.style.backgroundColor).toBe('rgb(1, 2, 3)');
    });
});
