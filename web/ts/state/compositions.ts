/**
 * Composition State Management
 *
 * High-level helpers for managing melded element compositions.
 * Compositions are persisted in UIState but managed through this module
 * to keep composition-specific logic separate.
 */

import { uiState, type CompositionState } from './ui';
import { log, SEG } from '../logger';
import { upsertComposition as apiUpsertComposition, deleteComposition as apiDeleteComposition } from '../api/canvas';

// Pure functions — canonical in @teranos/elements, re-exported here
export { buildEdgesFromChain, extractElementIds } from '@teranos/elements';
import { extractElementIds } from '@teranos/elements';

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
        log.debug(SEG.ELEMENT, '[Compositions] Updated composition', { id: composition.id });
    } else {
        // Add new
        uiState.setCanvasCompositions([...compositions, composition]);
        log.debug(SEG.ELEMENT, '[Compositions] Added composition', {
            id: composition.id,
            edges: composition.edges.length,
            elements: extractElementIds(composition.edges)
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
    log.debug(SEG.ELEMENT, '[Compositions] Removed composition', { id });

    // Enqueue for server sync (never throws)
    apiDeleteComposition(id);
}

/**
 * Check if an element is part of any composition (DAG-native)
 * Traverses edges to find if element appears in any from/to
 */
export function isElementInComposition(elementId: string): boolean {
    const compositions = uiState.getCanvasCompositions();
    return compositions.some(c =>
        c.edges.some(edge => edge.from === elementId || edge.to === elementId)
    );
}

/**
 * Find composition containing a specific element (DAG-native)
 * Traverses edges to find composition where element appears
 */
export function findCompositionByElement(elementId: string): CompositionState | null {
    const compositions = uiState.getCanvasCompositions();
    return compositions.find(c =>
        c.edges.some(edge => edge.from === elementId || edge.to === elementId)
    ) || null;
}

/**
 * Get all compositions
 */
export function getAllCompositions(): CompositionState[] {
    return uiState.getCanvasCompositions();
}

