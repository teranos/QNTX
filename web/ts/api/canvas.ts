/**
 * Canvas API Client
 *
 * Provides HTTP API calls for persisting canvas state (elements and compositions)
 * to the backend database.
 */

import type { CanvasElementState, CompositionState } from '../state/ui';
import type { CanvasElement, Composition, MinimizedWindow } from '../generated/proto/element/proto/canvas';
import { log, SEG } from '../logger';
import { apiFetch, apiJson } from '../client';
import { assertOk, jsonBody } from '../http-utils';
import { canvasSyncQueue } from './canvas-sync';
import { refusal } from '../self-person';
import { canvasQuery } from '../standing';

/**
 * The canvas of the namespace this person stands in, by name. Null is a
 * namespace with none: the node answered 404, and nothing is drawn for it.
 * Anything else the node would not answer is thrown in its words.
 */
export async function theCanvas(): Promise<{ name: string } | null> {
    const response = await apiFetch('/api/canvas');
    if (response.status === 404) return null;
    if (!response.ok) {
        throw new Error(`/api/canvas: ${await refusal(response)}`);
    }
    return await response.json() as { name: string };
}

/** Creates the canvas of this namespace under a name. The node's refusal is the error. */
export async function createCanvas(name: string): Promise<void> {
    const response = await apiFetch('/api/canvas', jsonBody('POST', { name }));
    if (!response.ok) {
        throw new Error(await refusal(response));
    }
}

/**
 * Upsert a canvas element (create or update).
 * Enqueues for server sync — never throws.
 */
export function upsertCanvasElement(item: CanvasElementState): void {
    canvasSyncQueue.add({ id: item.id, op: 'element_upsert' });
}

/**
 * Delete a canvas element.
 * Enqueues for server sync — never throws.
 */
export function deleteCanvasElement(id: string): void {
    canvasSyncQueue.add({ id, op: 'element_delete' });
}

/**
 * List all canvas elements
 */
export async function listCanvasElements(): Promise<CanvasElement[]> {
    try {
        const items = await apiJson<CanvasElement[]>('/api/canvas/elements' + canvasQuery());
        log.debug(SEG.ELEMENT, `[CanvasAPI] Listed ${items.length} elements`);
        return items;
    } catch (error) {
        log.error(SEG.ELEMENT, '[CanvasAPI] Failed to list elements:', error);
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
        const windows = await apiJson<MinimizedWindow[]>('/api/canvas/minimized-windows' + canvasQuery());
        log.debug(SEG.ELEMENT, `[CanvasAPI] Listed ${windows.length} minimized windows`);
        return windows;
    } catch (error) {
        log.error(SEG.ELEMENT, '[CanvasAPI] Failed to list minimized windows:', error);
        throw error;
    }
}

/**
 * List all canvas compositions
 */
export async function listCompositions(): Promise<Composition[]> {
    try {
        const compositions = await apiJson<Composition[]>('/api/canvas/compositions' + canvasQuery()) ?? [];
        log.debug(SEG.ELEMENT, `[CanvasAPI] Listed ${compositions.length} compositions`);
        return compositions;
    } catch (error) {
        log.error(SEG.ELEMENT, '[CanvasAPI] Failed to list compositions:', error);
        throw error;
    }
}

/**
 * Load all canvas state from backend (elements + compositions + minimized windows)
 * Converts backend format to frontend state format
 */
export async function loadCanvasState(): Promise<{
    elements: CanvasElementState[];
    compositions: CompositionState[];
    minimizedWindows: string[];
}> {
    try {
        const [elementsResponse, compositionsResponse, minimizedResponse] = await Promise.all([
            listCanvasElements(),
            listCompositions(),
            listMinimizedWindows(),
        ]);

        // Proto types flow through directly — CanvasElementState and CompositionState derive from proto
        const items: CanvasElementState[] = elementsResponse;
        const compositions: CompositionState[] = compositionsResponse;
        const minimizedWindows = (minimizedResponse || []).map(w => w.element_id);

        log.info(SEG.ELEMENT, `[CanvasAPI] Loaded canvas state: ${items.length} elements, ${compositions.length} compositions, ${minimizedWindows.length} minimized windows`);

        return { elements: items, compositions, minimizedWindows };
    } catch (error) {
        log.error(SEG.ELEMENT, '[CanvasAPI] Failed to load canvas state:', error);
        throw error;
    }
}

/**
 * Merge backend canvas state into local state.
 * Backend-only items are appended; local items are preserved as-is (local wins on ID conflict).
 * Pure function -- no side effects.
 */
export function mergeCanvasState(
    local: { elements: CanvasElementState[]; compositions: CompositionState[]; minimizedWindows: string[] },
    backend: { elements: CanvasElementState[]; compositions: CompositionState[]; minimizedWindows: string[] },
): { elements: CanvasElementState[]; compositions: CompositionState[]; minimizedWindows: string[]; mergedElements: number; mergedComps: number; mergedMinimized: number } {
    const localElementIds = new Set(local.elements.map(g => g.id));
    const localCompIds = new Set(local.compositions.map(c => c.id));
    const localMinIds = new Set(local.minimizedWindows);

    const newElements = backend.elements.filter(g => !localElementIds.has(g.id));
    const newComps = backend.compositions.filter(c => !localCompIds.has(c.id));
    const newMinimized = backend.minimizedWindows.filter(id => !localMinIds.has(id));

    return {
        elements: newElements.length > 0 ? [...local.elements, ...newElements] : local.elements,
        compositions: newComps.length > 0 ? [...local.compositions, ...newComps] : local.compositions,
        minimizedWindows: newMinimized.length > 0 ? [...local.minimizedWindows, ...newMinimized] : local.minimizedWindows,
        mergedElements: newElements.length,
        mergedComps: newComps.length,
        mergedMinimized: newMinimized.length,
    };
}

/**
 * Export canvas as static HTML via server-side rendering
 * Triggers browser download of self-contained HTML file
 */
export async function exportCanvasStatic(canvasId: string): Promise<void> {
    try {
        const response = await apiFetch(`/api/canvas/export?canvas_id=${encodeURIComponent(canvasId)}`);
        await assertOk(response, 'Canvas export failed');

        // Get HTML content and trigger download
        const html = await response.text();
        const blob = new Blob([html], { type: 'text/html' });
        const url = URL.createObjectURL(blob);
        const a = document.createElement('a');
        a.href = url;
        a.download = `canvas-${canvasId}.html`;
        document.body.appendChild(a);
        a.click();
        document.body.removeChild(a);
        URL.revokeObjectURL(url);

        log.info(SEG.ELEMENT, `[CanvasAPI] Exported canvas ${canvasId} (server-side rendering)`);
    } catch (error) {
        log.error(SEG.ELEMENT, '[CanvasAPI] Canvas export failed:', error);
        throw error;
    }
}
