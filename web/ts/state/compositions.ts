/**
 * Composition State Management
 *
 * High-level helpers for managing melded glyph compositions.
 * Compositions are persisted in UIState but managed through this module
 * to keep composition-specific logic separate.
 */

import { uiState, type CompositionState } from './ui';
import { log, SEG } from '../logger';
import { upsertComposition as apiUpsertComposition, deleteComposition as apiDeleteComposition } from '../api/canvas';

// Pure functions — canonical in @qntx/glyphs, re-exported here
export { buildEdgesFromChain, extractGlyphIds } from '@qntx/glyphs';
import { extractGlyphIds } from '@qntx/glyphs';

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

    // Enqueue for server sync (never throws)
    apiUpsertComposition(composition);
}

/**
 * Remove a composition from storage
 */
export function removeComposition(id: string): void {
    const compositions = uiState.getCanvasCompositions();
    const updated = compositions.filter(c => c.id !== id);
    uiState.setCanvasCompositions(updated);
    log.debug(SEG.GLYPH, '[Compositions] Removed composition', { id });

    // Enqueue for server sync (never throws)
    apiDeleteComposition(id);
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

