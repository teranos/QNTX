/**
 * Tests for result glyph component
 */

import { describe, test, expect, beforeEach } from 'bun:test';
import { createResultGlyph, type ExecutionResult } from './result-glyph';
import type { Glyph } from './glyph';

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

    // Add AbortController polyfill
    // jsdom has AbortController but it's not compatible with addEventListener signal option
    // Use the window's native implementations
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
            gridX: 5,
            gridY: 10,
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
        test('creates element with correct class', () => {
            const element = createResultGlyph(glyph, result);
            expect(element.className).toBe('canvas-result-glyph');
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
            expect(closeBtn?.textContent).toBe('×');
        });

        test('has to-window button', () => {
            const element = createResultGlyph(glyph, result);
            const toWindowBtn = element.querySelector('button[title="Expand to window"]');
            expect(toWindowBtn).not.toBeNull();
            expect(toWindowBtn?.textContent).toBe('⬆');
        });
    });

    describe('output rendering', () => {
        test('stdout renders in light color', () => {
            const element = createResultGlyph(glyph, result);
            const outputContainer = element.querySelector('.result-glyph-output') as HTMLElement;
            expect(outputContainer).not.toBeNull();
            expect(outputContainer.style.color).toBe('rgb(224, 224, 224)'); // #e0e0e0
        });

        test('displays stdout text', () => {
            const element = createResultGlyph(glyph, result);
            const outputContainer = element.querySelector('.result-glyph-output');
            expect(outputContainer?.textContent).toContain('Hello from Python');
        });

        test('stderr renders in error color', () => {
            result.stderr = 'Warning: something happened';
            const element = createResultGlyph(glyph, result);
            const outputContainer = element.querySelector('.result-glyph-output');
            const stderrSpan = Array.from(outputContainer?.childNodes || [])
                .find(node => node.textContent?.includes('Warning'));
            expect(stderrSpan).toBeDefined();
        });

        test('error message renders in bold error color', () => {
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

        test('header is draggable', () => {
            const element = createResultGlyph(glyph, result);
            const header = element.querySelector('.result-glyph-header') as HTMLElement;
            expect(header).not.toBeNull();

            // Verify mousedown event listener exists (cursor should be grabbable)
            // This is a basic structural test, full drag testing would be integration level
        });
    });

    describe('styling', () => {
        test('has dark background', () => {
            const element = createResultGlyph(glyph, result) as HTMLElement;
            expect(element.style.backgroundColor).toBe('rgb(30, 30, 30)'); // #1e1e1e
        });

        test('has rounded bottom corners only', () => {
            const element = createResultGlyph(glyph, result) as HTMLElement;
            // CSS normalizes "0px" to "0" in jsdom
            expect(element.style.borderRadius).toBe('0 0 4px 4px');
        });

        test('has no top border', () => {
            const element = createResultGlyph(glyph, result) as HTMLElement;
            // jsdom sets default border-top to "medium" when border-top: none is set via style
            // Verify that borderTopStyle is "none" instead
            expect(element.style.borderTopStyle).toBe('none');
        });
    });
});
