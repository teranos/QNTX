/**
 * Debounced auto-save for element content
 *
 * Shared by all elements that persist content changes to canvas state.
 */

import { uiState } from '../../state/ui';
import { log, SEG } from '../../logger';

const AUTOSAVE_DELAY = 500;

/**
 * Create a debounced auto-save function for an element.
 *
 * Call the returned function whenever content changes. It debounces at 500ms,
 * then persists via uiState.addCanvasElement({ ...existing, content }).
 *
 * @param elementId - The element's ID in canvas state
 * @param getContent - Returns the current content string to save
 * @param label - Log label (e.g. 'PyElement', 'TsElement')
 * @returns A function to call on every content change
 */
export function createAutoSave(
    elementId: string,
    getContent: () => string,
    label: string,
): { save: () => void; cancel: () => void } {
    let saveTimeout: number | undefined;

    return {
        save: () => {
            if (saveTimeout !== undefined) clearTimeout(saveTimeout);
            saveTimeout = window.setTimeout(() => {
                const existing = uiState.getCanvasElement(elementId);
                if (existing) {
                    uiState.addCanvasElement({ ...existing, content: getContent() });
                    log.debug(SEG.ELEMENT, `[${label}] Auto-saved content for ${elementId}`);
                }
            }, AUTOSAVE_DELAY);
        },
        cancel: () => {
            if (saveTimeout !== undefined) clearTimeout(saveTimeout);
        },
    };
}
