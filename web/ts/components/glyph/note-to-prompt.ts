/**
 * Note to Prompt Conversion
 *
 * Converts a note glyph into a prompt glyph, preserving position, size, and content.
 * This enables users to start with informal notes and upgrade them to executable prompts.
 */

import type { Glyph } from './glyph';
import { SO } from '@generated/sym.js';
import { log, SEG } from '../../logger';
import { uiState } from '../../state/ui';
import { getScriptStorage } from '../../storage/script-storage';
import { createPromptGlyph } from './prompt-glyph';

/**
 * Convert a note glyph to a prompt glyph
 *
 * @param container - Canvas container element
 * @param glyphId - ID of the note glyph to convert
 */
export async function convertNoteToPrompt(container: HTMLElement, glyphId: string): Promise<void> {
    const noteElement = container.querySelector(`[data-glyph-id="${glyphId}"]`) as HTMLElement | null;
    if (!noteElement) {
        log.error(SEG.GLYPH, `[Note→Prompt] Note glyph ${glyphId} not found`);
        return;
    }

    // Get note position and size
    const canvasRect = container.getBoundingClientRect();
    const noteRect = noteElement.getBoundingClientRect();
    const x = Math.round(noteRect.left - canvasRect.left);
    const y = Math.round(noteRect.top - canvasRect.top);
    const width = Math.round(noteRect.width);
    const height = Math.round(noteRect.height);

    // Load note content from storage
    const storage = getScriptStorage();
    const noteContent = await storage.load(glyphId) ?? '';

    // Delete the note glyph
    uiState.removeCanvasGlyph(glyphId);
    noteElement.remove();

    // Create new prompt glyph at same position
    const promptGlyph: Glyph = {
        id: `prompt-${crypto.randomUUID()}`,
        title: 'Prompt',
        symbol: SO,
        x,
        y,
        width,
        height,
        renderContent: () => {
            const content = document.createElement('div');
            content.textContent = 'Prompt glyph';
            return content;
        }
    };

    const promptElement = await createPromptGlyph(promptGlyph);
    container.appendChild(promptElement);

    // Transfer note content to prompt template
    await storage.save(promptGlyph.id, noteContent);

    // Update prompt textarea to show transferred content
    const textarea = promptElement.querySelector('textarea');
    if (textarea) {
        textarea.value = noteContent;
    }

    // Persist the new prompt glyph
    uiState.addCanvasGlyph({
        id: promptGlyph.id,
        symbol: SO,
        x,
        y,
        width,
        height
    });

    log.info(SEG.GLYPH, `[Note→Prompt] Converted note ${glyphId} to prompt ${promptGlyph.id}`);
}
