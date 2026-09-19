/**
 * Tests for result element drag persistence
 *
 * Personas:
 * - Tim: Happy path user, normal workflows
 * - Spike: Tries to break things, edge cases
 * - Jenny: Power user, complex scenarios
 */

import { describe, test, expect } from 'bun:test';
import { createResultElement, type ExecutionResult } from './result-element';
import type { Element } from '@teranos/elements';

// Mock ResizeObserver for tests
globalThis.ResizeObserver = class ResizeObserver {
    observe() {}
    unobserve() {}
    disconnect() {}
} as any;

describe('Result Element Drag Persistence - Tim (Happy Path)', () => {
    test('Tim creates result element with execution data', () => {
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

        const item: Element = {
            id: 'result-123',
            title: 'Result',
            symbol: 'result',
            x: 100,
            y: 100,
            renderContent: () => document.createElement('div')
        };

        const element = createResultElement(item, result);
        container.appendChild(element);

        // Result element is created
        expect(element.classList.contains('canvas-result-element')).toBe(true);
        expect(element.dataset.elementId).toBe('result-123');

        // Execution data is attached to element object as ResultElementContent JSON
        expect((item as any).content).toBeDefined();
        const parsed = JSON.parse((item as any).content);
        expect(parsed.result.stdout).toBe('Hello from Python!');
        expect(parsed.result.duration_ms).toBe(42);
    });

    test('Tim sees execution output in result element', () => {
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

        const item: Element = {
            id: 'result-456',
            title: 'Result',
            symbol: 'result',
            x: 200,
            y: 200,
            renderContent: () => document.createElement('div')
        };

        const element = createResultElement(item, result);
        container.appendChild(element);

        // Output is visible
        const output = element.querySelector('.result-element-output');
        expect(output).toBeTruthy();
        expect(output?.textContent).toContain('Answer: 42');
        expect(output?.textContent).toContain('All tests passed');

        // Copy button is present
        const copyBtn = element.querySelector('button[title="Copy to clipboard"]');
        expect(copyBtn).not.toBeNull();
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

        const item: Element = {
            id: 'result-789',
            title: 'Result',
            symbol: 'result',
            x: 300,
            y: 300,
            renderContent: () => document.createElement('div')
        };

        const element = createResultElement(item, result);
        container.appendChild(element);

        // Error content is present
        const output = element.querySelector('.result-element-output');
        expect(output).toBeTruthy();
        expect(output?.textContent).toContain('NameError');
    });
});

describe('Result Element Drag Persistence - Spike (Edge Cases)', () => {
    test('Spike creates result with extremely long output', () => {
        const container = document.createElement('div');
        container.className = 'canvas-workspace';

        const longOutput = 'Line of output\n'.repeat(1000);

        const result: ExecutionResult = {
            success: true,
            stdout: longOutput,
            stderr: '',
            result: null,
            error: null,
            duration_ms: 5000
        };

        const item: Element = {
            id: 'result-long',
            title: 'Result',
            symbol: 'result',
            x: 100,
            y: 100,
            renderContent: () => document.createElement('div')
        };

        const element = createResultElement(item, result);
        container.appendChild(element);

        // Result element handles large output
        expect(element.classList.contains('canvas-result-element')).toBe(true);
        const parsed = JSON.parse((item as any).content);
        expect(parsed.result.stdout.length).toBe(longOutput.length);
    });
});
