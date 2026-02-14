/**
 * Canvas selection state — who is selected, nothing more.
 *
 * Pure state + queries + mutations.  No action bar, no DOM classes,
 * no orchestration.  That lives in canvas-workspace-builder.ts which calls these.
 *
 * Per-canvas isolation: each canvasId has its own selection set.
 * Follows the same Map<canvasId, State> pattern as canvas-pan.ts.
 *
 * Exists as a separate module so glyph-interaction.ts can read
 * selection state without importing the canvas orchestrator
 * (breaking the circular canvas-glyph ↔ glyph-interaction import).
 */

const selectionsByCanvas = new Map<string, Set<string>>();

function getSet(canvasId: string): Set<string> {
    if (!selectionsByCanvas.has(canvasId)) {
        selectionsByCanvas.set(canvasId, new Set());
    }
    return selectionsByCanvas.get(canvasId)!;
}

// ── Queries ────────────────────────────────────────────────────────

export function isGlyphSelected(canvasId: string, glyphId: string): boolean {
    return getSet(canvasId).has(glyphId);
}

export function getSelectedGlyphIds(canvasId: string): string[] {
    return [...getSet(canvasId)];
}

export function getSelectedGlyphElements(canvasId: string, container: HTMLElement): HTMLElement[] {
    return [...getSet(canvasId)]
        .map(id => container.querySelector(`[data-glyph-id="${id}"]`) as HTMLElement | null)
        .filter((el): el is HTMLElement => el !== null);
}

export function hasSelection(canvasId: string): boolean {
    return getSet(canvasId).size > 0;
}

export function selectionSize(canvasId: string): number {
    return getSet(canvasId).size;
}

// ── Mutations ──────────────────────────────────────────────────────

export function addToSelection(canvasId: string, glyphId: string): void {
    getSet(canvasId).add(glyphId);
}

export function removeFromSelection(canvasId: string, glyphId: string): void {
    getSet(canvasId).delete(glyphId);
}

export function replaceSelection(canvasId: string, glyphIds: string[]): void {
    selectionsByCanvas.set(canvasId, new Set(glyphIds));
}

export function clearSelection(canvasId: string): void {
    selectionsByCanvas.set(canvasId, new Set());
}

/** Delete all selection state for a canvas (cleanup on subcanvas collapse) */
export function destroyCanvasSelection(canvasId: string): void {
    selectionsByCanvas.delete(canvasId);
}
