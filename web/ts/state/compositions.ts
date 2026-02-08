/**
 * Composition State Management
 *
 * High-level helpers for managing melded glyph compositions.
 * Compositions are persisted in UIState but managed through this module
 * to keep composition-specific logic separate.
 */

import { uiState, type CompositionState, type CompositionEdge } from './ui';
import { log, SEG } from '../logger';
import { upsertComposition as apiUpsertComposition, deleteComposition as apiDeleteComposition } from '../api/canvas';

/**
 * Build edges from a linear chain of glyph IDs
 * Creates consecutive edges connecting glyphs in order
 */
export function buildEdgesFromChain(
    glyphIds: string[],
    direction: 'right' | 'top' | 'bottom' = 'right'
): CompositionEdge[] {
    if (glyphIds.length < 2) {
        return [];
    }

    const edges: CompositionEdge[] = [];
    for (let i = 0; i < glyphIds.length - 1; i++) {
        edges.push({
            from: glyphIds[i],
            to: glyphIds[i + 1],
            direction,
            position: i
        });
    }
    return edges;
}

/**
 * Extract all unique glyph IDs from edges
 * Returns deduplicated array of glyph IDs
 */
export function extractGlyphIds(edges: CompositionEdge[]): string[] {
    const ids = new Set<string>();
    for (const edge of edges) {
        ids.add(edge.from);
        ids.add(edge.to);
    }
    return Array.from(ids);
}

/**
 * Add a new composition to storage
 */
export function addComposition(composition: CompositionState): void {
    const compositions = uiState.getCanvasCompositions();
    const existing = compositions.find(c => c.id === composition.id);

    if (existing) {
        // Update existing
        const updated = compositions.map(c =>
            c.id === composition.id ? composition : c
        );
        uiState.setCanvasCompositions(updated);
        log.debug(SEG.GLYPH, '[Compositions] Updated composition', { id: composition.id });
    } else {
        // Add new
        uiState.setCanvasCompositions([...compositions, composition]);
        log.debug(SEG.GLYPH, '[Compositions] Added composition', {
            id: composition.id,
            edges: composition.edges.length,
            glyphs: extractGlyphIds(composition.edges)
        });
    }

    // Sync with backend (fire-and-forget)
    apiUpsertComposition(composition).catch(err => {
        log.error(SEG.GLYPH, '[Compositions] Failed to sync composition to backend:', err);
    });
}

/**
 * Remove a composition from storage
 */
export function removeComposition(id: string): void {
    const compositions = uiState.getCanvasCompositions();
    const updated = compositions.filter(c => c.id !== id);
    uiState.setCanvasCompositions(updated);
    log.debug(SEG.GLYPH, '[Compositions] Removed composition', { id });

    // Sync with backend (fire-and-forget)
    apiDeleteComposition(id).catch(err => {
        log.error(SEG.GLYPH, '[Compositions] Failed to delete composition from backend:', err);
    });
}

/**
 * Check if a glyph is part of any composition (DAG-native)
 * Traverses edges to find if glyph appears in any from/to
 */
export function isGlyphInComposition(glyphId: string): boolean {
    const compositions = uiState.getCanvasCompositions();
    return compositions.some(c =>
        c.edges.some(edge => edge.from === glyphId || edge.to === glyphId)
    );
}

/**
 * Find composition containing a specific glyph (DAG-native)
 * Traverses edges to find composition where glyph appears
 */
export function findCompositionByGlyph(glyphId: string): CompositionState | null {
    const compositions = uiState.getCanvasCompositions();
    return compositions.find(c =>
        c.edges.some(edge => edge.from === glyphId || edge.to === glyphId)
    ) || null;
}

/**
 * Get all compositions
 */
export function getAllCompositions(): CompositionState[] {
    return uiState.getCanvasCompositions();
}

/**
 * Check if a composition can accept a new glyph
 * A composition is meldable if the initiator glyph is compatible with the composition's rightmost glyph
 */
export function isCompositionMeldable(comp: CompositionState, initiatorGlyphType: string): boolean {
    if (comp.edges.length === 0) return false;

    // Get the rightmost glyph type from the composition
    // This would require looking up the glyph elements, which we'll handle in meld-system.ts
    // For now, this is a placeholder that will be used by the DOM manipulation layer

    return true; // Actual logic will be in canMeldWithComposition in meldability.ts
}
