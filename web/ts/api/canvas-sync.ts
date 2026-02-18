/**
 * Canvas Sync Queue
 *
 * Queues canvas glyph and composition mutations for reliable server sync.
 * Local state (UIState/IndexedDB) is always written first — this queue
 * ensures the server eventually receives the same data.
 */

import { log, SEG } from '../logger';
import { apiFetch } from '../api';
import { syncStateManager } from '../state/sync-state';
import { connectivityManager } from '../connectivity';
import { uiState } from '../state/ui';

export type CanvasSyncOp = 'glyph_upsert' | 'glyph_delete' | 'composition_upsert' | 'composition_delete' | 'minimized_add' | 'minimized_delete';

export interface CanvasSyncEntry {
    id: string;
    op: CanvasSyncOp;
    retryCount?: number;
    nextRetryAt?: number;
}

const STORAGE_KEY = 'qntx-canvas-sync-queue';
const MAX_RETRIES = 3;
const BASE_BACKOFF_MS = 1000;

/** Classify sync op into entity type for dedup (same ID + same type → collapse) */
function entityTypeOf(op: string): string {
    if (op.startsWith('glyph')) return 'glyph';
    if (op.startsWith('composition')) return 'composition';
    return 'minimized';
}

class CanvasSyncQueueImpl {
    private flushing = false;
    private flushAdditions: CanvasSyncEntry[] = [];
    private listeners = new Set<() => void>();

    private get queue(): CanvasSyncEntry[] {
        try {
            const stored = globalThis.localStorage?.getItem(STORAGE_KEY);
            return stored ? JSON.parse(stored) : [];
        } catch {
            return [];
        }
    }

    private set queue(entries: CanvasSyncEntry[]) {
        try {
            globalThis.localStorage?.setItem(STORAGE_KEY, JSON.stringify(entries));
        } catch { /* localStorage unavailable */ }
    }

    /** Number of pending entries in the queue */
    get size(): number {
        return this.queue.length;
    }

    /** Read-only snapshot of pending entries for diagnostics */
    get entries(): readonly CanvasSyncEntry[] {
        return this.queue;
    }

    /** Subscribe to queue changes. Returns unsubscribe function. */
    onChange(cb: () => void): () => void {
        this.listeners.add(cb);
        return () => { this.listeners.delete(cb); };
    }

    private notify(): void {
        for (const cb of this.listeners) cb();
    }

    /** Enqueue a canvas operation with deduplication */
    add(entry: CanvasSyncEntry): void {
        const q = this.queue;

        // Dedup: for same ID and entity type, latest op wins.
        // delete supersedes pending upsert; duplicate upserts collapse.
        // Fresh add resets retry state (user edited again → no backoff).
        const entityType = entityTypeOf(entry.op);
        const filtered = q.filter(e => {
            return !(e.id === entry.id && entityTypeOf(e.op) === entityType);
        });
        filtered.push({ id: entry.id, op: entry.op });
        this.queue = filtered;

        if (entry.op.endsWith('upsert') && !entry.op.startsWith('minimized')) {
            syncStateManager.setState(entry.id, 'unsynced');
        }

        // Buffer additions during flush so they aren't overwritten
        if (this.flushing) {
            this.flushAdditions.push({ id: entry.id, op: entry.op });
        }

        log.debug(SEG.GLYPH, `[CanvasSync] Enqueued ${entry.op} ${entry.id} (queue: ${filtered.length})`);
        this.notify();

        if (connectivityManager.state === 'online') {
            this.flush();
        }
    }

    /** Flush all queued operations to server */
    async flush(): Promise<void> {
        if (this.flushing) return;
        this.flushing = true;

        try {
            const q = this.queue;
            if (q.length === 0) return;

            log.debug(SEG.GLYPH, `[CanvasSync] Flushing ${q.length} operations`);

            const remaining: CanvasSyncEntry[] = [];
            const now = Date.now();
            for (const entry of q) {
                // Skip entries still in backoff
                if (entry.nextRetryAt && entry.nextRetryAt > now) {
                    remaining.push(entry);
                    continue;
                }

                try {
                    const ok = await this.processEntry(entry);
                    if (!ok) {
                        remaining.push(this.applyBackoff(entry));
                    }
                } catch (err) {
                    syncStateManager.setState(entry.id, 'failed');
                    remaining.push(this.applyBackoff(entry));
                    log.warn(SEG.GLYPH, `[CanvasSync] Error syncing ${entry.op} ${entry.id}:`, err);
                }
            }

            // Remove permanently failed entries (exceeded max retries)
            const survived = remaining.filter(e => {
                if (e.retryCount && e.retryCount >= MAX_RETRIES) {
                    syncStateManager.setState(e.id, 'failed');
                    log.warn(SEG.GLYPH, `[CanvasSync] Permanently failed ${e.op} ${e.id} after ${MAX_RETRIES} retries`);
                    return false;
                }
                return true;
            });

            // Merge entries added during flush (fresh adds take precedence)
            const additions = this.flushAdditions;
            this.flushAdditions = [];
            for (const entry of additions) {
                const entityType = entityTypeOf(entry.op);
                const idx = survived.findIndex(e => {
                    return e.id === entry.id && entityTypeOf(e.op) === entityType;
                });
                if (idx >= 0) survived[idx] = entry;
                else survived.push(entry);
            }

            this.queue = survived;
        } finally {
            this.flushing = false;
            this.notify();
        }
    }

    /** Increment retry count and set exponential backoff delay */
    private applyBackoff(entry: CanvasSyncEntry): CanvasSyncEntry {
        const retryCount = (entry.retryCount || 0) + 1;
        const delay = BASE_BACKOFF_MS * Math.pow(2, retryCount - 1);
        return { ...entry, retryCount, nextRetryAt: Date.now() + delay };
    }

    /** Process a single queue entry. Returns true if synced (remove from queue). */
    private async processEntry(entry: CanvasSyncEntry): Promise<boolean> {
        switch (entry.op) {
            case 'glyph_upsert':
                return this.syncGlyphUpsert(entry.id);
            case 'glyph_delete':
                return this.syncGlyphDelete(entry.id);
            case 'composition_upsert':
                return this.syncCompositionUpsert(entry.id);
            case 'composition_delete':
                return this.syncCompositionDelete(entry.id);
            case 'minimized_add':
                return this.syncMinimizedAdd(entry.id);
            case 'minimized_delete':
                return this.syncMinimizedDelete(entry.id);
        }
    }

    private async syncGlyphUpsert(id: string): Promise<boolean> {
        const glyph = uiState.getCanvasGlyphs().find(g => g.id === id);
        if (!glyph) {
            log.warn(SEG.GLYPH, `[CanvasSync] Glyph ${id} not found in UIState, dropping`);
            return true;
        }

        syncStateManager.setState(id, 'syncing');
        const response = await apiFetch('/api/canvas/glyphs', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({
                id: glyph.id,
                symbol: glyph.symbol,
                x: Math.round(glyph.x),
                y: Math.round(glyph.y),
                width: glyph.width != null ? Math.round(glyph.width) : undefined,
                height: glyph.height != null ? Math.round(glyph.height) : undefined,
                content: glyph.content,
            }),
        });

        if (response.ok) {
            syncStateManager.setState(id, 'synced');
            log.debug(SEG.GLYPH, `[CanvasSync] Synced glyph ${id}`);
            return true;
        }

        syncStateManager.setState(id, 'failed');
        log.warn(SEG.GLYPH, `[CanvasSync] Failed to sync glyph ${id}: ${response.status}`);
        return false;
    }

    private async syncGlyphDelete(id: string): Promise<boolean> {
        const response = await apiFetch(`/api/canvas/glyphs/${id}`, { method: 'DELETE' });

        if (response.ok || response.status === 404) {
            syncStateManager.clearState(id);
            log.debug(SEG.GLYPH, `[CanvasSync] Deleted glyph ${id}`);
            return true;
        }

        syncStateManager.setState(id, 'failed');
        log.warn(SEG.GLYPH, `[CanvasSync] Failed to delete glyph ${id}: ${response.status}`);
        return false;
    }

    private async syncCompositionUpsert(id: string): Promise<boolean> {
        const composition = uiState.getCanvasCompositions().find(c => c.id === id);
        if (!composition) {
            log.warn(SEG.GLYPH, `[CanvasSync] Composition ${id} not found in UIState, dropping`);
            return true;
        }

        syncStateManager.setState(id, 'syncing');
        const response = await apiFetch('/api/canvas/compositions', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({
                id: composition.id,
                edges: composition.edges,
                x: Math.round(composition.x),
                y: Math.round(composition.y),
            }),
        });

        if (response.ok) {
            syncStateManager.setState(id, 'synced');
            log.debug(SEG.GLYPH, `[CanvasSync] Synced composition ${id}`);
            return true;
        }

        syncStateManager.setState(id, 'failed');
        log.warn(SEG.GLYPH, `[CanvasSync] Failed to sync composition ${id}: ${response.status}`);
        return false;
    }

    private async syncCompositionDelete(id: string): Promise<boolean> {
        const response = await apiFetch(`/api/canvas/compositions/${id}`, { method: 'DELETE' });

        if (response.ok || response.status === 404) {
            syncStateManager.clearState(id);
            log.debug(SEG.GLYPH, `[CanvasSync] Deleted composition ${id}`);
            return true;
        }

        syncStateManager.setState(id, 'failed');
        log.warn(SEG.GLYPH, `[CanvasSync] Failed to delete composition ${id}: ${response.status}`);
        return false;
    }

    private async syncMinimizedAdd(id: string): Promise<boolean> {
        const response = await apiFetch('/api/canvas/minimized-windows', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ glyph_id: id }),
        });

        if (response.ok) {
            log.debug(SEG.GLYPH, `[CanvasSync] Synced minimized window ${id}`);
            return true;
        }

        log.warn(SEG.GLYPH, `[CanvasSync] Failed to sync minimized window ${id}: ${response.status}`);
        return false;
    }

    private async syncMinimizedDelete(id: string): Promise<boolean> {
        const response = await apiFetch(`/api/canvas/minimized-windows/${id}`, { method: 'DELETE' });

        if (response.ok || response.status === 404) {
            log.debug(SEG.GLYPH, `[CanvasSync] Deleted minimized window ${id}`);
            return true;
        }

        log.warn(SEG.GLYPH, `[CanvasSync] Failed to delete minimized window ${id}: ${response.status}`);
        return false;
    }
}

export const canvasSyncQueue = new CanvasSyncQueueImpl();

// Auto-flush when connectivity returns
connectivityManager.subscribe((state) => {
    if (state === 'online') {
        canvasSyncQueue.flush();
    }
});
