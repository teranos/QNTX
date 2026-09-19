/**
 * Auto-meld result elements — convenience helper for py-element and prompt-element
 *
 * This helper eliminates duplication between py-element and prompt-element result spawning logic.
 * Result elements are automatically melded below their parent element (bottom direction).
 */

import { log, SEG } from '../../../logger';
import type { Element } from '@teranos/elements';
import { performMeld, extendComposition, makeDraggable } from '@teranos/elements';

/**
 * Auto-meld a result element below a parent element.
 * Composition-aware: extends existing composition or creates new meld.
 *
 * @param parentElement - The parent element element (e.g., py element or prompt element)
 * @param parentElementId - The parent element ID
 * @param parentSymbol - The parent element symbol (e.g., 'py', 'prompt')
 * @param parentTitle - The parent element title for composition metadata
 * @param resultElement - The result element element to meld below parent
 * @param resultElementId - The result element ID
 * @param logPrefix - Prefix for log messages (e.g., 'PyElement', 'Prompt')
 */
export function autoMeldResultBelow(
    parentElement: HTMLElement,
    parentElementId: string,
    parentSymbol: string,
    parentTitle: string,
    resultElement: HTMLElement,
    resultElementId: string,
    logPrefix: string
): void {
    // Check if parent is already inside a composition
    const parentComposition = parentElement.closest('.melded-composition') as HTMLElement | null;
    if (parentComposition) {
        try {
            extendComposition(parentComposition, resultElement, resultElementId, parentElementId, 'bottom', 'to');

            const updatedId = parentComposition.getAttribute('data-element-id') || '';
            const compositionElement: Element = {
                id: updatedId,
                title: 'Melded Composition',
                renderContent: () => parentComposition
            };
            makeDraggable(parentComposition, parentComposition, compositionElement, {
                logLabel: 'MeldedComposition'
            });

            log.debug(SEG.ELEMENT, `[${logPrefix}] Extended composition with result below ${parentElementId}`);
        } catch (err) {
            log.error(SEG.ELEMENT, `[${logPrefix}] Failed to extend composition with result:`, err);
        }
        return;
    }

    // Standalone parent — create new composition
    const parentItem: Element = {
        id: parentElementId,
        title: parentTitle,
        symbol: parentSymbol,
        renderContent: () => parentElement
    };

    const resultItem: Element = {
        id: resultElementId,
        title: 'Result',
        symbol: 'result',
        renderContent: () => resultElement
    };

    try {
        const composition = performMeld(parentElement, resultElement, parentItem, resultItem, 'bottom');

        const compositionElement: Element = {
            id: composition.getAttribute('data-element-id') || `melded-${parentElementId}-${resultElementId}`,
            title: 'Melded Composition',
            renderContent: () => composition
        };
        makeDraggable(composition, composition, compositionElement, {
            logLabel: 'MeldedComposition'
        });

        log.debug(SEG.ELEMENT, `[${logPrefix}] Auto-melded result below ${parentElementId}`);
    } catch (err) {
        log.error(SEG.ELEMENT, `[${logPrefix}] Failed to auto-meld result with parent:`, err);
    }
}
