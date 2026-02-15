/**
 * Tests for canvas sync queue — offline-first canvas CRUD (#431)
 *
 * Personas:
 * - Tim: Happy path (enqueue, flush, sync succeeds)
 * - Spike: Network failures, missing UIState items, concurrent flushes, dedup, backoff
 * - Jenny: Tube journey — realistic tunnel/station cycles through the sync queue
 */

import { describe, test, expect, beforeEach, mock } from 'bun:test';
import { syncStateManager } from '../state/sync-state';

// Mock connectivity — start offline so add() doesn't auto-flush
let mockConnectivity: 'online' | 'offline' = 'offline';
const connectivitySubscribers = new Set<(s: 'online' | 'offline') => void>();

mock.module('../connectivity', () => ({
    connectivityManager: {
        get state() { return mockConnectivity; },
        subscribe(cb: (s: 'online' | 'offline') => void) {
            connectivitySubscribers.add(cb);
            cb(mockConnectivity);
            return () => { connectivitySubscribers.delete(cb); };
        },
    },
}));

// Mock apiFetch — controlled responses per test
let mockApiFetch: (path: string, init?: RequestInit) => Promise<Response>;

mock.module('../api', () => ({
    apiFetch: (path: string, init?: RequestInit) => mockApiFetch(path, init),
}));

// Mock UIState — controlled glyph/composition data per test
let mockGlyphs: Array<{ id: string; symbol: string; x: number; y: number; width?: number; height?: number; content?: string }> = [];
let mockCompositions: Array<{ id: string; edges: Array<{ from: string; to: string; direction: string; position: number }>; x: number; y: number }> = [];

mock.module('../state/ui', () => ({
    uiState: {
        getCanvasGlyphs: () => mockGlyphs,
        getCanvasCompositions: () => mockCompositions,
    },
}));

const { canvasSyncQueue } = await import('./canvas-sync');

const STORAGE_KEY = 'qntx-canvas-sync-queue';

describe('Canvas Sync - Tim (Happy Path)', () => {
    beforeEach(() => {
        localStorage.clear();
        mockConnectivity = 'offline';
        connectivitySubscribers.clear();
        mockApiFetch = async () => new Response(null, { status: 200 });
        mockGlyphs = [
            { id: 'g-1', symbol: 'ax', x: 100, y: 200 },
            { id: 'g-2', symbol: 'py', x: 300, y: 400 },
        ];
        mockCompositions = [
            { id: 'c-1', edges: [{ from: 'g-1', to: 'g-2', direction: 'right', position: 0 }], x: 100, y: 200 },
        ];
        syncStateManager.clearState('g-1');
        syncStateManager.clearState('g-2');
        syncStateManager.clearState('c-1');
    });

    test('Tim adds glyph upsert, queue persists to localStorage', () => {
        canvasSyncQueue.add({ id: 'g-1', op: 'glyph_upsert' });

        const stored = JSON.parse(localStorage.getItem(STORAGE_KEY) || '[]');
        expect(stored).toEqual([{ id: 'g-1', op: 'glyph_upsert' }]);
    });

    test('Tim adds glyph upsert, sync state set to unsynced', () => {
        canvasSyncQueue.add({ id: 'g-1', op: 'glyph_upsert' });

        expect(syncStateManager.getState('g-1')).toBe('unsynced');
    });

    test('Tim flushes glyph upsert, synced and removed from queue', async () => {
        canvasSyncQueue.add({ id: 'g-1', op: 'glyph_upsert' });

        await canvasSyncQueue.flush();

        expect(syncStateManager.getState('g-1')).toBe('synced');
        const stored = JSON.parse(localStorage.getItem(STORAGE_KEY) || '[]');
        expect(stored).toEqual([]);
    });

    test('Tim flushes glyph upsert, POST sends correct payload', async () => {
        canvasSyncQueue.add({ id: 'g-1', op: 'glyph_upsert' });

        let capturedPath = '';
        let capturedBody = '';
        mockApiFetch = async (path, init) => {
            capturedPath = path;
            capturedBody = init?.body as string;
            return new Response(null, { status: 200 });
        };

        await canvasSyncQueue.flush();

        expect(capturedPath).toBe('/api/canvas/glyphs');
        const parsed = JSON.parse(capturedBody);
        expect(parsed.id).toBe('g-1');
        expect(parsed.symbol).toBe('ax');
        expect(parsed.x).toBe(100);
        expect(parsed.y).toBe(200);
    });

    test('Tim flushes composition upsert, POST sends correct payload', async () => {
        canvasSyncQueue.add({ id: 'c-1', op: 'composition_upsert' });

        let capturedPath = '';
        let capturedBody = '';
        mockApiFetch = async (path, init) => {
            capturedPath = path;
            capturedBody = init?.body as string;
            return new Response(null, { status: 200 });
        };

        await canvasSyncQueue.flush();

        expect(capturedPath).toBe('/api/canvas/compositions');
        const parsed = JSON.parse(capturedBody);
        expect(parsed.id).toBe('c-1');
        expect(parsed.edges).toHaveLength(1);
    });

    test('Tim flushes glyph delete, DELETE sent to correct URL', async () => {
        canvasSyncQueue.add({ id: 'g-1', op: 'glyph_delete' });

        let capturedPath = '';
        let capturedMethod = '';
        mockApiFetch = async (path, init) => {
            capturedPath = path;
            capturedMethod = init?.method || 'GET';
            return new Response(null, { status: 200 });
        };

        await canvasSyncQueue.flush();

        expect(capturedPath).toBe('/api/canvas/glyphs/g-1');
        expect(capturedMethod).toBe('DELETE');
    });

    test('Tim flushes composition delete, DELETE sent to correct URL', async () => {
        canvasSyncQueue.add({ id: 'c-1', op: 'composition_delete' });

        let capturedPath = '';
        let capturedMethod = '';
        mockApiFetch = async (path, init) => {
            capturedPath = path;
            capturedMethod = init?.method || 'GET';
            return new Response(null, { status: 200 });
        };

        await canvasSyncQueue.flush();

        expect(capturedPath).toBe('/api/canvas/compositions/c-1');
        expect(capturedMethod).toBe('DELETE');
    });

    test('Tim flushes multiple ops, all synced in order', async () => {
        canvasSyncQueue.add({ id: 'g-1', op: 'glyph_upsert' });
        canvasSyncQueue.add({ id: 'c-1', op: 'composition_upsert' });
        canvasSyncQueue.add({ id: 'g-2', op: 'glyph_upsert' });

        const synced: string[] = [];
        mockApiFetch = async (path) => {
            synced.push(path);
            return new Response(null, { status: 200 });
        };

        await canvasSyncQueue.flush();

        expect(synced).toEqual([
            '/api/canvas/glyphs',
            '/api/canvas/compositions',
            '/api/canvas/glyphs',
        ]);
        expect(syncStateManager.getState('g-1')).toBe('synced');
        expect(syncStateManager.getState('c-1')).toBe('synced');
        expect(syncStateManager.getState('g-2')).toBe('synced');
    });
});

describe('Canvas Sync - Spike (Edge Cases)', () => {
    beforeEach(() => {
        localStorage.clear();
        mockConnectivity = 'offline';
        connectivitySubscribers.clear();
        mockApiFetch = async () => new Response(null, { status: 200 });
        mockGlyphs = [
            { id: 'g-1', symbol: 'ax', x: 100, y: 200 },
            { id: 'g-2', symbol: 'py', x: 300, y: 400 },
        ];
        mockCompositions = [
            { id: 'c-1', edges: [{ from: 'g-1', to: 'g-2', direction: 'right', position: 0 }], x: 100, y: 200 },
        ];
        syncStateManager.clearState('g-1');
        syncStateManager.clearState('g-2');
        syncStateManager.clearState('c-1');
    });

    test('Spike: duplicate glyph upserts collapse', () => {
        canvasSyncQueue.add({ id: 'g-1', op: 'glyph_upsert' });
        canvasSyncQueue.add({ id: 'g-1', op: 'glyph_upsert' });

        const stored = JSON.parse(localStorage.getItem(STORAGE_KEY) || '[]');
        expect(stored).toEqual([{ id: 'g-1', op: 'glyph_upsert' }]);
    });

    test('Spike: glyph delete supersedes pending upsert', () => {
        canvasSyncQueue.add({ id: 'g-1', op: 'glyph_upsert' });
        canvasSyncQueue.add({ id: 'g-1', op: 'glyph_delete' });

        const stored = JSON.parse(localStorage.getItem(STORAGE_KEY) || '[]');
        expect(stored).toEqual([{ id: 'g-1', op: 'glyph_delete' }]);
    });

    test('Spike: composition delete supersedes pending upsert', () => {
        canvasSyncQueue.add({ id: 'c-1', op: 'composition_upsert' });
        canvasSyncQueue.add({ id: 'c-1', op: 'composition_delete' });

        const stored = JSON.parse(localStorage.getItem(STORAGE_KEY) || '[]');
        expect(stored).toEqual([{ id: 'c-1', op: 'composition_delete' }]);
    });

    test('Spike: glyph and composition with same ID are independent', () => {
        canvasSyncQueue.add({ id: 'x-1', op: 'glyph_upsert' });
        canvasSyncQueue.add({ id: 'x-1', op: 'composition_upsert' });

        const stored = JSON.parse(localStorage.getItem(STORAGE_KEY) || '[]');
        expect(stored).toEqual([
            { id: 'x-1', op: 'glyph_upsert' },
            { id: 'x-1', op: 'composition_upsert' },
        ]);
    });

    test('Spike: server error keeps entry in queue', async () => {
        canvasSyncQueue.add({ id: 'g-1', op: 'glyph_upsert' });
        mockApiFetch = async () => new Response(null, { status: 500 });

        await canvasSyncQueue.flush();

        expect(syncStateManager.getState('g-1')).toBe('failed');
        const stored = JSON.parse(localStorage.getItem(STORAGE_KEY) || '[]');
        expect(stored).toHaveLength(1);
        expect(stored[0].id).toBe('g-1');
        expect(stored[0].op).toBe('glyph_upsert');
    });

    test('Spike: network error keeps entry in queue', async () => {
        canvasSyncQueue.add({ id: 'g-1', op: 'glyph_upsert' });
        mockApiFetch = async () => { throw new Error('network down'); };

        await canvasSyncQueue.flush();

        expect(syncStateManager.getState('g-1')).toBe('failed');
        const stored = JSON.parse(localStorage.getItem(STORAGE_KEY) || '[]');
        expect(stored).toHaveLength(1);
        expect(stored[0].id).toBe('g-1');
        expect(stored[0].op).toBe('glyph_upsert');
    });

    test('Spike: glyph not found in UIState, dropped from queue', async () => {
        canvasSyncQueue.add({ id: 'g-missing', op: 'glyph_upsert' });

        await canvasSyncQueue.flush();

        const stored = JSON.parse(localStorage.getItem(STORAGE_KEY) || '[]');
        expect(stored).toEqual([]);
    });

    test('Spike: composition not found in UIState, dropped from queue', async () => {
        canvasSyncQueue.add({ id: 'c-missing', op: 'composition_upsert' });

        await canvasSyncQueue.flush();

        const stored = JSON.parse(localStorage.getItem(STORAGE_KEY) || '[]');
        expect(stored).toEqual([]);
    });

    test('Spike: 404 on glyph delete treated as success', async () => {
        canvasSyncQueue.add({ id: 'g-1', op: 'glyph_delete' });
        mockApiFetch = async () => new Response(null, { status: 404 });

        await canvasSyncQueue.flush();

        const stored = JSON.parse(localStorage.getItem(STORAGE_KEY) || '[]');
        expect(stored).toEqual([]);
    });

    test('Spike: 404 on composition delete treated as success', async () => {
        canvasSyncQueue.add({ id: 'c-1', op: 'composition_delete' });
        mockApiFetch = async () => new Response(null, { status: 404 });

        await canvasSyncQueue.flush();

        const stored = JSON.parse(localStorage.getItem(STORAGE_KEY) || '[]');
        expect(stored).toEqual([]);
    });

    test('Spike: concurrent flush calls, second is no-op', async () => {
        canvasSyncQueue.add({ id: 'g-1', op: 'glyph_upsert' });

        let flushCount = 0;
        mockApiFetch = async () => {
            flushCount++;
            await new Promise(r => setTimeout(r, 50));
            return new Response(null, { status: 200 });
        };

        const flush1 = canvasSyncQueue.flush();
        const flush2 = canvasSyncQueue.flush();

        await Promise.all([flush1, flush2]);

        expect(flushCount).toBe(1);
    });

    test('Spike: add() during flush() preserves new entry', async () => {
        canvasSyncQueue.add({ id: 'g-1', op: 'glyph_upsert' });

        let flushStarted = false;
        mockApiFetch = async () => {
            if (!flushStarted) {
                flushStarted = true;
                // Simulate user creating a new glyph while flush is in-flight
                canvasSyncQueue.add({ id: 'g-2', op: 'glyph_upsert' });
            }
            return new Response(null, { status: 200 });
        };

        await canvasSyncQueue.flush();

        // g-1 synced and removed, but g-2 must survive
        const stored = JSON.parse(localStorage.getItem(STORAGE_KEY) || '[]');
        expect(stored).toHaveLength(1);
        expect(stored[0].id).toBe('g-2');
        expect(stored[0].op).toBe('glyph_upsert');
    });

    test('Spike: empty queue flush, nothing happens', async () => {
        let fetchCalled = false;
        mockApiFetch = async () => {
            fetchCalled = true;
            return new Response(null, { status: 200 });
        };

        await canvasSyncQueue.flush();

        expect(fetchCalled).toBe(false);
    });

    test('Spike: partial failure — first succeeds, second fails, third succeeds', async () => {
        canvasSyncQueue.add({ id: 'g-1', op: 'glyph_upsert' });
        canvasSyncQueue.add({ id: 'c-1', op: 'composition_upsert' });
        canvasSyncQueue.add({ id: 'g-2', op: 'glyph_upsert' });

        let callCount = 0;
        mockApiFetch = async () => {
            callCount++;
            if (callCount === 2) return new Response(null, { status: 500 });
            return new Response(null, { status: 200 });
        };

        await canvasSyncQueue.flush();

        expect(syncStateManager.getState('g-1')).toBe('synced');
        expect(syncStateManager.getState('c-1')).toBe('failed');
        expect(syncStateManager.getState('g-2')).toBe('synced');

        const stored = JSON.parse(localStorage.getItem(STORAGE_KEY) || '[]');
        expect(stored).toHaveLength(1);
        expect(stored[0].id).toBe('c-1');
        expect(stored[0].op).toBe('composition_upsert');
        expect(stored[0].retryCount).toBe(1);
    });
});
