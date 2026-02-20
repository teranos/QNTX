/**
 * Tests for attestation sync queue — critical path
 *
 * Personas:
 * - Tim: Happy path (enqueue, flush, sync succeeds)
 * - Spike: Network failures, missing attestations, concurrent flushes
 */

import { describe, test, expect, beforeEach, mock } from 'bun:test';
import { syncStateManager } from '../state/sync-state';

// Mock connectivity — start offline so add() doesn't auto-flush
let mockConnectivity: 'online' | 'degraded' | 'offline' = 'offline';
const connectivitySubscribers = new Set<(s: 'online' | 'degraded' | 'offline') => void>();

mock.module('../connectivity', () => ({
    connectivityManager: {
        get state() { return mockConnectivity; },
        subscribe(cb: (s: 'online' | 'degraded' | 'offline') => void) {
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

// Mock qntx-wasm — must include ALL exports any consumer needs (mock.module is process-global)
let mockGetAttestation: (id: string) => Promise<unknown>;

mock.module('../qntx-wasm', () => ({
    getAttestation: (id: string) => mockGetAttestation(id),
    putAttestation: async (a: unknown) => a,
    queryAttestations: () => [],
    parseQuery: () => ({ ok: false, error: 'no wasm in test' }),
    getCompletions: () => ({ slot: 'subjects', prefix: '', items: [] }),
}));

const { syncQueue } = await import('./attestation-sync');

const STORAGE_KEY = 'qntx-attestation-sync-queue';

describe('Attestation Sync - Tim (Happy Path)', () => {
    beforeEach(() => {
        localStorage.clear();
        mockConnectivity = 'offline';
        connectivitySubscribers.clear();
        mockApiFetch = async () => new Response(null, { status: 200 });
        mockGetAttestation = async (id) => ({ id, subjects: ['TEST'], predicates: ['is'], contexts: ['QNTX'] });

        // Clear any pending sync states
        syncStateManager.clearState('att-1');
        syncStateManager.clearState('att-2');
        syncStateManager.clearState('att-3');
    });

    test('Tim adds attestation, queue persists to localStorage', () => {
        syncQueue.add('att-1');

        const stored = JSON.parse(localStorage.getItem(STORAGE_KEY) || '[]');
        expect(stored).toEqual(['att-1']);
    });

    test('Tim adds attestation, sync state set to unsynced', () => {
        syncQueue.add('att-1');

        expect(syncStateManager.getState('att-1')).toBe('unsynced');
    });

    test('Tim adds duplicate, queue stays deduplicated', () => {
        syncQueue.add('att-1');
        syncQueue.add('att-1');

        const stored = JSON.parse(localStorage.getItem(STORAGE_KEY) || '[]');
        expect(stored).toEqual(['att-1']);
    });

    test('Tim flushes, attestation synced and removed from queue', async () => {
        syncQueue.add('att-1');

        await syncQueue.flush();

        expect(syncStateManager.getState('att-1')).toBe('synced');
        const stored = JSON.parse(localStorage.getItem(STORAGE_KEY) || '[]');
        expect(stored).toEqual([]);
    });

    test('Tim flushes multiple, all synced in order', async () => {
        syncQueue.add('att-1');
        syncQueue.add('att-2');
        syncQueue.add('att-3');

        const synced: string[] = [];
        mockApiFetch = async (_path, init) => {
            const body = JSON.parse(init?.body as string);
            synced.push(body.id);
            return new Response(null, { status: 200 });
        };

        await syncQueue.flush();

        expect(synced).toEqual(['att-1', 'att-2', 'att-3']);
        expect(syncStateManager.getState('att-1')).toBe('synced');
        expect(syncStateManager.getState('att-2')).toBe('synced');
        expect(syncStateManager.getState('att-3')).toBe('synced');
    });

    test('Tim flushes, POST sends correct payload', async () => {
        syncQueue.add('att-1');

        let capturedPath = '';
        let capturedBody = '';
        mockApiFetch = async (path, init) => {
            capturedPath = path;
            capturedBody = init?.body as string;
            return new Response(null, { status: 200 });
        };

        await syncQueue.flush();

        expect(capturedPath).toBe('/api/attestations');
        const parsed = JSON.parse(capturedBody);
        expect(parsed.id).toBe('att-1');
        expect(parsed.subjects).toEqual(['TEST']);
    });
});

describe('Attestation Sync - Spike (Edge Cases)', () => {
    beforeEach(() => {
        localStorage.clear();
        mockConnectivity = 'offline';
        connectivitySubscribers.clear();
        mockApiFetch = async () => new Response(null, { status: 200 });
        mockGetAttestation = async (id) => ({ id, subjects: ['TEST'], predicates: ['is'], contexts: ['QNTX'] });

        syncStateManager.clearState('att-1');
        syncStateManager.clearState('att-2');
        syncStateManager.clearState('att-3');
    });

    test('Spike triggers flush with server error, attestation stays in queue', async () => {
        syncQueue.add('att-1');
        mockApiFetch = async () => new Response(null, { status: 500 });

        await syncQueue.flush();

        expect(syncStateManager.getState('att-1')).toBe('failed');
        const stored = JSON.parse(localStorage.getItem(STORAGE_KEY) || '[]');
        expect(stored).toEqual(['att-1']);
    });

    test('Spike triggers flush with network error, attestation stays in queue', async () => {
        syncQueue.add('att-1');
        mockApiFetch = async () => { throw new Error('network down'); };

        await syncQueue.flush();

        expect(syncStateManager.getState('att-1')).toBe('failed');
        const stored = JSON.parse(localStorage.getItem(STORAGE_KEY) || '[]');
        expect(stored).toEqual(['att-1']);
    });

    test('Spike flushes attestation missing from IndexedDB, dropped from queue', async () => {
        syncQueue.add('att-1');
        mockGetAttestation = async () => null;

        await syncQueue.flush();

        // Dropped — not in remaining queue, no 'failed' state
        const stored = JSON.parse(localStorage.getItem(STORAGE_KEY) || '[]');
        expect(stored).toEqual([]);
    });

    test('Spike partial failure: first succeeds, second fails, third succeeds', async () => {
        syncQueue.add('att-1');
        syncQueue.add('att-2');
        syncQueue.add('att-3');

        let callCount = 0;
        mockApiFetch = async () => {
            callCount++;
            if (callCount === 2) return new Response(null, { status: 500 });
            return new Response(null, { status: 200 });
        };

        await syncQueue.flush();

        expect(syncStateManager.getState('att-1')).toBe('synced');
        expect(syncStateManager.getState('att-2')).toBe('failed');
        expect(syncStateManager.getState('att-3')).toBe('synced');

        const stored = JSON.parse(localStorage.getItem(STORAGE_KEY) || '[]');
        expect(stored).toEqual(['att-2']);
    });

    test('Spike concurrent flush calls, second is no-op', async () => {
        syncQueue.add('att-1');

        let flushCount = 0;
        mockApiFetch = async () => {
            flushCount++;
            // Slow response to keep first flush active
            await new Promise(r => setTimeout(r, 50));
            return new Response(null, { status: 200 });
        };

        const flush1 = syncQueue.flush();
        const flush2 = syncQueue.flush(); // Should return immediately

        await Promise.all([flush1, flush2]);

        expect(flushCount).toBe(1);
    });

    test('Spike flushes empty queue, nothing happens', async () => {
        let fetchCalled = false;
        mockApiFetch = async () => {
            fetchCalled = true;
            return new Response(null, { status: 200 });
        };

        await syncQueue.flush();

        expect(fetchCalled).toBe(false);
    });
});
