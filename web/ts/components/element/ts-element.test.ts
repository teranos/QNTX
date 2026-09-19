/**
 * Tests for TS element — local-only orange tint + basic DOM structure
 *
 * Personas:
 * - Tim: Happy path user, normal workflows
 */

import { describe, test, expect, beforeEach, mock } from 'bun:test';
import type { Element } from '@teranos/elements';

// Mock ResizeObserver
globalThis.ResizeObserver = class ResizeObserver {
    observe() {}
    unobserve() {}
    disconnect() {}
} as any;

// Mock connectivity (ts-element subscribes but doesn't use it for color)
mock.module('../../connectivity', () => ({
    connectivityManager: {
        get state() { return 'offline' as const; },
        subscribe(cb: (s: 'online' | 'degraded' | 'offline') => void) {
            cb('offline');
            return () => {};
        },
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

// Mock ats-wasm (not needed for DOM structure tests)
mock.module('../../ats-wasm', () => ({
    putAttestation: async () => {},
    queryAttestations: async () => [],
    parseQuery: () => ({ ok: true, query: {} }),
    generateASUID: () => ({ full: 'AS-TEST-MOCK-QNTX-XXXXXXXX', short: 'AS-TEST-MOCK-QNTX-XXXX' }),
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

const { createTsElement } = await import('./ts-element');

function makeElement(id: string, extras: Partial<Element> = {}): Element {
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

describe('TS Element - Tim (Happy Path)', () => {
    beforeEach(() => {
        document.body.innerHTML = '';
        localStorage.clear();
    });

    test('Tim creates TS element with correct DOM structure', async () => {
        const element = await createTsElement(makeElement('ts-tim-1'));

        expect(element.dataset.elementId).toBe('ts-tim-1');
        expect(element.dataset.symbol).toBe('ts');
        expect(element.classList.contains('canvas-ts-element')).toBe(true);
        expect(element.classList.contains('canvas-element')).toBe(true);
    });

    test('Tim sees title bar with label and run button', async () => {
        const element = await createTsElement(makeElement('ts-tim-2'));

        const titleBar = element.querySelector('.title-bar') as HTMLElement;
        expect(titleBar).toBeTruthy();
        expect(titleBar.querySelector('span')?.textContent).toBe('ts');
        expect(element.querySelector('.titlebar-btn')).toBeTruthy();
        expect(element.querySelector('.titlebar-btn')?.textContent).toBe('\u25B6');
    });

    test('Tim sees TS element is always orange (local-only, browser-native)', async () => {
        const element = await createTsElement(makeElement('ts-tim-3'));

        // TS element is local-active — always orange, regardless of connectivity
        expect(element.dataset.localActive).toBe('true');
        expect(element.style.backgroundColor).toBe('rgba(61, 45, 20, 0.92)');

        const titleBar = element.querySelector('.title-bar') as HTMLElement;
        expect(titleBar.style.backgroundColor).toMatch(/#5c3d1a|rgb\(92, 61, 26\)/);
    });

    test('Tim sees title bar label styled warm to match orange tint', async () => {
        const element = await createTsElement(makeElement('ts-tim-4'));

        const titleBar = element.querySelector('.title-bar') as HTMLElement;
        const label = titleBar.querySelector('span:not(.symbol)') as HTMLElement;
        expect(label.style.color).toMatch(/#f0c878|rgb\(240, 200, 120\)/);
    });

    test('Tim sees editor container appended to element', async () => {
        const element = await createTsElement(makeElement('ts-tim-5'));

        const editor = element.querySelector('.ts-element-editor');
        expect(editor).toBeTruthy();
    });
});
