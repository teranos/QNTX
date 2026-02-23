/**
 * Tests for TS glyph — local-only orange tint + basic DOM structure
 *
 * Personas:
 * - Tim: Happy path user, normal workflows
 */

import { describe, test, expect, beforeEach, mock } from 'bun:test';
import type { Glyph } from './glyph';

// Mock ResizeObserver
globalThis.ResizeObserver = class ResizeObserver {
    observe() {}
    unobserve() {}
    disconnect() {}
} as any;

// Mock connectivity (ts-glyph subscribes but doesn't use it for color)
mock.module('../../connectivity', () => ({
    connectivityManager: {
        get state() { return 'offline' as const; },
        subscribe(cb: (s: 'online' | 'degraded' | 'offline') => void) {
            cb('offline');
            return () => {};
        },
    },
}));

mock.module('../../state/sync-state', () => ({
    syncStateManager: {
        subscribe() { return () => {}; },
        setState() {},
        clearState() {},
    },
}));

// Mock qntx-wasm (not needed for DOM structure tests)
mock.module('../../qntx-wasm', () => ({
    putAttestation: async () => {},
    queryAttestations: async () => [],
    parseQuery: () => ({ ok: true, query: {} }),
    rebuildFuzzyIndex: async () => ({ subjects: 0, predicates: 0, contexts: 0, actors: 0, hash: '' }),
    getCompletions: () => ({ slot: 'subjects', prefix: '', items: [] }),
    richSearch: async () => ({ query: '', matches: [], total: 0 }),
}));

// Mock CodeMirror (heavy dependency, not needed for DOM structure tests)
mock.module('@codemirror/view', () => {
    const mockView = {
        state: { doc: { toString: () => '' } },
        destroy: () => {},
    };
    return {
        EditorView: class {
            static lineWrapping = [];
            static updateListener = { of: () => [] };
            constructor() { return mockView; }
        },
        keymap: { of: () => [] },
    };
});

mock.module('@codemirror/state', () => ({
    EditorState: {
        create: () => ({ doc: { toString: () => '' } }),
    },
}));

mock.module('@codemirror/commands', () => ({
    defaultKeymap: [],
}));

mock.module('@codemirror/theme-one-dark', () => ({
    oneDark: [],
}));

mock.module('@codemirror/lang-javascript', () => ({
    javascript: () => [],
}));

const { createTsGlyph } = await import('./ts-glyph');

function makeGlyph(id: string, extras: Partial<Glyph> = {}): Glyph {
    return {
        id,
        title: 'TypeScript',
        symbol: 'ts',
        x: 100,
        y: 200,
        renderContent: () => document.createElement('div') as any,
        ...extras,
    };
}

describe('TS Glyph - Tim (Happy Path)', () => {
    beforeEach(() => {
        document.body.innerHTML = '';
        localStorage.clear();
    });

    test('Tim creates TS glyph with correct DOM structure', async () => {
        const element = await createTsGlyph(makeGlyph('ts-tim-1'));

        expect(element.dataset.glyphId).toBe('ts-tim-1');
        expect(element.dataset.glyphSymbol).toBe('ts');
        expect(element.classList.contains('canvas-ts-glyph')).toBe(true);
        expect(element.classList.contains('canvas-glyph')).toBe(true);
    });

    test('Tim sees title bar with label and run button', async () => {
        const element = await createTsGlyph(makeGlyph('ts-tim-2'));

        const titleBar = element.querySelector('.canvas-glyph-title-bar') as HTMLElement;
        expect(titleBar).toBeTruthy();
        expect(titleBar.querySelector('span')?.textContent).toBe('ts');
        expect(element.querySelector('.glyph-play-btn')).toBeTruthy();
        expect(element.querySelector('.glyph-play-btn')?.textContent).toBe('\u25B6');
    });

    test('Tim sees TS glyph is always orange (local-only, browser-native)', async () => {
        const element = await createTsGlyph(makeGlyph('ts-tim-3'));

        // TS glyph is local-active — always orange, regardless of connectivity
        expect(element.dataset.localActive).toBe('true');
        expect(element.style.backgroundColor).toBe('rgba(61, 45, 20, 0.92)');

        const titleBar = element.querySelector('.canvas-glyph-title-bar') as HTMLElement;
        expect(titleBar.style.backgroundColor).toMatch(/#5c3d1a|rgb\(92, 61, 26\)/);
    });

    test('Tim sees title bar label styled warm to match orange tint', async () => {
        const element = await createTsGlyph(makeGlyph('ts-tim-4'));

        const titleBar = element.querySelector('.canvas-glyph-title-bar') as HTMLElement;
        const label = titleBar.querySelector('span:first-child') as HTMLElement;
        expect(label.style.color).toMatch(/#f0c878|rgb\(240, 200, 120\)/);
        expect(label.style.fontWeight).toBe('bold');
    });

    test('Tim sees editor container appended to glyph', async () => {
        const element = await createTsGlyph(makeGlyph('ts-tim-5'));

        const editor = element.querySelector('.ts-glyph-editor');
        expect(editor).toBeTruthy();
    });
});
