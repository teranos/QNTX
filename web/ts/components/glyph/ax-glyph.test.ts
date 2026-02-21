/**
 * Tests for AX glyph — canvasPlaced refactor + color state tracking
 *
 * Personas:
 * - Tim: Happy path user, normal workflows
 * - Spike: Tries to break things, edge cases
 * - Jenny: Power user, complex scenarios
 */

import { describe, test, expect, beforeEach, mock } from 'bun:test';
import type { Glyph } from './glyph';
import { AX } from '@generated/sym.js';

// Mock ResizeObserver
globalThis.ResizeObserver = class ResizeObserver {
    observe() {}
    unobserve() {}
    disconnect() {}
} as any;

// Connectivity mock — tests need to control online/offline transitions
let mockState: 'online' | 'degraded' | 'offline' = 'offline';
const subscribers = new Set<(s: 'online' | 'degraded' | 'offline') => void>();

mock.module('../../connectivity', () => ({
    connectivityManager: {
        get state() { return mockState; },
        subscribe(cb: (s: 'online' | 'degraded' | 'offline') => void) {
            subscribers.add(cb);
            cb(mockState);
            return () => { subscribers.delete(cb); };
        },
    },
}));

// Mock uiState — mock.module is process-global so every mock must be superset-complete
const mockCanvasGlyphs: any[] = [];
const mockCanvasCompositions: any[] = [];
const mockCanvasPan: Record<string, any> = {};
mock.module('../../state/ui', () => ({
    uiState: {
        getCanvasGlyphs: () => mockCanvasGlyphs,
        setCanvasGlyphs: (glyphs: any[]) => { mockCanvasGlyphs.length = 0; mockCanvasGlyphs.push(...glyphs); },
        upsertCanvasGlyph: (glyph: any) => {
            const index = mockCanvasGlyphs.findIndex(g => g.id === glyph.id);
            if (index >= 0) { mockCanvasGlyphs[index] = glyph; } else { mockCanvasGlyphs.push(glyph); }
        },
        addCanvasGlyph: (glyph: any) => {
            const index = mockCanvasGlyphs.findIndex(g => g.id === glyph.id);
            if (index >= 0) { mockCanvasGlyphs[index] = glyph; } else { mockCanvasGlyphs.push(glyph); }
        },
        removeCanvasGlyph: (id: string) => {
            const index = mockCanvasGlyphs.findIndex(g => g.id === id);
            if (index >= 0) mockCanvasGlyphs.splice(index, 1);
        },
        getCanvasCompositions: () => mockCanvasCompositions,
        setCanvasCompositions: (comps: any[]) => { mockCanvasCompositions.length = 0; mockCanvasCompositions.push(...comps); },
        clearCanvasGlyphs: () => mockCanvasGlyphs.length = 0,
        clearCanvasCompositions: () => mockCanvasCompositions.length = 0,
        loadPersistedState: () => {},
        // Superset-complete stubs (mock.module is process-global, leaks into other test files)
        getCanvasPan: (id: string) => mockCanvasPan[id] ?? null,
        setCanvasPan: (id: string, pan: any) => { mockCanvasPan[id] = pan; },
        getMinimizedWindows: () => [],
        addMinimizedWindow: () => {},
        removeMinimizedWindow: () => {},
        setMinimizedWindows: () => {},
        isWindowMinimized: () => false,
        clearMinimizedWindows: () => {},
        isPanelVisible: () => false,
        setPanelVisible: () => {},
        togglePanel: () => false,
        closeAllPanels: () => {},
        getActiveModality: () => 'ax',
        setActiveModality: () => {},
        getBudgetWarnings: () => ({ daily: false, weekly: false, monthly: false }),
        setBudgetWarning: () => {},
        resetBudgetWarnings: () => {},
        getUsageView: () => 'week',
        setUsageView: () => {},
        getGraphSession: () => ({}),
        setGraphSession: () => {},
        setGraphQuery: () => {},
        setGraphVerbosity: () => {},
        clearGraphSession: () => {},
        subscribe: () => () => {},
        subscribeAll: () => () => {},
        getState: () => ({}),
        get: () => undefined,
        clearStorage: () => {},
        reset: () => {},
    },
}));

mock.module('../../state/sync-state', () => ({
    syncStateManager: {
        subscribe() { return () => {}; },
        setState() {},
        clearState() {},
    },
}));

// Mock qntx-wasm so parseQuery resolves synchronously (no real WASM in test).
// Must include ALL exports any consumer needs — mock.module is process-global,
// so this mock may be used by ts-glyph.ts (which imports putAttestation) too.
mock.module('../../qntx-wasm', () => ({
    putAttestation: async (a: unknown) => a,
    queryAttestations: () => [],
    parseQuery: () => ({ ok: false, error: 'no wasm in test' }),
    rebuildFuzzyIndex: async () => ({ subjects: 0, predicates: 0, contexts: 0, actors: 0, hash: '' }),
    getCompletions: () => ({ slot: 'subjects', prefix: '', items: [] }),
}));

const { createAxGlyph, updateAxGlyphError } = await import('./ax-glyph');

function makeGlyph(id: string, extras: Partial<Glyph> = {}): Glyph {
    return {
        id,
        title: 'AX Query',
        symbol: AX,
        x: 100,
        y: 200,
        renderContent: () => document.createElement('div') as any,
        ...extras,
    };
}

function setConnectivity(state: 'online' | 'degraded' | 'offline') {
    mockState = state;
    for (const cb of subscribers) cb(state);
}

describe('AX Glyph - Tim (Happy Path)', () => {
    beforeEach(() => {
        document.body.innerHTML = '';
        mockCanvasGlyphs.length = 0;
        mockState = 'offline';
        subscribers.clear();
    });

    test('Tim creates AX glyph with correct DOM structure', () => {
        const element = createAxGlyph(makeGlyph('ax-tim-1'));

        expect(element.dataset.glyphId).toBe('ax-tim-1');
        expect(element.dataset.glyphSymbol).toBe(AX);
        expect(element.classList.contains('canvas-ax-glyph')).toBe(true);
        expect(element.classList.contains('canvas-glyph')).toBe(true);
        expect(element.querySelector('.ax-query-input')).toBeTruthy();
        expect(element.querySelector('.ax-glyph-results')).toBeTruthy();
    });

    test('Tim sees title bar with shared canvas-glyph-title-bar class', () => {
        const element = createAxGlyph(makeGlyph('ax-tim-2'));

        const titleBar = element.querySelector('.canvas-glyph-title-bar') as HTMLElement;
        expect(titleBar).toBeTruthy();
        expect(titleBar.style.padding).toBe('4px 4px 4px 8px');
        expect(titleBar.querySelector('span')?.textContent).toBe(AX);
        expect(titleBar.querySelector('.ax-query-input')).toBeTruthy();
    });

    test('Tim creates fresh glyph, starts in idle color state', () => {
        const element = createAxGlyph(makeGlyph('ax-tim-3'));
        const titleBar = element.querySelector('.canvas-glyph-title-bar') as HTMLElement;

        expect(element.style.backgroundColor).toBe('rgba(30, 30, 35, 0.92)');
        expect(titleBar.style.backgroundColor).toBe('var(--bg-tertiary)');
    });

    test('Tim creates glyph with persisted query from uiState', () => {
        mockCanvasGlyphs.push({ id: 'ax-tim-4', symbol: AX, x: 0, y: 0, content: 'is git' });

        const element = createAxGlyph(makeGlyph('ax-tim-4'));

        const input = element.querySelector('.ax-query-input') as HTMLInputElement;
        expect(input.value).toBe('is git');
    });

    test('Tim goes online, container and title bar turn teal together', () => {
        mockCanvasGlyphs.push({ id: 'ax-tim-5', symbol: AX, x: 0, y: 0, content: 'TEST5' });

        const element = createAxGlyph(makeGlyph('ax-tim-5'));
        document.body.appendChild(element);
        const titleBar = element.querySelector('.canvas-glyph-title-bar') as HTMLElement;

        // Offline → orange
        expect(element.style.backgroundColor).toBe('rgba(61, 45, 20, 0.92)');
        expect(titleBar.style.backgroundColor).toMatch(/#5c3d1a|rgb\(92, 61, 26\)/);

        // Online → teal
        setConnectivity('online');
        expect(element.style.backgroundColor).toBe('rgba(31, 61, 61, 0.92)');
        expect(titleBar.style.backgroundColor).toMatch(/#1f3d3d|rgb\(31, 61, 61\)/);
    });

    test('Tim title bar background always matches container state', () => {
        mockCanvasGlyphs.push({ id: 'ax-tim-6', symbol: AX, x: 0, y: 0, content: 'ALICE' });

        const element = createAxGlyph(makeGlyph('ax-tim-6'));
        document.body.appendChild(element);
        const titleBar = element.querySelector('.canvas-glyph-title-bar') as HTMLElement;

        // Offline → orange pair
        expect(element.style.backgroundColor).toBe('rgba(61, 45, 20, 0.92)');
        expect(titleBar.style.backgroundColor).toMatch(/#5c3d1a|rgb\(92, 61, 26\)/);

        // Online → teal pair
        setConnectivity('online');
        expect(element.style.backgroundColor).toBe('rgba(31, 61, 61, 0.92)');
        expect(titleBar.style.backgroundColor).toMatch(/#1f3d3d|rgb\(31, 61, 61\)/);

        // Offline again → orange pair
        setConnectivity('offline');
        expect(element.style.backgroundColor).toBe('rgba(61, 45, 20, 0.92)');
        expect(titleBar.style.backgroundColor).toMatch(/#5c3d1a|rgb\(92, 61, 26\)/);
    });
});

describe('AX Glyph - Spike (Edge Cases)', () => {
    beforeEach(() => {
        document.body.innerHTML = '';
        mockCanvasGlyphs.length = 0;
        mockState = 'offline';
        subscribers.clear();
    });

    test('Spike triggers error — container and title bar both turn red', () => {
        const element = createAxGlyph(makeGlyph('ax-spike-1'));
        document.body.appendChild(element);

        updateAxGlyphError('ax-spike-1', 'bad query', 'error');

        const titleBar = element.querySelector('.canvas-glyph-title-bar') as HTMLElement;
        expect(element.style.backgroundColor).toContain('rgba(61, 31, 31');
        expect(titleBar.style.backgroundColor).toMatch(/#3d1f1f|rgb\(61, 31, 31\)/);
    });
});

describe('AX Glyph - Jenny (Power User)', () => {
    beforeEach(() => {
        document.body.innerHTML = '';
        mockCanvasGlyphs.length = 0;
        mockState = 'online';
        subscribers.clear();
    });

    test('Jenny goes offline, AX re-fires local query and turns orange', () => {
        mockCanvasGlyphs.push({ id: 'ax-jenny-1', symbol: AX, x: 0, y: 0, content: 'of qntx' });

        const element = createAxGlyph(makeGlyph('ax-jenny-1'));
        document.body.appendChild(element);
        const titleBar = element.querySelector('.canvas-glyph-title-bar') as HTMLElement;

        // Online → teal
        expect(element.style.backgroundColor).toBe('rgba(31, 61, 61, 0.92)');
        expect(titleBar.style.backgroundColor).toMatch(/#1f3d3d|rgb\(31, 61, 61\)/);

        // Offline → orange + data attributes updated
        setConnectivity('offline');
        expect(element.style.backgroundColor).toBe('rgba(61, 45, 20, 0.92)');
        expect(titleBar.style.backgroundColor).toMatch(/#5c3d1a|rgb\(92, 61, 26\)/);
        expect(element.dataset.localActive).toBe('true');
        expect(element.dataset.connectivityMode).toBe('offline');
    });
});
