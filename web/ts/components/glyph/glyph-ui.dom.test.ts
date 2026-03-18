/**
 * Tests for GlyphUI SDK — spawnResult event dispatch, titleBar options
 *
 * Personas:
 * - Tim: Happy path plugin author using the SDK
 * - Spike: Edge cases — calling methods before glyph() is called
 */

import { describe, test, expect, mock } from 'bun:test';
import type { Glyph } from './glyph';
import type { SpawnResultDetail } from './glyph-ui';

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

const { createGlyphUI } = await import('./glyph-ui');

function makeGlyph(id: string): Glyph {
    return {
        id,
        title: 'Test',
        symbol: 'test',
        x: 100, y: 100,
        renderContent: () => document.createElement('div'),
    };
}

describe('GlyphUI SDK - Tim (Happy Path)', () => {
    if (!USE_JSDOM) {
        test.skip('Skipped locally (run with USE_JSDOM=1 to enable)', () => {});
        return;
    }

    test('Tim calls spawnResult and the correct DOM event fires', () => {
        const glyph = makeGlyph('test-spawn-1');
        const ui = createGlyphUI(glyph, 'myPlugin');
        const { element } = ui.glyph({
            defaults: { x: 0, y: 0, width: 300, height: 200 },
            titleBar: { label: 'test' },
        });

        let received: SpawnResultDetail | null = null;
        element.addEventListener('glyph:spawn-result', ((e: CustomEvent<SpawnResultDetail>) => {
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
        expect(received!.glyphId).toBe('test-spawn-1');
        expect(received!.name).toBe('myPlugin');
        expect(received!.result.success).toBe(true);
        expect(received!.result.stdout).toBe('hello world');
        expect(received!.result.duration_ms).toBe(42);
    });

    test('Tim calls spawnResult with error result', () => {
        const glyph = makeGlyph('test-spawn-2');
        const ui = createGlyphUI(glyph, 'failPlugin');
        const { element } = ui.glyph({
            defaults: { x: 0, y: 0, width: 300, height: 200 },
            titleBar: { label: 'test' },
        });

        let received: SpawnResultDetail | null = null;
        element.addEventListener('glyph:spawn-result', ((e: CustomEvent<SpawnResultDetail>) => {
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
        const glyph = makeGlyph('test-color-1');
        const ui = createGlyphUI(glyph, 'colorPlugin');
        const { element } = ui.glyph({
            defaults: { x: 0, y: 0, width: 300, height: 200 },
            titleBar: { label: 'colored', color: '#2a5578', labelColor: '#FFD43B' },
        });

        const titleBar = element.querySelector('.glyph-title-bar') as HTMLElement;
        expect(titleBar.style.backgroundColor).toMatch(/#2a5578|rgb\(42, 85, 120\)/);

        const label = titleBar.querySelector('span:first-child') as HTMLElement;
        expect(label.style.color).toMatch(/#FFD43B|rgb\(255, 212, 59\)/);
    });
});

describe('GlyphUI SDK - Spike (Edge Cases)', () => {
    if (!USE_JSDOM) {
        test.skip('Skipped locally (run with USE_JSDOM=1 to enable)', () => {});
        return;
    }

    test('Spike calls spawnResult before glyph() — no crash', () => {
        const glyph = makeGlyph('test-early-1');
        const ui = createGlyphUI(glyph, 'earlyPlugin');

        expect(() => {
            ui.spawnResult({
                success: true, stdout: '', stderr: '',
                result: null, error: null, duration_ms: 0,
            });
        }).not.toThrow();
    });
});
