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

/**
 * Determine composition type from glyph CSS classes (binary melding)
 */
export function getCompositionType(
    initiatorElement: HTMLElement,
    targetElement: HTMLElement
): CompositionState['type'] | null {
    const initiatorClass = initiatorElement.className;
    const targetClass = targetElement.className;

    if (initiatorClass.includes('canvas-ax-glyph')) {
        if (targetClass.includes('canvas-prompt-glyph')) {
            return 'ax-prompt';
        }
        if (targetClass.includes('canvas-py-glyph')) {
            return 'ax-py';
        }
    }

    if (initiatorClass.includes('canvas-py-glyph')) {
        if (targetClass.includes('canvas-prompt-glyph')) {
            return 'py-prompt';
        }
        if (targetClass.includes('canvas-py-glyph')) {
            return 'py-py';
        }
    }

    log.debug(SEG.GLYPH, '[Compositions] No composition type match', {
        initiatorClass,
        targetClass
    });
    return null;
}

/**
 * Get composition type for N-glyph chains
 * Returns 'multi-glyph' for chains with 3+ glyphs
 */
export function getMultiGlyphCompositionType(glyphCount: number): CompositionState['type'] {
    return glyphCount >= 3 ? 'multi-glyph' : 'ax-prompt'; // Fallback shouldn't happen
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
            type: composition.type,
            glyphIds: composition.glyphIds
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
 * Check if a glyph is part of any composition
 */
export function isGlyphInComposition(glyphId: string): boolean {
    const compositions = uiState.getCanvasCompositions();
    return compositions.some(c =>
        c.glyphIds.includes(glyphId)
    );
}

/**
 * Find composition containing a specific glyph
 */
export function findCompositionByGlyph(glyphId: string): CompositionState | null {
    const compositions = uiState.getCanvasCompositions();
    return compositions.find(c =>
        c.glyphIds.includes(glyphId)
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
    if (comp.glyphIds.length === 0) return false;

    // Get the rightmost glyph type from the composition
    // This would require looking up the glyph elements, which we'll handle in meld-system.ts
    // For now, this is a placeholder that will be used by the DOM manipulation layer

    return true; // Actual logic will be in canMeldWithComposition in meldability.ts
}
