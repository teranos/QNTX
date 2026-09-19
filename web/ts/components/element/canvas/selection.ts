/**
 * Canvas selection state — who is selected, nothing more.
 *
 * Pure state + queries + mutations.  No action bar, no DOM classes,
 * no orchestration.  That lives in canvas-workspace-builder.ts which calls these.
 *
 * Per-canvas isolation: each canvasId has its own selection set.
 * Follows the same Map<canvasId, State> pattern as canvas-pan.ts.
 *
 * Exists as a separate module so element-interaction.ts can read
 * selection state without importing the canvas orchestrator
 * (breaking the circular canvas-element ↔ element-interaction import).
 */

const selectionsByCanvas = new Map<string, Set<string>>();

function getSet(canvasId: string): Set<string> {
    if (!selectionsByCanvas.has(canvasId)) {
        selectionsByCanvas.set(canvasId, new Set());
    }
    return selectionsByCanvas.get(canvasId)!;
}

// ── Queries ────────────────────────────────────────────────────────

export function isElementSelected(canvasId: string, elementId: string): boolean {
    return getSet(canvasId).has(elementId);
}

export function getSelectedElementIds(canvasId: string): string[] {
    return [...getSet(canvasId)];
}

export function getSelectedElementElements(canvasId: string, container: HTMLElement): HTMLElement[] {
    return [...getSet(canvasId)]
        .map(id => container.querySelector(`[data-element-id="${id}"]`) as HTMLElement | null)
        .filter((el): el is HTMLElement => el !== null);
}

export function hasSelection(canvasId: string): boolean {
    return getSet(canvasId).size > 0;
}

export function selectionSize(canvasId: string): number {
    return getSet(canvasId).size;
}

// ── Mutations ──────────────────────────────────────────────────────

export function addToSelection(canvasId: string, elementId: string): void {
    getSet(canvasId).add(elementId);
}

export function removeFromSelection(canvasId: string, elementId: string): void {
    getSet(canvasId).delete(elementId);
}

export function replaceSelection(canvasId: string, elementIds: string[]): void {
    selectionsByCanvas.set(canvasId, new Set(elementIds));
}

export function clearSelection(canvasId: string): void {
    selectionsByCanvas.set(canvasId, new Set());
}

/** Delete all selection state for a canvas (cleanup on subcanvas collapse) */
export function destroyCanvasSelection(canvasId: string): void {
    selectionsByCanvas.delete(canvasId);
}
