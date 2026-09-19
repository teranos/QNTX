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
let mockConnectivity: 'online' | 'degraded' | 'offline' = 'offline';
const connectivitySubscribers = new Set<(s: 'online' | 'degraded' | 'offline') => void>();

// Mock apiFetch — controlled responses per test
let mockApiFetch: (path: string, init?: RequestInit) => Promise<Response>;

mock.module('../client', () => ({
    connectivity: {
        get state() { return mockConnectivity; },
        subscribe(cb: (s: 'online' | 'degraded' | 'offline') => void) {
            connectivitySubscribers.add(cb);
            cb(mockConnectivity);
            return () => { connectivitySubscribers.delete(cb); };
        },
        subscribeAuth: () => () => {},
    },
    apiFetch: (path: string, init?: RequestInit) => mockApiFetch(path, init),
}));

// Mock UIState — process-global, must be superset-complete (see test/mock-ui-state.ts)
import { createMockUiState } from '../test/mock-ui-state';
const { uiState, elements: mockElements, compositions: mockCompositions } = createMockUiState();
mock.module('../state/ui', () => ({ uiState }));

const { canvasSyncQueue } = await import('./canvas-sync');

const STORAGE_KEY = 'qntx-canvas-sync-queue';

describe('Canvas Sync - Tim (Happy Path)', () => {
    beforeEach(() => {
        localStorage.clear();
        mockConnectivity = 'offline';
        connectivitySubscribers.clear();
        mockApiFetch = async () => new Response(null, { status: 200 });
        mockElements.length = 0;
        mockElements.push(
            { id: 'g-1', symbol: 'ax', x: 100, y: 200 },
            { id: 'g-2', symbol: 'py', x: 300, y: 400 },
        );
        mockCompositions.length = 0;
        mockCompositions.push(
            { id: 'c-1', edges: [{ from: 'g-1', to: 'g-2', direction: 'right', position: 0 }], x: 100, y: 200 },
        );
        syncStateManager.clearState('g-1');
        syncStateManager.clearState('g-2');
        syncStateManager.clearState('c-1');
    });

    test('Tim adds element upsert, queue persists to localStorage', () => {
        canvasSyncQueue.add({ id: 'g-1', op: 'element_upsert' });

        const stored = JSON.parse(localStorage.getItem(STORAGE_KEY) || '[]');
        expect(stored).toEqual([{ id: 'g-1', op: 'element_upsert' }]);
    });

    test('Tim adds element upsert, sync state set to unsynced', () => {
        canvasSyncQueue.add({ id: 'g-1', op: 'element_upsert' });

        expect(syncStateManager.getState('g-1')).toBe('unsynced');
    });

    test('Tim flushes element upsert, synced and removed from queue', async () => {
        canvasSyncQueue.add({ id: 'g-1', op: 'element_upsert' });

        await canvasSyncQueue.flush();

        expect(syncStateManager.getState('g-1')).toBe('synced');
        const stored = JSON.parse(localStorage.getItem(STORAGE_KEY) || '[]');
        expect(stored).toEqual([]);
    });

    test('Tim flushes element upsert, POST sends correct payload', async () => {
        canvasSyncQueue.add({ id: 'g-1', op: 'element_upsert' });

        let capturedPath = '';
        let capturedBody = '';
        mockApiFetch = async (path, init) => {
            capturedPath = path;
            capturedBody = init?.body as string;
            return new Response(null, { status: 200 });
        };

        await canvasSyncQueue.flush();

        expect(capturedPath).toBe('/api/canvas/elements');
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

    test('Tim flushes element delete, DELETE sent to correct URL', async () => {
        canvasSyncQueue.add({ id: 'g-1', op: 'element_delete' });

        let capturedPath = '';
        let capturedMethod = '';
        mockApiFetch = async (path, init) => {
            capturedPath = path;
            capturedMethod = init?.method || 'GET';
            return new Response(null, { status: 200 });
        };

        await canvasSyncQueue.flush();

        expect(capturedPath).toBe('/api/canvas/elements/g-1');
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
        canvasSyncQueue.add({ id: 'g-1', op: 'element_upsert' });
        canvasSyncQueue.add({ id: 'c-1', op: 'composition_upsert' });
        canvasSyncQueue.add({ id: 'g-2', op: 'element_upsert' });

        const synced: string[] = [];
        mockApiFetch = async (path) => {
            synced.push(path);
            return new Response(null, { status: 200 });
        };

        await canvasSyncQueue.flush();

        expect(synced).toEqual([
            '/api/canvas/elements',
            '/api/canvas/compositions',
            '/api/canvas/elements',
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
        mockElements.length = 0;
        mockElements.push(
            { id: 'g-1', symbol: 'ax', x: 100, y: 200 },
            { id: 'g-2', symbol: 'py', x: 300, y: 400 },
        );
        mockCompositions.length = 0;
        mockCompositions.push(
            { id: 'c-1', edges: [{ from: 'g-1', to: 'g-2', direction: 'right', position: 0 }], x: 100, y: 200 },
        );
        syncStateManager.clearState('g-1');
        syncStateManager.clearState('g-2');
        syncStateManager.clearState('c-1');
    });

    test('Spike: duplicate element upserts collapse', () => {
        canvasSyncQueue.add({ id: 'g-1', op: 'element_upsert' });
        canvasSyncQueue.add({ id: 'g-1', op: 'element_upsert' });

        const stored = JSON.parse(localStorage.getItem(STORAGE_KEY) || '[]');
        expect(stored).toEqual([{ id: 'g-1', op: 'element_upsert' }]);
    });

    test('Spike: element delete supersedes pending upsert', () => {
        canvasSyncQueue.add({ id: 'g-1', op: 'element_upsert' });
        canvasSyncQueue.add({ id: 'g-1', op: 'element_delete' });

        const stored = JSON.parse(localStorage.getItem(STORAGE_KEY) || '[]');
        expect(stored).toEqual([{ id: 'g-1', op: 'element_delete' }]);
    });

    test('Spike: composition delete supersedes pending upsert', () => {
        canvasSyncQueue.add({ id: 'c-1', op: 'composition_upsert' });
        canvasSyncQueue.add({ id: 'c-1', op: 'composition_delete' });

        const stored = JSON.parse(localStorage.getItem(STORAGE_KEY) || '[]');
        expect(stored).toEqual([{ id: 'c-1', op: 'composition_delete' }]);
    });

    test('Spike: element and composition with same ID are independent', () => {
        canvasSyncQueue.add({ id: 'x-1', op: 'element_upsert' });
        canvasSyncQueue.add({ id: 'x-1', op: 'composition_upsert' });

        const stored = JSON.parse(localStorage.getItem(STORAGE_KEY) || '[]');
        expect(stored).toEqual([
            { id: 'x-1', op: 'element_upsert' },
            { id: 'x-1', op: 'composition_upsert' },
        ]);
    });

    test('Spike: server error keeps entry in queue', async () => {
        canvasSyncQueue.add({ id: 'g-1', op: 'element_upsert' });
        mockApiFetch = async () => new Response(null, { status: 500 });

        await canvasSyncQueue.flush();

        expect(syncStateManager.getState('g-1')).toBe('failed');
        const stored = JSON.parse(localStorage.getItem(STORAGE_KEY) || '[]');
        expect(stored).toHaveLength(1);
        expect(stored[0].id).toBe('g-1');
        expect(stored[0].op).toBe('element_upsert');
    });

    test('Spike: network error keeps entry in queue', async () => {
        canvasSyncQueue.add({ id: 'g-1', op: 'element_upsert' });
        mockApiFetch = async () => { throw new Error('network down'); };

        await canvasSyncQueue.flush();

        expect(syncStateManager.getState('g-1')).toBe('failed');
        const stored = JSON.parse(localStorage.getItem(STORAGE_KEY) || '[]');
        expect(stored).toHaveLength(1);
        expect(stored[0].id).toBe('g-1');
        expect(stored[0].op).toBe('element_upsert');
    });

    test('Spike: element not found in UIState, dropped from queue', async () => {
        canvasSyncQueue.add({ id: 'g-missing', op: 'element_upsert' });

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

    test('Spike: 404 on element delete treated as success', async () => {
        canvasSyncQueue.add({ id: 'g-1', op: 'element_delete' });
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
        canvasSyncQueue.add({ id: 'g-1', op: 'element_upsert' });

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
        canvasSyncQueue.add({ id: 'g-1', op: 'element_upsert' });

        let flushStarted = false;
        mockApiFetch = async () => {
            if (!flushStarted) {
                flushStarted = true;
                // Simulate user creating a new element while flush is in-flight
                canvasSyncQueue.add({ id: 'g-2', op: 'element_upsert' });
            }
            return new Response(null, { status: 200 });
        };

        await canvasSyncQueue.flush();

        // g-1 synced and removed, but g-2 must survive
        const stored = JSON.parse(localStorage.getItem(STORAGE_KEY) || '[]');
        expect(stored).toHaveLength(1);
        expect(stored[0].id).toBe('g-2');
        expect(stored[0].op).toBe('element_upsert');
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
        canvasSyncQueue.add({ id: 'g-1', op: 'element_upsert' });
        canvasSyncQueue.add({ id: 'c-1', op: 'composition_upsert' });
        canvasSyncQueue.add({ id: 'g-2', op: 'element_upsert' });

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
