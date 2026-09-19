/**
 * Tests for ElementUI SDK — spawnResult event dispatch, titleBar options
 *
 * Personas:
 * - Tim: Happy path plugin author using the SDK
 * - Spike: Edge cases — calling methods before element() is called
 */

import { describe, test, expect, mock } from 'bun:test';
import type { Element } from '@teranos/elements';
import type { SpawnResultDetail } from './element-ui';

// Only run under JSDOM (CI) — CustomEvent dispatch requires consistent DOM
const USE_JSDOM = process.env.USE_JSDOM === '1';

// Mock ResizeObserver
globalThis.ResizeObserver = class ResizeObserver {
    observe() {}
    unobserve() {}
    disconnect() {}
} as any;

// Use superset-complete mock for uiState (avoids leaking a minimal mock)
import { createMockUiState } from '../../test/mock-ui-state';
const { uiState } = createMockUiState();
mock.module('../../state/ui', () => ({ uiState }));

mock.module('../../connectivity', () => ({
    connectivityManager: {
        get state() { return 'online' as const; },
        subscribe: () => () => {},
        subscribeAuth: () => () => {},
    },
}));

mock.module('../../state/sync-state', () => ({
    syncStateManager: {
        subscribe() { return () => {}; },
        setState() {},
        clearState() {},
    },
}));

const { createElementUI } = await import('./element-ui');

function makeElement(id: string): Element {
    return {
        id,
        title: 'Test',
        symbol: 'test',
        x: 100, y: 100,
        renderContent: () => document.createElement('div'),
    };
}

describe('ElementUI SDK - Tim (Happy Path)', () => {
    if (!USE_JSDOM) {
        test.skip('Skipped locally (run with USE_JSDOM=1 to enable)', () => {});
        return;
    }

    test('Tim calls spawnResult and the correct DOM event fires', () => {
        const item = makeElement('test-spawn-1');
        const ui = createElementUI(item, 'myPlugin');
        const { element } = ui.element({
            defaults: { x: 0, y: 0, width: 300, height: 200 },
            titleBar: { label: 'test' },
        });

        let received: SpawnResultDetail | null = null;
        element.addEventListener('element:spawn-result', ((e: CustomEvent<SpawnResultDetail>) => {
            received = e.detail;
        }) as EventListener);

        ui.spawnResult({
            success: true,
            stdout: 'hello world',
            stderr: '',
            result: null,
            error: null,
            duration_ms: 42,
        });

        expect(received).not.toBeNull();
        expect(received!.elementId).toBe('test-spawn-1');
        expect(received!.name).toBe('myPlugin');
        expect(received!.result.success).toBe(true);
        expect(received!.result.stdout).toBe('hello world');
        expect(received!.result.duration_ms).toBe(42);
    });

    test('Tim calls spawnResult with error result', () => {
        const item = makeElement('test-spawn-2');
        const ui = createElementUI(item, 'failPlugin');
        const { element } = ui.element({
            defaults: { x: 0, y: 0, width: 300, height: 200 },
            titleBar: { label: 'test' },
        });

        let received: SpawnResultDetail | null = null;
        element.addEventListener('element:spawn-result', ((e: CustomEvent<SpawnResultDetail>) => {
            received = e.detail;
        }) as EventListener);

        ui.spawnResult({
            success: false,
            stdout: '',
            stderr: 'traceback here',
            result: null,
            error: 'SyntaxError: invalid',
            duration_ms: 5,
        });

        expect(received!.result.success).toBe(false);
        expect(received!.result.error).toBe('SyntaxError: invalid');
        expect(received!.result.stderr).toBe('traceback here');
    });

    test('Tim sets titleBar color and labelColor via SDK', () => {
        const item = makeElement('test-color-1');
        const ui = createElementUI(item, 'colorPlugin');
        const { element } = ui.element({
            defaults: { x: 0, y: 0, width: 300, height: 200 },
            titleBar: { label: 'colored', color: '#2a5578', labelColor: '#FFD43B' },
        });

        const titleBar = element.querySelector('.title-bar') as HTMLElement;
        expect(titleBar.style.backgroundColor).toMatch(/#2a5578|rgb\(42, 85, 120\)/);

        const label = titleBar.querySelector('span:not(.symbol)') as HTMLElement;
        expect(label.style.color).toMatch(/#FFD43B|rgb\(255, 212, 59\)/);
    });
});

describe('ElementUI SDK - Spike (Edge Cases)', () => {
    if (!USE_JSDOM) {
        test.skip('Skipped locally (run with USE_JSDOM=1 to enable)', () => {});
        return;
    }

    test('Spike calls spawnResult before element() — no crash', () => {
        const item = makeElement('test-early-1');
        const ui = createElementUI(item, 'earlyPlugin');

        expect(() => {
            ui.spawnResult({
                success: true, stdout: '', stderr: '',
                result: null, error: null, duration_ms: 0,
            });
        }).not.toThrow();
    });
});
