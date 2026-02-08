/**
 * Tests for error glyphs
 *
 * Personas:
 * - Tim: Happy path user, normal workflows
 * - Spike: Tries to break things, edge cases
 * - Jenny: Power user, complex scenarios
 */

import { describe, test, expect } from 'bun:test';
import { Window } from 'happy-dom';
import { createErrorGlyph } from './error-glyph';

// Setup happy-dom
const window = new Window();
const document = window.document;
globalThis.document = document as any;
globalThis.window = window as any;
globalThis.localStorage = window.localStorage;

// Mock ResizeObserver for tests
globalThis.ResizeObserver = class ResizeObserver {
    observe() {}
    unobserve() {}
    disconnect() {}
} as any;

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
