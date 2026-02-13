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

export type CanvasSyncOp = 'glyph_upsert' | 'glyph_delete' | 'composition_upsert' | 'composition_delete';

export interface CanvasSyncEntry {
    id: string;
    op: CanvasSyncOp;
}

const STORAGE_KEY = 'qntx-canvas-sync-queue';

class CanvasSyncQueueImpl {
    private flushing = false;

    private get queue(): CanvasSyncEntry[] {
        try {
            const stored = localStorage.getItem(STORAGE_KEY);
            return stored ? JSON.parse(stored) : [];
        } catch {
            return [];
        }
    }

    private set queue(entries: CanvasSyncEntry[]) {
        localStorage.setItem(STORAGE_KEY, JSON.stringify(entries));
    }

    /** Enqueue a canvas operation with deduplication */
    add(entry: CanvasSyncEntry): void {
        const q = this.queue;

        // Dedup: for same ID and entity type, latest op wins.
        // delete supersedes pending upsert; duplicate upserts collapse.
        const entityType = entry.op.startsWith('glyph') ? 'glyph' : 'composition';
        const filtered = q.filter(e => {
            const eType = e.op.startsWith('glyph') ? 'glyph' : 'composition';
            return !(e.id === entry.id && eType === entityType);
        });
        filtered.push(entry);
        this.queue = filtered;

        if (entry.op.endsWith('upsert')) {
            syncStateManager.setState(entry.id, 'unsynced');
        }

        log.debug(SEG.GLYPH, `[CanvasSync] Enqueued ${entry.op} ${entry.id} (queue: ${filtered.length})`);

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
            for (const entry of q) {
                try {
                    const ok = await this.processEntry(entry);
                    if (!ok) {
                        remaining.push(entry);
                    }
                } catch (err) {
                    syncStateManager.setState(entry.id, 'failed');
                    remaining.push(entry);
                    log.warn(SEG.GLYPH, `[CanvasSync] Error syncing ${entry.op} ${entry.id}:`, err);
                }
            }

            this.queue = remaining;
        } finally {
            this.flushing = false;
        }
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
                x: glyph.x,
                y: glyph.y,
                width: glyph.width,
                height: glyph.height,
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
                x: composition.x,
                y: composition.y,
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
}

export const canvasSyncQueue = new CanvasSyncQueueImpl();

// Auto-flush when connectivity returns
connectivityManager.subscribe((state) => {
    if (state === 'online') {
        canvasSyncQueue.flush();
    }
});
