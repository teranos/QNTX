/**
 * Auto-meld result glyphs — convenience helper for py-glyph and prompt-glyph
 *
 * This helper eliminates duplication between py-glyph and prompt-glyph result spawning logic.
 * Result glyphs are automatically melded below their parent glyph (bottom direction).
 */

import { log, SEG } from '../../../logger';
import type { Glyph } from '../glyph';
import { performMeld, extendComposition } from './meld-composition';
import { makeDraggable } from '../glyph-interaction';

/**
 * Auto-meld a result glyph below a parent glyph.
 * Composition-aware: extends existing composition or creates new meld.
 *
 * @param parentElement - The parent glyph element (e.g., py glyph or prompt glyph)
 * @param parentGlyphId - The parent glyph ID
 * @param parentSymbol - The parent glyph symbol (e.g., 'py', 'prompt')
 * @param parentTitle - The parent glyph title for composition metadata
 * @param resultElement - The result glyph element to meld below parent
 * @param resultGlyphId - The result glyph ID
 * @param logPrefix - Prefix for log messages (e.g., 'PyGlyph', 'Prompt')
 */
export function autoMeldResultBelow(
    parentElement: HTMLElement,
    parentGlyphId: string,
    parentSymbol: string,
    parentTitle: string,
    resultElement: HTMLElement,
    resultGlyphId: string,
    logPrefix: string
): void {
    // Check if parent is already inside a composition
    const parentComposition = parentElement.closest('.melded-composition') as HTMLElement | null;
    if (parentComposition) {
        try {
            extendComposition(parentComposition, resultElement, resultGlyphId, parentGlyphId, 'bottom', 'to');

            const updatedId = parentComposition.getAttribute('data-glyph-id') || '';
            const compositionGlyph: Glyph = {
                id: updatedId,
                title: 'Melded Composition',
                renderContent: () => parentComposition
            };
            makeDraggable(parentComposition, parentComposition, compositionGlyph, {
                logLabel: 'MeldedComposition'
            });

            log.debug(SEG.GLYPH, `[${logPrefix}] Extended composition with result below ${parentGlyphId}`);
        } catch (err) {
            log.error(SEG.GLYPH, `[${logPrefix}] Failed to extend composition with result:`, err);
        }
        return;
    }

    // Standalone parent — create new composition
    const parentGlyph: Glyph = {
        id: parentGlyphId,
        title: parentTitle,
        symbol: parentSymbol,
        renderContent: () => parentElement
    };

    const resultGlyph: Glyph = {
        id: resultGlyphId,
        title: 'Result',
        symbol: 'result',
        renderContent: () => resultElement
    };

    try {
        const composition = performMeld(parentElement, resultElement, parentGlyph, resultGlyph, 'bottom');

        const compositionGlyph: Glyph = {
            id: composition.getAttribute('data-glyph-id') || `melded-${parentGlyphId}-${resultGlyphId}`,
            title: 'Melded Composition',
            renderContent: () => composition
        };
        makeDraggable(composition, composition, compositionGlyph, {
            logLabel: 'MeldedComposition'
        });

        log.debug(SEG.GLYPH, `[${logPrefix}] Auto-melded result below ${parentGlyphId}`);
    } catch (err) {
        log.error(SEG.GLYPH, `[${logPrefix}] Failed to auto-meld result with parent:`, err);
    }
}
