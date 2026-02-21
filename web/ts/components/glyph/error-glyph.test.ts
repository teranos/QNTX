/**
 * Tests for error glyphs
 *
 * Personas:
 * - Tim: Happy path user, normal workflows
 * - Spike: Tries to break things, edge cases
 * - Jenny: Power user, complex scenarios
 */

import { describe, test, expect, mock } from 'bun:test';
import { createErrorGlyph } from './error-glyph';

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

describe('Error Glyph - Tim (Happy Path)', () => {
    test('Tim sees error glyph for failed result rendering', () => {
        // Tim has a canvas
        const container = document.createElement('div');
        container.className = 'canvas-workspace';

        // A result glyph fails to render and error glyph appears
        const errorGlyph = createErrorGlyph(
            'result-789',
            'result',
            { x: 100, y: 100 },
            {
                type: 'missing_data',
                message: 'Result glyph missing execution data',
                details: { glyphId: 'result-789' }
            }
        );

        container.appendChild(errorGlyph);

        // Error glyph is visible
        expect(errorGlyph.classList.contains('canvas-error-glyph')).toBe(true);
        expect(errorGlyph.dataset.glyphSymbol).toBe('error');

        // Shows diagnostic information
        const content = errorGlyph.querySelector('.error-glyph-content');
        expect(content).toBeTruthy();
        expect(content?.textContent).toContain('Failed Glyph: result');
        expect(content?.textContent).toContain('missing_data');
    });

    test('Tim clicks copy button to get error details', () => {
        const container = document.createElement('div');
        container.className = 'canvas-workspace';

        const errorGlyph = createErrorGlyph(
            'py-456',
            'py',
            { x: 200, y: 200 },
            {
                type: 'parse_failed',
                message: 'Failed to parse Python code'
            }
        );

        container.appendChild(errorGlyph);

        // Copy button exists
        const copyBtn = errorGlyph.querySelector('button[title="Copy error details"]');
        expect(copyBtn).toBeTruthy();
        expect(copyBtn?.textContent).toBe('📋');
    });

    test('Tim dismisses error glyph with X button', () => {
        const container = document.createElement('div');
        container.className = 'canvas-workspace';

        const errorGlyph = createErrorGlyph(
            'note-111',
            'prose',
            { x: 50, y: 50 },
            {
                type: 'render_error',
                message: 'Failed to render note'
            }
        );

        container.appendChild(errorGlyph);

        // Dismiss button exists
        const dismissBtn = errorGlyph.querySelector('button[title*="Dismiss"]');
        expect(dismissBtn).toBeTruthy();
        expect(dismissBtn?.textContent).toBe('✕');
    });
});

describe('Error Glyph - Spike (Edge Cases)', () => {
    test('Spike creates error glyph with empty error details', () => {
        const container = document.createElement('div');
        container.className = 'canvas-workspace';

        const errorGlyph = createErrorGlyph(
            'glyph-999',
            'unknown',
            { x: 0, y: 0 },
            {
                type: '',
                message: ''
            }
        );

        container.appendChild(errorGlyph);

        // Error glyph still renders
        expect(errorGlyph.classList.contains('canvas-error-glyph')).toBe(true);

        const content = errorGlyph.querySelector('.error-glyph-content');
        expect(content).toBeTruthy();
    });

    test('Spike creates error glyph with huge details object', () => {
        const container = document.createElement('div');
        container.className = 'canvas-workspace';

        const hugeDetails: Record<string, unknown> = {};
        for (let i = 0; i < 100; i++) {
            hugeDetails[`key${i}`] = `value${i}`.repeat(50);
        }

        const errorGlyph = createErrorGlyph(
            'glyph-huge',
            'result',
            { x: 100, y: 100 },
            {
                type: 'huge_error',
                message: 'Error with massive details',
                details: hugeDetails
            }
        );

        container.appendChild(errorGlyph);

        // Error glyph handles large data
        expect(errorGlyph.classList.contains('canvas-error-glyph')).toBe(true);
        const content = errorGlyph.querySelector('.error-glyph-content');
        expect(content).toBeTruthy();
    });
});

describe('Error Glyph - Jenny (Complex Scenarios)', () => {
    test('Jenny rapidly clicks dismiss button twice', () => {
        // Jenny creates an error glyph
        const container = document.createElement('div');
        container.className = 'canvas-workspace';
        document.body.appendChild(container);

        const errorGlyph = createErrorGlyph(
            'result-double-click',
            'result',
            { x: 100, y: 100 },
            {
                type: 'test_error',
                message: 'Test error for rapid dismiss'
            }
        );
        container.appendChild(errorGlyph);

        // Verify error glyph is in DOM
        expect(container.contains(errorGlyph)).toBe(true);

        // Get dismiss button
        const dismissBtn = errorGlyph.querySelector('button[title*="Dismiss"]') as HTMLButtonElement;
        expect(dismissBtn).toBeTruthy();

        // Jenny clicks dismiss twice rapidly
        dismissBtn.click();

        // After first click, error glyph should be removed
        expect(container.contains(errorGlyph)).toBe(false);

        // Second click on already-removed element should not throw
        expect(() => {
            dismissBtn.click();
        }).not.toThrow();

        // Error glyph remains removed (no resurrection)
        expect(container.contains(errorGlyph)).toBe(false);
        expect(container.querySelectorAll('.canvas-error-glyph').length).toBe(0);

        // Cleanup
        document.body.innerHTML = '';
    });
});
