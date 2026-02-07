/**
 * Composition State Management
 *
 * High-level helpers for managing melded glyph compositions.
 * Compositions are persisted in UIState but managed through this module
 * to keep composition-specific logic separate.
 */

import { uiState, type CompositionState } from './ui';
import { log, SEG } from '../logger';

/**
 * Determine composition type from glyph CSS classes
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
    }

    log.debug(SEG.GLYPH, '[Compositions] No composition type match', {
        initiatorClass,
        targetClass
    });
    return null;
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
            initiatorId: composition.initiatorId,
            targetId: composition.targetId
        });
    }
}

/**
 * Remove a composition from storage
 */
export function removeComposition(id: string): void {
    const compositions = uiState.getCanvasCompositions();
    const updated = compositions.filter(c => c.id !== id);
    uiState.setCanvasCompositions(updated);
    log.debug(SEG.GLYPH, '[Compositions] Removed composition', { id });
}

/**
 * Check if a glyph is part of any composition
 */
export function isGlyphInComposition(glyphId: string): boolean {
    const compositions = uiState.getCanvasCompositions();
    return compositions.some(c =>
        c.initiatorId === glyphId || c.targetId === glyphId
    );
}

/**
 * Find composition containing a specific glyph
 */
export function findCompositionByGlyph(glyphId: string): CompositionState | null {
    const compositions = uiState.getCanvasCompositions();
    return compositions.find(c =>
        c.initiatorId === glyphId || c.targetId === glyphId
    ) || null;
}

/**
 * Get all compositions
 */
export function getAllCompositions(): CompositionState[] {
    return uiState.getCanvasCompositions();
}
