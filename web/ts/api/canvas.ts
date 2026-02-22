/**
 * Canvas API Client
 *
 * Provides HTTP API calls for persisting canvas state (glyphs and compositions)
 * to the backend database.
 */

import type { CanvasGlyphState, CompositionState } from '../state/ui';
import type { CanvasGlyph, Composition, MinimizedWindow } from '../generated/proto/glyph/proto/canvas';
import { log, SEG } from '../logger';
import { apiFetch } from '../api';
import { canvasSyncQueue } from './canvas-sync';

/**
 * Upsert a canvas glyph (create or update).
 * Enqueues for server sync — never throws.
 */
export function upsertCanvasGlyph(glyph: CanvasGlyphState): void {
    canvasSyncQueue.add({ id: glyph.id, op: 'glyph_upsert' });
}

/**
 * Delete a canvas glyph.
 * Enqueues for server sync — never throws.
 */
export function deleteCanvasGlyph(id: string): void {
    canvasSyncQueue.add({ id, op: 'glyph_delete' });
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
 * Upsert a canvas composition (create or update).
 * Enqueues for server sync — never throws.
 */
export function upsertComposition(composition: CompositionState): void {
    canvasSyncQueue.add({ id: composition.id, op: 'composition_upsert' });
}

/**
 * Delete a canvas composition.
 * Enqueues for server sync — never throws.
 */
export function deleteComposition(id: string): void {
    canvasSyncQueue.add({ id, op: 'composition_delete' });
}

/**
 * Add a minimized window.
 * Enqueues for server sync — never throws.
 */
export function addMinimizedWindow(id: string): void {
    canvasSyncQueue.add({ id, op: 'minimized_add' });
}

/**
 * Delete a minimized window.
 * Enqueues for server sync — never throws.
 */
export function deleteMinimizedWindow(id: string): void {
    canvasSyncQueue.add({ id, op: 'minimized_delete' });
}

/**
 * List all minimized windows
 */
export async function listMinimizedWindows(): Promise<MinimizedWindow[]> {
    try {
        const response = await apiFetch('/api/canvas/minimized-windows');
        if (!response.ok) {
            const error = await response.json();
            throw new Error(error.error || 'Failed to list minimized windows');
        }

        const windows = await response.json();
        log.debug(SEG.GLYPH, `[CanvasAPI] Listed ${windows.length} minimized windows`);
        return windows;
    } catch (error) {
        log.error(SEG.GLYPH, '[CanvasAPI] Failed to list minimized windows:', error);
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
 * Export the canvas as a self-contained static HTML page.
 * Triggers a file download in the browser.
 */
export async function exportCanvasStatic(): Promise<void> {
    const response = await apiFetch('/api/canvas/export/static');
    if (!response.ok) {
        const error = await response.json();
        throw new Error(error.error || 'Failed to export canvas');
    }

    const htmlContent = await response.text();
    const blob = new Blob([htmlContent], { type: 'text/html' });
    const url = URL.createObjectURL(blob);

    const a = document.createElement('a');
    a.href = url;
    a.download = 'canvas.html';
    document.body.appendChild(a);
    a.click();
    document.body.removeChild(a);
    URL.revokeObjectURL(url);

    log.info(SEG.GLYPH, '[CanvasAPI] Exported canvas as static HTML');
}

/**
 * Load all canvas state from backend (glyphs + compositions + minimized windows)
 * Converts backend format to frontend state format
 */
export async function loadCanvasState(): Promise<{
    glyphs: CanvasGlyphState[];
    compositions: CompositionState[];
    minimizedWindows: string[];
}> {
    try {
        const [glyphsResponse, compositionsResponse, minimizedResponse] = await Promise.all([
            listCanvasGlyphs(),
            listCompositions(),
            listMinimizedWindows(),
        ]);

        // Proto types flow through directly — CanvasGlyphState and CompositionState derive from proto
        const glyphs: CanvasGlyphState[] = glyphsResponse;
        const compositions: CompositionState[] = compositionsResponse;
        const minimizedWindows = (minimizedResponse || []).map(w => w.glyph_id);

        log.info(SEG.GLYPH, `[CanvasAPI] Loaded canvas state: ${glyphs.length} glyphs, ${compositions.length} compositions, ${minimizedWindows.length} minimized windows`);

        return { glyphs, compositions, minimizedWindows };
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
    local: { glyphs: CanvasGlyphState[]; compositions: CompositionState[]; minimizedWindows: string[] },
    backend: { glyphs: CanvasGlyphState[]; compositions: CompositionState[]; minimizedWindows: string[] },
): { glyphs: CanvasGlyphState[]; compositions: CompositionState[]; minimizedWindows: string[]; mergedGlyphs: number; mergedComps: number; mergedMinimized: number } {
    const localGlyphIds = new Set(local.glyphs.map(g => g.id));
    const localCompIds = new Set(local.compositions.map(c => c.id));
    const localMinIds = new Set(local.minimizedWindows);

    const newGlyphs = backend.glyphs.filter(g => !localGlyphIds.has(g.id));
    const newComps = backend.compositions.filter(c => !localCompIds.has(c.id));
    const newMinimized = backend.minimizedWindows.filter(id => !localMinIds.has(id));

    return {
        glyphs: newGlyphs.length > 0 ? [...local.glyphs, ...newGlyphs] : local.glyphs,
        compositions: newComps.length > 0 ? [...local.compositions, ...newComps] : local.compositions,
        minimizedWindows: newMinimized.length > 0 ? [...local.minimizedWindows, ...newMinimized] : local.minimizedWindows,
        mergedGlyphs: newGlyphs.length,
        mergedComps: newComps.length,
        mergedMinimized: newMinimized.length,
    };
}
