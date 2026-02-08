/**
 * Tests for result glyph component
 * Focus: API contract, structure, and behavior (not styling)
 */

import { describe, test, expect, beforeEach } from 'bun:test';
import { createResultGlyph, type ExecutionResult } from './result-glyph';
import type { Glyph } from './glyph';

describe('ResultGlyph', () => {
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
            const element = createResultGlyph(glyph, result);
            container.appendChild(element);

            const closeBtn = element.querySelector('button[title="Close result"]') as HTMLElement;
            closeBtn?.click();

            expect(container.contains(element)).toBe(false);
        });
    });
});
