/**
 * Canvas API Client
 *
 * Provides HTTP API calls for persisting canvas state (glyphs and compositions)
 * to the backend database.
 */

import type { CanvasGlyphState, CompositionState } from '../state/ui';
import type { CanvasGlyph, Composition } from '../generated/proto/glyph/proto/canvas';
import { log, SEG } from '../logger';
import { apiFetch } from '../api';
import { syncStateManager } from '../state/sync-state';

/**
 * Upsert a canvas glyph (create or update)
 */
export async function upsertCanvasGlyph(glyph: CanvasGlyphState): Promise<void> {
    // Mark as syncing
    syncStateManager.setState(glyph.id, 'syncing');

    try {
        const payload = {
            id: glyph.id,
            symbol: glyph.symbol,
            x: glyph.x,
            y: glyph.y,
            width: glyph.width,
            height: glyph.height,
            content: glyph.content,
        };

        const response = await apiFetch('/api/canvas/glyphs', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify(payload),
        });

        if (!response.ok) {
            const error = await response.json();
            throw new Error(error.error || 'Failed to upsert canvas glyph');
        }

        log.debug(SEG.GLYPH, `[CanvasAPI] Upserted glyph ${glyph.id}`);

        // Mark as synced on success
        syncStateManager.setState(glyph.id, 'synced');
    } catch (error) {
        log.error(SEG.GLYPH, `[CanvasAPI] Failed to upsert glyph ${glyph.id}:`, error);

        // Mark as failed on error
        syncStateManager.setState(glyph.id, 'failed');

        // TODO(#431): Queue operation for retry when offline
        // Instead of just throwing, enqueue to offline queue (IndexedDB)
        // Queue will automatically process when connectivity returns

        throw error;
    }
}

/**
 * Delete a canvas glyph
 */
export async function deleteCanvasGlyph(id: string): Promise<void> {
    try {
        const response = await apiFetch(`/api/canvas/glyphs/${id}`, {
            method: 'DELETE',
        });

        if (!response.ok) {
            const error = await response.json();
            throw new Error(error.error || 'Failed to delete canvas glyph');
        }

        log.debug(SEG.GLYPH, `[CanvasAPI] Deleted glyph ${id}`);

        // Clear sync state on successful deletion
        syncStateManager.clearState(id);
    } catch (error) {
        log.error(SEG.GLYPH, `[CanvasAPI] Failed to delete glyph ${id}:`, error);
        // TODO(#431): Queue deletion for retry when offline
        throw error;
    }
}

/**
 * List all canvas glyphs
 */
export async function listCanvasGlyphs(): Promise<CanvasGlyph[]> {
    try {
        const response = await apiFetch('/api/canvas/glyphs');
        if (!response.ok) {
            const error = await response.json();
            throw new Error(error.error || 'Failed to list canvas glyphs');
        }

        const glyphs = await response.json();
        log.debug(SEG.GLYPH, `[CanvasAPI] Listed ${glyphs.length} glyphs`);
        return glyphs;
    } catch (error) {
        log.error(SEG.GLYPH, '[CanvasAPI] Failed to list glyphs:', error);
        throw error;
    }
}

/**
 * Upsert a canvas composition (create or update)
 */
export async function upsertComposition(composition: CompositionState): Promise<void> {
    // Mark as syncing
    syncStateManager.setState(composition.id, 'syncing');

    try {
        const payload = {
            id: composition.id,
            edges: composition.edges,
            x: composition.x,
            y: composition.y,
        };

        const response = await apiFetch('/api/canvas/compositions', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify(payload),
        });

        if (!response.ok) {
            const error = await response.json();
            throw new Error(error.error || 'Failed to upsert composition');
        }

        log.debug(SEG.GLYPH, `[CanvasAPI] Upserted composition ${composition.id}`);

        // Mark as synced on success
        syncStateManager.setState(composition.id, 'synced');
    } catch (error) {
        log.error(SEG.GLYPH, `[CanvasAPI] Failed to upsert composition ${composition.id}:`, error);

        // Mark as failed on error
        syncStateManager.setState(composition.id, 'failed');

        // TODO(#431): Queue operation for retry when offline
        throw error;
    }
}

/**
 * Delete a canvas composition
 */
export async function deleteComposition(id: string): Promise<void> {
    try {
        const response = await apiFetch(`/api/canvas/compositions/${id}`, {
            method: 'DELETE',
        });

        if (!response.ok) {
            const error = await response.json();
            throw new Error(error.error || 'Failed to delete composition');
        }

        log.debug(SEG.GLYPH, `[CanvasAPI] Deleted composition ${id}`);

        // Clear sync state on successful deletion
        syncStateManager.clearState(id);
    } catch (error) {
        log.error(SEG.GLYPH, `[CanvasAPI] Failed to delete composition ${id}:`, error);
        // TODO(#431): Queue deletion for retry when offline
        throw error;
    }
}

/**
 * List all canvas compositions
 */
export async function listCompositions(): Promise<Composition[]> {
    try {
        const response = await apiFetch('/api/canvas/compositions');
        if (!response.ok) {
            const error = await response.json();
            throw new Error(error.error || 'Failed to list compositions');
        }

        const compositions = await response.json();
        log.debug(SEG.GLYPH, `[CanvasAPI] Listed ${compositions.length} compositions`);
        return compositions;
    } catch (error) {
        log.error(SEG.GLYPH, '[CanvasAPI] Failed to list compositions:', error);
        throw error;
    }
}

/**
 * Load all canvas state from backend (glyphs + compositions)
 * Converts backend format to frontend state format
 */
export async function loadCanvasState(): Promise<{
    glyphs: CanvasGlyphState[];
    compositions: CompositionState[];
}> {
    try {
        const [glyphsResponse, compositionsResponse] = await Promise.all([
            listCanvasGlyphs(),
            listCompositions(),
        ]);

        // Strip timestamps from proto types — frontend state doesn't track them
        const glyphs: CanvasGlyphState[] = glyphsResponse.map(
            ({ created_at, updated_at, ...state }) => state
        );

        // Composition proto type matches CompositionState exactly
        const compositions: CompositionState[] = compositionsResponse;

        log.info(SEG.GLYPH, `[CanvasAPI] Loaded canvas state: ${glyphs.length} glyphs, ${compositions.length} compositions`);

        return { glyphs, compositions };
    } catch (error) {
        log.error(SEG.GLYPH, '[CanvasAPI] Failed to load canvas state:', error);
        throw error;
    }
}

/**
 * Merge backend canvas state into local state.
 * Backend-only items are appended; local items are preserved as-is (local wins on ID conflict).
 * Pure function -- no side effects.
 */
export function mergeCanvasState(
    local: { glyphs: CanvasGlyphState[]; compositions: CompositionState[] },
    backend: { glyphs: CanvasGlyphState[]; compositions: CompositionState[] },
): { glyphs: CanvasGlyphState[]; compositions: CompositionState[]; mergedGlyphs: number; mergedComps: number } {
    const localGlyphIds = new Set(local.glyphs.map(g => g.id));
    const localCompIds = new Set(local.compositions.map(c => c.id));

    const newGlyphs = backend.glyphs.filter(g => !localGlyphIds.has(g.id));
    const newComps = backend.compositions.filter(c => !localCompIds.has(c.id));

    return {
        glyphs: newGlyphs.length > 0 ? [...local.glyphs, ...newGlyphs] : local.glyphs,
        compositions: newComps.length > 0 ? [...local.compositions, ...newComps] : local.compositions,
        mergedGlyphs: newGlyphs.length,
        mergedComps: newComps.length,
    };
}
