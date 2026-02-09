/**
 * Tests for result glyph component
 * Focus: API contract, structure, and behavior (not styling)
 */

import { describe, test, expect, beforeEach } from 'bun:test';
import { createResultGlyph, type ExecutionResult } from './result-glyph';
import type { Glyph } from './glyph';
import { performMeld } from './meld/meld-composition';
import { uiState } from '../../state/ui';

// Only run these tests when USE_JSDOM=1 (CI environment)
const USE_JSDOM = process.env.USE_JSDOM === '1';

// Setup jsdom if enabled
if (USE_JSDOM) {
    const { JSDOM } = await import('jsdom');
    const dom = new JSDOM('<!DOCTYPE html><html><body></body></html>', {
        url: 'http://localhost'
    });
    const { window } = dom;
    const { document } = window;

    globalThis.document = document as any;
    globalThis.window = window as any;
    globalThis.localStorage = window.localStorage as any;

    // jsdom's AbortController is compatible with addEventListener signal option
    globalThis.AbortController = window.AbortController as any;
    globalThis.AbortSignal = window.AbortSignal as any;
}

describe('ResultGlyph', () => {
    if (!USE_JSDOM) {
        test.skip('Skipped locally (run with USE_JSDOM=1 to enable)', () => {});
        return;
    }

    let glyph: Glyph;
    let result: ExecutionResult;

    beforeEach(() => {
        glyph = {
            id: 'result-test-123',
            title: 'Python Result',
            symbol: 'result',
            width: 400,
            renderContent: () => document.createElement('div')
        };

        result = {
            success: true,
            stdout: 'Hello from Python\n',
            stderr: '',
            result: null,
            error: null,
            duration_ms: 42
        };
    });

    describe('rendering', () => {
        test('creates element with result and base glyph classes', () => {
            const element = createResultGlyph(glyph, result);
            expect(element.classList.contains('canvas-result-glyph')).toBe(true);
            expect(element.classList.contains('canvas-glyph')).toBe(true);
        });

        test('sets data-glyph-id attribute', () => {
            const element = createResultGlyph(glyph, result);
            expect(element.dataset.glyphId).toBe('result-test-123');
        });

        test('has header with duration', () => {
            const element = createResultGlyph(glyph, result);
            const header = element.querySelector('.result-glyph-header');
            expect(header).not.toBeNull();
            expect(header?.textContent).toContain('42ms');
        });

        test('has close button', () => {
            const element = createResultGlyph(glyph, result);
            const closeBtn = element.querySelector('button[title="Close result"]');
            expect(closeBtn).not.toBeNull();
        });

        test('has to-window button', () => {
            const element = createResultGlyph(glyph, result);
            const toWindowBtn = element.querySelector('button[title="Expand to window"]');
            expect(toWindowBtn).not.toBeNull();
        });
    });

    describe('output rendering', () => {
        test('displays stdout text', () => {
            const element = createResultGlyph(glyph, result);
            const outputContainer = element.querySelector('.result-glyph-output');
            expect(outputContainer).not.toBeNull();
            expect(outputContainer?.textContent).toContain('Hello from Python');
        });

        test('displays stderr text', () => {
            result.stderr = 'Warning: something happened';
            const element = createResultGlyph(glyph, result);
            const outputContainer = element.querySelector('.result-glyph-output');
            expect(outputContainer?.textContent).toContain('Warning: something happened');
        });

        test('displays error message', () => {
            result.error = 'RuntimeError: test error';
            const element = createResultGlyph(glyph, result);
            const outputContainer = element.querySelector('.result-glyph-output');
            expect(outputContainer?.textContent).toContain('Error: RuntimeError');
        });

        test('shows placeholder for empty output', () => {
            result.stdout = '';
            result.stderr = '';
            result.error = null;
            const element = createResultGlyph(glyph, result);
            const outputContainer = element.querySelector('.result-glyph-output');
            expect(outputContainer?.textContent).toBe('(no output)');
        });
    });

    describe('interactions', () => {
        test('close button removes element from DOM', () => {
            const container = document.createElement('div');
            document.body.appendChild(container);

            const element = createResultGlyph(glyph, result);
            container.appendChild(element);

            const closeBtn = element.querySelector('button[title="Close result"]') as HTMLElement;
            closeBtn?.click();

            expect(container.contains(element)).toBe(false);

            // Cleanup
            document.body.removeChild(container);
        });

        test('close button unmelds composition when result is in composition', () => {
            const canvas = document.createElement('div');
            canvas.className = 'canvas-workspace';
            document.body.appendChild(canvas);

            // Create py glyph
            const pyElement = document.createElement('div');
            pyElement.className = 'canvas-py-glyph';
            pyElement.setAttribute('data-glyph-id', 'py-test');
            pyElement.setAttribute('data-glyph-symbol', 'py');
            pyElement.style.position = 'absolute';
            pyElement.style.left = '100px';
            pyElement.style.top = '100px';
            canvas.appendChild(pyElement);

            const pyGlyph: Glyph = {
                id: 'py-test',
                title: 'Python',
                symbol: 'py',
                renderContent: () => pyElement
            };

            // Create result glyph
            const resultElement = createResultGlyph(glyph, result);
            canvas.appendChild(resultElement);

            // Meld them together (py on top, result below)
            const composition = performMeld(pyElement, resultElement, pyGlyph, glyph, 'bottom');

            // Verify composition exists
            expect(composition.classList.contains('melded-composition')).toBe(true);
            expect(composition.contains(pyElement)).toBe(true);
            expect(composition.contains(resultElement)).toBe(true);

            // Close the result glyph
            const closeBtn = resultElement.querySelector('button[title="Close result"]') as HTMLElement;
            closeBtn?.click();

            // Verify composition was unmelded
            expect(canvas.querySelector('.melded-composition')).toBeNull();

            // Verify py glyph is restored to canvas as standalone
            expect(canvas.contains(pyElement)).toBe(true);
            expect(pyElement.style.position).toBe('absolute');

            // Verify result glyph is removed
            expect(canvas.contains(resultElement)).toBe(false);

            // Cleanup
            uiState.setCanvasCompositions([]);
            document.body.removeChild(canvas);
        });
    });
});
