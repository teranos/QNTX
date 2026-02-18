/**
 * Tests for sync badge DOM component
 *
 * Personas:
 * - Tim: Badge creates, hides when empty, shows count when pending
 * - Spike: Badge updates reactively as queue changes
 */

import { describe, test, expect, beforeEach, mock } from 'bun:test';

let mockConnectivity: 'online' | 'degraded' | 'offline' = 'offline';

mock.module('./connectivity', () => ({
    connectivityManager: {
        get state() { return mockConnectivity; },
        subscribe(cb: (s: 'online' | 'degraded' | 'offline') => void) {
            cb(mockConnectivity);
            return () => {};
        },
    },
}));

let mockApiFetch: (path: string, init?: RequestInit) => Promise<Response>;

mock.module('./api', () => ({
    apiFetch: (path: string, init?: RequestInit) => mockApiFetch(path, init),
}));

// NOTE: Do NOT mock ./state/ui here — it's process-global and would break
// composition/UIState tests that need the real uiState. The badge tests only
// need the sync queue's size/onChange, not actual glyph data.

const { canvasSyncQueue } = await import('./api/canvas-sync');
const { initSyncBadge } = await import('./sync-badge');

describe('Sync Badge DOM', () => {
    beforeEach(() => {
        localStorage.clear();
        mockConnectivity = 'offline';
        mockApiFetch = async () => new Response(null, { status: 200 });

        // Clear DOM between tests (happy-dom accumulates)
        document.body.innerHTML = '';

        // Create system drawer header for badge insertion
        const header = document.createElement('div');
        header.id = 'system-drawer-header';
        document.body.appendChild(header);
    });

    test('Tim: initSyncBadge creates hidden badge element', () => {
        initSyncBadge();

        const badge = document.getElementById('sync-badge');
        expect(badge).not.toBeNull();
        expect(badge!.hidden).toBe(true);
        expect(badge!.classList.contains('sync-badge')).toBe(true);
    });

    test('Tim: badge shows count when items enqueued', () => {
        initSyncBadge();

        canvasSyncQueue.add({ id: 'g-1', op: 'glyph_upsert' });

        const badge = document.getElementById('sync-badge')!;
        expect(badge.hidden).toBe(false);
        expect(badge.textContent).toBe('1 pending');
    });

    test('Tim: badge hides after flush empties queue', async () => {
        initSyncBadge();

        canvasSyncQueue.add({ id: 'g-1', op: 'glyph_upsert' });
        expect(document.getElementById('sync-badge')!.hidden).toBe(false);

        await canvasSyncQueue.flush();
        expect(document.getElementById('sync-badge')!.hidden).toBe(true);
    });

    test('Spike: badge updates count as items are added', () => {
        initSyncBadge();

        canvasSyncQueue.add({ id: 'g-1', op: 'glyph_upsert' });
        expect(document.getElementById('sync-badge')!.textContent).toBe('1 pending');

        canvasSyncQueue.add({ id: 'g-2', op: 'glyph_upsert' });
        expect(document.getElementById('sync-badge')!.textContent).toBe('2 pending');
    });
});
