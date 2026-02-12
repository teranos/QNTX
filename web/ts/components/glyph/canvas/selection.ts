/**
 * Canvas selection state — who is selected, nothing more.
 *
 * Pure state + queries + mutations.  No action bar, no DOM classes,
 * no orchestration.  That lives in canvas-glyph.ts which calls these.
 *
 * Exists as a separate module so glyph-interaction.ts can read
 * selection state without importing the canvas orchestrator
 * (breaking the circular canvas-glyph ↔ glyph-interaction import).
 */

let selectedGlyphIds = new Set<string>();

// ── Queries ────────────────────────────────────────────────────────

export function isGlyphSelected(glyphId: string): boolean {
    return selectedGlyphIds.has(glyphId);
}

export function getSelectedGlyphIds(): string[] {
    return [...selectedGlyphIds];
}

export function getSelectedGlyphElements(container: HTMLElement): HTMLElement[] {
    return [...selectedGlyphIds]
        .map(id => container.querySelector(`[data-glyph-id="${id}"]`) as HTMLElement | null)
        .filter((el): el is HTMLElement => el !== null);
}

export function hasSelection(): boolean {
    return selectedGlyphIds.size > 0;
}

export function selectionSize(): number {
    return selectedGlyphIds.size;
}

// ── Mutations ──────────────────────────────────────────────────────

export function addToSelection(glyphId: string): void {
    selectedGlyphIds.add(glyphId);
}

export function removeFromSelection(glyphId: string): void {
    selectedGlyphIds.delete(glyphId);
}

export function replaceSelection(glyphIds: string[]): void {
    selectedGlyphIds = new Set(glyphIds);
}

export function clearSelection(): void {
    selectedGlyphIds = new Set();
}
