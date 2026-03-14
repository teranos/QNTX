/**
 * Debounced auto-save for glyph content
 *
 * Shared by all glyphs that persist content changes to canvas state.
 */

import { uiState } from '../../state/ui';
import { log, SEG } from '../../logger';

const AUTOSAVE_DELAY = 500;

/**
 * Create a debounced auto-save function for a glyph.
 *
 * Call the returned function whenever content changes. It debounces at 500ms,
 * then persists via uiState.addCanvasGlyph({ ...existing, content }).
 *
 * @param glyphId - The glyph's ID in canvas state
 * @param getContent - Returns the current content string to save
 * @param label - Log label (e.g. 'PyGlyph', 'TsGlyph')
 * @returns A function to call on every content change
 */
export function createAutoSave(
    glyphId: string,
    getContent: () => string,
    label: string,
): { save: () => void; cancel: () => void } {
    let saveTimeout: number | undefined;

    return {
        save: () => {
            if (saveTimeout !== undefined) clearTimeout(saveTimeout);
            saveTimeout = window.setTimeout(() => {
                const existing = uiState.getCanvasGlyphs().find(g => g.id === glyphId);
                if (existing) {
                    uiState.addCanvasGlyph({ ...existing, content: getContent() });
                    log.debug(SEG.GLYPH, `[${label}] Auto-saved content for ${glyphId}`);
                }
            }, AUTOSAVE_DELAY);
        },
        cancel: () => {
            if (saveTimeout !== undefined) clearTimeout(saveTimeout);
        },
    };
}
