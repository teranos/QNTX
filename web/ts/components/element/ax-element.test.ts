/**
 * Tests for AX element — canvasPlaced refactor + color state tracking
 *
 * Personas:
 * - Tim: Happy path user, normal workflows
 * - Spike: Tries to break things, edge cases
 * - Jenny: Power user, complex scenarios
 */

import { describe, test, expect, beforeEach, mock } from 'bun:test';
import type { Element } from '@teranos/elements';
import { AX } from '../../sym';

// Mock ResizeObserver
globalThis.ResizeObserver = class ResizeObserver {
    observe() {}
    unobserve() {}
    disconnect() {}
} as any;

// Connectivity mock — tests need to control online/offline transitions
let mockState: 'online' | 'degraded' | 'offline' = 'offline';
const subscribers = new Set<(s: 'online' | 'degraded' | 'offline') => void>();

mock.module('../../client', () => ({
    connectivity: {
        get state() { return mockState; },
        subscribe(cb: (s: 'online' | 'degraded' | 'offline') => void) {
            subscribers.add(cb);
            cb(mockState);
            return () => { subscribers.delete(cb); };
        },
        subscribeAuth: () => () => {},
    },
    sendMessage: () => false,
    apiFetch: () => Promise.resolve(new Response()),
}));

// Mock uiState — process-global, must be superset-complete (see test/mock-ui-state.ts)
import { createMockUiState } from '../../test/mock-ui-state';
const { uiState, elements: mockCanvasElements } = createMockUiState();
mock.module('../../state/ui', () => ({ uiState }));

mock.module('../../state/sync-state', () => ({
    syncStateManager: {
        subscribe() { return () => {}; },
        setState() {},
        clearState() {},
    },
}));

// Mock ats-wasm so parseQuery resolves synchronously (no real WASM in test).
// Must include ALL exports any consumer needs — mock.module is process-global,
// so this mock may be used by ts-element.ts (which imports putAttestation) too.
mock.module('../../ats-wasm', () => ({
    putAttestation: async (a: unknown) => a,
    queryAttestations: () => [],
    parseQuery: () => ({ ok: false, error: 'no wasm in test' }),
    generateASUID: () => ({ full: 'AS-TEST-MOCK-QNTX-XXXXXXXX', short: 'AS-TEST-MOCK-QNTX-XXXX' }),
}));

const { createAxElement, updateAxElementError } = await import('./ax-element');

function makeElement(id: string, extras: Partial<Element> = {}): Element {
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

describe('AX Element - Tim (Happy Path)', () => {
    beforeEach(() => {
        document.body.innerHTML = '';
        mockCanvasElements.length = 0;
        mockState = 'offline';
        subscribers.clear();
    });

    test('Tim creates AX element with correct DOM structure', () => {
        const element = createAxElement(makeElement('ax-tim-1'));

        expect(element.dataset.elementId).toBe('ax-tim-1');
        expect(element.dataset.symbol).toBe(AX);
        expect(element.classList.contains('canvas-ax-element')).toBe(true);
        expect(element.classList.contains('canvas-element')).toBe(true);
        expect(element.querySelector('.ax-query-input')).toBeTruthy();
        expect(element.querySelector('.ax-element-results')).toBeTruthy();
    });

    test('Tim sees title bar with shared title-bar class', () => {
        const element = createAxElement(makeElement('ax-tim-2'));

        const titleBar = element.querySelector('.title-bar') as HTMLElement;
        expect(titleBar).toBeTruthy();
        expect(titleBar.style.padding).toBe('4px 4px 4px 8px');
        expect(titleBar.querySelector('span')?.textContent).toBe(AX);
        expect(titleBar.querySelector('.ax-query-input')).toBeTruthy();
    });

    test('Tim creates fresh element, starts in idle color state', () => {
        const element = createAxElement(makeElement('ax-tim-3'));
        const titleBar = element.querySelector('.title-bar') as HTMLElement;

        expect(element.style.backgroundColor).toBe('var(--element-status-idle-bg)');
        expect(titleBar.style.backgroundColor).toBe('var(--element-status-idle-section-bg)');
    });

    test('Tim creates element with persisted query from uiState', () => {
        mockCanvasElements.push({ id: 'ax-tim-4', symbol: AX, x: 0, y: 0, content: 'is git' });

        const element = createAxElement(makeElement('ax-tim-4'));

        const input = element.querySelector('.ax-query-input') as HTMLInputElement;
        expect(input.value).toBe('is git');
    });

    test('Tim goes online, container and title bar turn teal together', () => {
        mockCanvasElements.push({ id: 'ax-tim-5', symbol: AX, x: 0, y: 0, content: 'TEST5' });

        const element = createAxElement(makeElement('ax-tim-5'));
        document.body.appendChild(element);
        const titleBar = element.querySelector('.title-bar') as HTMLElement;

        // Offline → orange
        expect(element.style.backgroundColor).toBe('rgba(61, 45, 20, 0.92)');
        expect(titleBar.style.backgroundColor).toMatch(/#5c3d1a|rgb\(92, 61, 26\)/);

        // Online → teal
        setConnectivity('online');
        expect(element.style.backgroundColor).toBe('rgba(31, 61, 61, 0.92)');
        expect(titleBar.style.backgroundColor).toMatch(/#1f3d3d|rgb\(31, 61, 61\)/);
    });

    test('Tim title bar background always matches container state', () => {
        mockCanvasElements.push({ id: 'ax-tim-6', symbol: AX, x: 0, y: 0, content: 'ALICE' });

        const element = createAxElement(makeElement('ax-tim-6'));
        document.body.appendChild(element);
        const titleBar = element.querySelector('.title-bar') as HTMLElement;

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

describe('AX Element - Spike (Edge Cases)', () => {
    beforeEach(() => {
        document.body.innerHTML = '';
        mockCanvasElements.length = 0;
        mockState = 'offline';
        subscribers.clear();
    });

    test('Spike triggers error — container and title bar both turn red', () => {
        const element = createAxElement(makeElement('ax-spike-1'));
        document.body.appendChild(element);

        updateAxElementError('ax-spike-1', 'bad query', 'error');

        const titleBar = element.querySelector('.title-bar') as HTMLElement;
        expect(element.dataset.responseState).toBe('error');
        expect(element.style.backgroundColor).toBe('var(--element-status-error-bg)');
        expect(titleBar.style.backgroundColor).toBe('var(--element-status-error-section-bg)');
    });
});

describe('AX Element - Jenny (Power User)', () => {
    beforeEach(() => {
        document.body.innerHTML = '';
        mockCanvasElements.length = 0;
        mockState = 'online';
        subscribers.clear();
    });

    test('Jenny goes offline, AX re-fires local query and turns orange', () => {
        mockCanvasElements.push({ id: 'ax-jenny-1', symbol: AX, x: 0, y: 0, content: 'of qntx' });

        const element = createAxElement(makeElement('ax-jenny-1'));
        document.body.appendChild(element);
        const titleBar = element.querySelector('.title-bar') as HTMLElement;

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
