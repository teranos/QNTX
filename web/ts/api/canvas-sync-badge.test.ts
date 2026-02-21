/**
 * Tests for canvas sync queue badge (pending count indicator)
 *
 * Personas:
 * - Tim: Badge shows correct count, hides when empty
 * - Spike: Edge cases — rapid add/flush, badge updates on onChange
 */

import { describe, test, expect, beforeEach, mock } from 'bun:test';

let mockConnectivity: 'online' | 'degraded' | 'offline' = 'offline';

mock.module('../connectivity', () => ({
    connectivityManager: {
        get state() { return mockConnectivity; },
        subscribe(cb: (s: 'online' | 'degraded' | 'offline') => void) {
            cb(mockConnectivity);
            return () => {};
        },
    },
}));

let mockApiFetch: (path: string, init?: RequestInit) => Promise<Response>;

mock.module('../api', () => ({
    apiFetch: (path: string, init?: RequestInit) => mockApiFetch(path, init),
}));

let mockGlyphs: Array<{ id: string; symbol: string; x: number; y: number }> = [];

mock.module('../state/ui', () => ({
    uiState: {
        getCanvasGlyphs: () => mockGlyphs,
        setCanvasGlyphs: (g: any[]) => { mockGlyphs.length = 0; mockGlyphs.push(...g); },
        addCanvasGlyph: (g: any) => { const i = mockGlyphs.findIndex((x: any) => x.id === g.id); if (i >= 0) mockGlyphs[i] = g; else mockGlyphs.push(g); },
        upsertCanvasGlyph: (g: any) => { const i = mockGlyphs.findIndex((x: any) => x.id === g.id); if (i >= 0) mockGlyphs[i] = g; else mockGlyphs.push(g); },
        removeCanvasGlyph: (id: string) => { const i = mockGlyphs.findIndex((g: any) => g.id === id); if (i >= 0) mockGlyphs.splice(i, 1); },
        clearCanvasGlyphs: () => { mockGlyphs.length = 0; },
        getCanvasCompositions: () => [],
        setCanvasCompositions: () => {},
        clearCanvasCompositions: () => {},
        getCanvasPan: () => null,
        setCanvasPan: () => {},
        loadPersistedState: () => {},
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

const { canvasSyncQueue } = await import('./canvas-sync');

describe('Canvas Sync Queue - size and onChange', () => {
    beforeEach(() => {
        localStorage.clear();
        mockConnectivity = 'offline';
        mockApiFetch = async () => new Response(null, { status: 200 });
        mockGlyphs = [
            { id: 'g-1', symbol: 'ax', x: 100, y: 200 },
            { id: 'g-2', symbol: 'py', x: 300, y: 400 },
        ];
    });

    test('Tim: size returns 0 for empty queue', () => {
        expect(canvasSyncQueue.size).toBe(0);
    });

    test('Tim: size reflects queued items', () => {
        canvasSyncQueue.add({ id: 'g-1', op: 'glyph_upsert' });
        expect(canvasSyncQueue.size).toBe(1);

        canvasSyncQueue.add({ id: 'g-2', op: 'glyph_upsert' });
        expect(canvasSyncQueue.size).toBe(2);
    });

    test('Tim: size decreases after flush', async () => {
        canvasSyncQueue.add({ id: 'g-1', op: 'glyph_upsert' });
        canvasSyncQueue.add({ id: 'g-2', op: 'glyph_upsert' });
        expect(canvasSyncQueue.size).toBe(2);

        await canvasSyncQueue.flush();
        expect(canvasSyncQueue.size).toBe(0);
    });

    test('Tim: duplicate add does not increase size', () => {
        canvasSyncQueue.add({ id: 'g-1', op: 'glyph_upsert' });
        canvasSyncQueue.add({ id: 'g-1', op: 'glyph_upsert' });
        expect(canvasSyncQueue.size).toBe(1);
    });

    test('Tim: onChange fires on add', () => {
        let callCount = 0;
        canvasSyncQueue.onChange(() => { callCount++; });

        canvasSyncQueue.add({ id: 'g-1', op: 'glyph_upsert' });
        expect(callCount).toBe(1);
    });

    test('Tim: onChange fires on flush', async () => {
        canvasSyncQueue.add({ id: 'g-1', op: 'glyph_upsert' });

        let callCount = 0;
        canvasSyncQueue.onChange(() => { callCount++; });

        await canvasSyncQueue.flush();
        expect(callCount).toBeGreaterThanOrEqual(1);
    });

    test('Spike: onChange unsubscribe stops notifications', () => {
        let callCount = 0;
        const unsub = canvasSyncQueue.onChange(() => { callCount++; });

        canvasSyncQueue.add({ id: 'g-1', op: 'glyph_upsert' });
        expect(callCount).toBe(1);

        unsub();

        canvasSyncQueue.add({ id: 'g-2', op: 'glyph_upsert' });
        expect(callCount).toBe(1); // No additional call
    });
});
