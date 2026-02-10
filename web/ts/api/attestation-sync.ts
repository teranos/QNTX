/**
 * Attestation Sync Queue
 *
 * Pushes locally-created attestations (IndexedDB) to the server when online.
 * Attestations created by ts-glyph are stored locally first, then synced.
 *
 * Color language: orange glyphs (local) → teal (synced to server).
 */

import { log, SEG } from '../logger';
import { apiFetch } from '../api';
import { getAttestation } from '../qntx-wasm';
import { syncStateManager } from '../state/sync-state';
import { connectivityManager } from '../connectivity';

// TODO: Migrate to IndexedDB via storage.ts (localStorage elimination)
const STORAGE_KEY = 'qntx-attestation-sync-queue';

class SyncQueueImpl {
    private flushing = false;

    private get queue(): string[] {
        try {
            const stored = localStorage.getItem(STORAGE_KEY);
            return stored ? JSON.parse(stored) : [];
        } catch {
            return [];
        }
    }

    private set queue(ids: string[]) {
        localStorage.setItem(STORAGE_KEY, JSON.stringify(ids));
    }

    /** Add attestation ID to sync queue and attempt immediate flush */
    add(id: string): void {
        const q = this.queue;
        if (!q.includes(id)) {
            q.push(id);
            this.queue = q;
            syncStateManager.setState(id, 'unsynced');
            log.debug(SEG.GLYPH, `[SyncQueue] Enqueued ${id} (queue: ${q.length})`);
        }

        if (connectivityManager.state === 'online') {
            this.flush();
        }
    }

    /** Flush all queued attestations to server */
    async flush(): Promise<void> {
        if (this.flushing) return;
        this.flushing = true;

        try {
            const q = this.queue;
            if (q.length === 0) return;

            log.debug(SEG.GLYPH, `[SyncQueue] Flushing ${q.length} attestations`);

            const remaining: string[] = [];
            for (const id of q) {
                try {
                    syncStateManager.setState(id, 'syncing');
                    const attestation = await getAttestation(id);
                    if (!attestation) {
                        log.warn(SEG.GLYPH, `[SyncQueue] Attestation ${id} not found in IndexedDB, dropping`);
                        continue;
                    }

                    const response = await apiFetch('/api/attestations', {
                        method: 'POST',
                        headers: { 'Content-Type': 'application/json' },
                        body: JSON.stringify(attestation),
                    });

                    if (response.ok) {
                        syncStateManager.setState(id, 'synced');
                        log.debug(SEG.GLYPH, `[SyncQueue] Synced ${id}`);
                    } else {
                        syncStateManager.setState(id, 'failed');
                        remaining.push(id);
                        log.warn(SEG.GLYPH, `[SyncQueue] Failed to sync ${id}: ${response.status}`);
                    }
                } catch (err) {
                    syncStateManager.setState(id, 'failed');
                    remaining.push(id);
                    log.warn(SEG.GLYPH, `[SyncQueue] Error syncing ${id}:`, err);
                }
            }

            this.queue = remaining;
        } finally {
            this.flushing = false;
        }
    }
}

export const syncQueue = new SyncQueueImpl();

// Auto-flush when connectivity returns
connectivityManager.subscribe((state) => {
    if (state === 'online') {
        syncQueue.flush();
    }
});
