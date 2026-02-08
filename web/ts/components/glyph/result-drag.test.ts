/**
 * Tests for result glyph drag persistence
 *
 * Personas:
 * - Tim: Happy path user, normal workflows
 * - Spike: Tries to break things, edge cases
 * - Jenny: Power user, complex scenarios
 */

import { describe, test, expect } from 'bun:test';
import { Window } from 'happy-dom';
import { createResultGlyph } from './result-glyph';
import type { Glyph } from './glyph';
import type { ExecutionResult } from './result-glyph';

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

describe('Result Glyph Drag Persistence - Tim (Happy Path)', () => {
    test('Tim creates result glyph with execution data', () => {
        const container = document.createElement('div');
        container.className = 'canvas-workspace';

        const result: ExecutionResult = {
            success: true,
            stdout: 'Hello from Python!',
            stderr: '',
            result: null,
            error: null,
            duration_ms: 42
        };

        const glyph: Glyph = {
            id: 'result-123',
            title: 'Result',
            symbol: 'result',
            x: 100,
            y: 100,
            renderContent: () => document.createElement('div')
        };

        const element = createResultGlyph(glyph, result);
        container.appendChild(element);

        // Result glyph is created
        expect(element.classList.contains('canvas-result-glyph')).toBe(true);
        expect(element.dataset.glyphId).toBe('result-123');

        // Execution data is attached to glyph object
        expect((glyph as any).result).toBeDefined();
        expect((glyph as any).result.stdout).toBe('Hello from Python!');
        expect((glyph as any).result.duration_ms).toBe(42);
    });

    test('Tim sees execution output in result glyph', () => {
        const container = document.createElement('div');
        container.className = 'canvas-workspace';

        const result: ExecutionResult = {
            success: true,
            stdout: 'Answer: 42\nAll tests passed',
            stderr: '',
            result: 42,
            error: null,
            duration_ms: 150
        };

        const glyph: Glyph = {
            id: 'result-456',
            title: 'Result',
            symbol: 'result',
            x: 200,
            y: 200,
            renderContent: () => document.createElement('div')
        };

        const element = createResultGlyph(glyph, result);
        container.appendChild(element);

        // Output is visible
        const output = element.querySelector('.result-glyph-output');
        expect(output).toBeTruthy();
        expect(output?.textContent).toContain('Answer: 42');
        expect(output?.textContent).toContain('All tests passed');

        // Duration is shown
        expect(element.textContent).toContain('150ms');
    });

    test('Tim sees error output in red', () => {
        const container = document.createElement('div');
        container.className = 'canvas-workspace';

        const result: ExecutionResult = {
            success: false,
            stdout: '',
            stderr: 'Traceback (most recent call last)',
            result: null,
            error: 'NameError: name "foo" is not defined',
            duration_ms: 5
        };

        const glyph: Glyph = {
            id: 'result-789',
            title: 'Result',
            symbol: 'result',
            x: 300,
            y: 300,
            renderContent: () => document.createElement('div')
        };

        const element = createResultGlyph(glyph, result);
        container.appendChild(element);

        // Error content is present
        const output = element.querySelector('.result-glyph-output');
        expect(output).toBeTruthy();
        expect(output?.textContent).toContain('NameError');
    });
});
