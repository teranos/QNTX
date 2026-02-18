/**
 * Tests for canvas sync queue exponential backoff
 *
 * Personas:
 * - Spike: Retry behavior, backoff timing, permanent failure after max retries
 */

import { describe, test, expect, beforeEach, mock } from 'bun:test';
import { syncStateManager } from '../state/sync-state';

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
        getCanvasCompositions: () => [],
    },
}));

const { canvasSyncQueue } = await import('./canvas-sync');

const STORAGE_KEY = 'qntx-canvas-sync-queue';

describe('Canvas Sync - Spike (Backoff)', () => {
    beforeEach(() => {
        localStorage.clear();
        mockConnectivity = 'offline';
        mockApiFetch = async () => new Response(null, { status: 200 });
        mockGlyphs = [{ id: 'g-1', symbol: 'ax', x: 100, y: 200 }];
        syncStateManager.clearState('g-1');
    });

    test('failed item gets retryCount incremented', async () => {
        canvasSyncQueue.add({ id: 'g-1', op: 'glyph_upsert' });
        mockApiFetch = async () => new Response(null, { status: 500 });

        await canvasSyncQueue.flush();

        const stored = JSON.parse(localStorage.getItem(STORAGE_KEY) || '[]');
        expect(stored).toHaveLength(1);
        expect(stored[0].retryCount).toBe(1);
    });

    test('backoff delay set after failure (1s for first retry)', async () => {
        canvasSyncQueue.add({ id: 'g-1', op: 'glyph_upsert' });
        mockApiFetch = async () => new Response(null, { status: 500 });

        const before = Date.now();
        await canvasSyncQueue.flush();

        const stored = JSON.parse(localStorage.getItem(STORAGE_KEY) || '[]');
        expect(stored[0].nextRetryAt).toBeGreaterThanOrEqual(before + 900);
        expect(stored[0].nextRetryAt).toBeLessThan(before + 2000);
    });

    test('item skipped during flush if backoff not expired', async () => {
        localStorage.setItem(STORAGE_KEY, JSON.stringify([
            { id: 'g-1', op: 'glyph_upsert', retryCount: 1, nextRetryAt: Date.now() + 60000 }
        ]));

        let fetchCalled = false;
        mockApiFetch = async () => {
            fetchCalled = true;
            return new Response(null, { status: 200 });
        };

        await canvasSyncQueue.flush();

        expect(fetchCalled).toBe(false);
        const stored = JSON.parse(localStorage.getItem(STORAGE_KEY) || '[]');
        expect(stored).toHaveLength(1);
    });

    test('item retried after backoff expired', async () => {
        localStorage.setItem(STORAGE_KEY, JSON.stringify([
            { id: 'g-1', op: 'glyph_upsert', retryCount: 1, nextRetryAt: Date.now() - 1000 }
        ]));

        await canvasSyncQueue.flush();

        expect(syncStateManager.getState('g-1')).toBe('synced');
        const stored = JSON.parse(localStorage.getItem(STORAGE_KEY) || '[]');
        expect(stored).toHaveLength(0);
    });

    test('permanently failed after 3 attempts, removed from queue', async () => {
        canvasSyncQueue.add({ id: 'g-1', op: 'glyph_upsert' });
        mockApiFetch = async () => new Response(null, { status: 500 });

        // Attempt 1
        await canvasSyncQueue.flush();
        let stored = JSON.parse(localStorage.getItem(STORAGE_KEY) || '[]');
        expect(stored[0].retryCount).toBe(1);

        // Expire backoff, attempt 2
        stored[0].nextRetryAt = Date.now() - 1;
        localStorage.setItem(STORAGE_KEY, JSON.stringify(stored));
        await canvasSyncQueue.flush();
        stored = JSON.parse(localStorage.getItem(STORAGE_KEY) || '[]');
        expect(stored[0].retryCount).toBe(2);

        // Expire backoff, attempt 3
        stored[0].nextRetryAt = Date.now() - 1;
        localStorage.setItem(STORAGE_KEY, JSON.stringify(stored));
        await canvasSyncQueue.flush();
        stored = JSON.parse(localStorage.getItem(STORAGE_KEY) || '[]');

        // Permanently removed after 3 failures
        expect(stored).toHaveLength(0);
        expect(syncStateManager.getState('g-1')).toBe('failed');
    });

    test('backoff doubles each retry (1s, 2s, 4s)', async () => {
        canvasSyncQueue.add({ id: 'g-1', op: 'glyph_upsert' });
        mockApiFetch = async () => new Response(null, { status: 500 });

        // Attempt 1 → backoff ~1s
        let before = Date.now();
        await canvasSyncQueue.flush();
        let stored = JSON.parse(localStorage.getItem(STORAGE_KEY) || '[]');
        expect(stored[0].nextRetryAt - before).toBeGreaterThan(800);
        expect(stored[0].nextRetryAt - before).toBeLessThan(1500);

        // Attempt 2 → backoff ~2s
        stored[0].nextRetryAt = Date.now() - 1;
        localStorage.setItem(STORAGE_KEY, JSON.stringify(stored));
        before = Date.now();
        await canvasSyncQueue.flush();
        stored = JSON.parse(localStorage.getItem(STORAGE_KEY) || '[]');
        expect(stored[0].nextRetryAt - before).toBeGreaterThan(1800);
        expect(stored[0].nextRetryAt - before).toBeLessThan(2500);
    });

    test('new add() for same entity resets retryCount', async () => {
        canvasSyncQueue.add({ id: 'g-1', op: 'glyph_upsert' });
        mockApiFetch = async () => new Response(null, { status: 500 });

        await canvasSyncQueue.flush();
        let stored = JSON.parse(localStorage.getItem(STORAGE_KEY) || '[]');
        expect(stored[0].retryCount).toBe(1);

        // User edits glyph again → fresh entry, no backoff
        canvasSyncQueue.add({ id: 'g-1', op: 'glyph_upsert' });
        stored = JSON.parse(localStorage.getItem(STORAGE_KEY) || '[]');
        expect(stored[0].retryCount).toBeUndefined();
        expect(stored[0].nextRetryAt).toBeUndefined();
    });
});
