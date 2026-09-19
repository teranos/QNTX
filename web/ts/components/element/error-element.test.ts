/**
 * Tests for error elements
 *
 * Personas:
 * - Tim: Happy path user, normal workflows
 * - Spike: Tries to break things, edge cases
 * - Jenny: Power user, complex scenarios
 */

import { describe, test, expect, mock } from 'bun:test';
import { createErrorElement } from './error-element';

// Mock ResizeObserver for tests
globalThis.ResizeObserver = class ResizeObserver {
    observe() {}
    unobserve() {}
    disconnect() {}
} as any;

// Mock uiState — process-global, must be superset-complete (see test/mock-ui-state.ts)
import { createMockUiState } from '../../test/mock-ui-state';
const { uiState } = createMockUiState();
mock.module('../../state/ui', () => ({ uiState }));

describe('Error Element - Tim (Happy Path)', () => {
    test('Tim sees error element for failed result rendering', () => {
        // Tim has a canvas
        const container = document.createElement('div');
        container.className = 'canvas-workspace';

        // A result element fails to render and error element appears
        const errorElement = createErrorElement(
            'result-789',
            'result',
            { x: 100, y: 100 },
            {
                type: 'missing_data',
                message: 'Result element missing execution data',
                details: { elementId: 'result-789' }
            }
        );

        container.appendChild(errorElement);

        // Error element is visible
        expect(errorElement.classList.contains('canvas-error-element')).toBe(true);
        expect(errorElement.dataset.symbol).toBe('error');

        // Shows diagnostic information
        const content = errorElement.querySelector('.error-element-content');
        expect(content).toBeTruthy();
        expect(content?.textContent).toContain('Failed Element: result');
        expect(content?.textContent).toContain('missing_data');
    });

    test('Tim clicks copy button to get error details', () => {
        const container = document.createElement('div');
        container.className = 'canvas-workspace';

        const errorElement = createErrorElement(
            'py-456',
            'py',
            { x: 200, y: 200 },
            {
                type: 'parse_failed',
                message: 'Failed to parse Python code'
            }
        );

        container.appendChild(errorElement);

        // Copy button exists
        const copyBtn = errorElement.querySelector('button[title="Copy error details"]');
        expect(copyBtn).toBeTruthy();
        expect(copyBtn?.textContent).toBe('📋');
    });

    test('Tim dismisses error element with X button', () => {
        const container = document.createElement('div');
        container.className = 'canvas-workspace';

        const errorElement = createErrorElement(
            'note-111',
            'prose',
            { x: 50, y: 50 },
            {
                type: 'render_error',
                message: 'Failed to render note'
            }
        );

        container.appendChild(errorElement);

        // Dismiss button exists
        const dismissBtn = errorElement.querySelector('button[title*="Dismiss"]');
        expect(dismissBtn).toBeTruthy();
        expect(dismissBtn?.textContent).toBe('✕');
    });
});

describe('Error Element - Spike (Edge Cases)', () => {
    test('Spike creates error element with empty error details', () => {
        const container = document.createElement('div');
        container.className = 'canvas-workspace';

        const errorElement = createErrorElement(
            'element-999',
            'unknown',
            { x: 0, y: 0 },
            {
                type: '',
                message: ''
            }
        );

        container.appendChild(errorElement);

        // Error element still renders
        expect(errorElement.classList.contains('canvas-error-element')).toBe(true);

        const content = errorElement.querySelector('.error-element-content');
        expect(content).toBeTruthy();
    });

    test('Spike creates error element with huge details object', () => {
        const container = document.createElement('div');
        container.className = 'canvas-workspace';

        const hugeDetails: Record<string, unknown> = {};
        for (let i = 0; i < 100; i++) {
            hugeDetails[`key${i}`] = `value${i}`.repeat(50);
        }

        const errorElement = createErrorElement(
            'element-huge',
            'result',
            { x: 100, y: 100 },
            {
                type: 'huge_error',
                message: 'Error with massive details',
                details: hugeDetails
            }
        );

        container.appendChild(errorElement);

        // Error element handles large data
        expect(errorElement.classList.contains('canvas-error-element')).toBe(true);
        const content = errorElement.querySelector('.error-element-content');
        expect(content).toBeTruthy();
    });
});

describe('Error Element - Jenny (Complex Scenarios)', () => {
    test('Jenny rapidly clicks dismiss button twice', () => {
        // Jenny creates an error element
        const container = document.createElement('div');
        container.className = 'canvas-workspace';
        document.body.appendChild(container);

        const errorElement = createErrorElement(
            'result-double-click',
            'result',
            { x: 100, y: 100 },
            {
                type: 'test_error',
                message: 'Test error for rapid dismiss'
            }
        );
        container.appendChild(errorElement);

        // Verify error element is in DOM
        expect(container.contains(errorElement)).toBe(true);

        // Get dismiss button
        const dismissBtn = errorElement.querySelector('button[title*="Dismiss"]') as HTMLButtonElement;
        expect(dismissBtn).toBeTruthy();

        // Jenny clicks dismiss twice rapidly
        dismissBtn.click();

        // After first click, error element should be removed
        expect(container.contains(errorElement)).toBe(false);

        // Second click on already-removed element should not throw
        expect(() => {
            dismissBtn.click();
        }).not.toThrow();

        // Error element remains removed (no resurrection)
        expect(container.contains(errorElement)).toBe(false);
        expect(container.querySelectorAll('.canvas-error-element').length).toBe(0);

        // Cleanup
        document.body.innerHTML = '';
    });
});
