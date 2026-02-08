/**
 * Glyph Conversions
 *
 * Transforms one glyph type into another, preserving position, size, and content.
 * Swap is atomic: old element removed and new element added in the same tick
 * so state and DOM are never out of sync.
 */

import type { Glyph } from './glyph';
import { SO, Prose } from '@generated/sym.js';
import { log, SEG } from '../../logger';
import { uiState } from '../../state/ui';
import { getScriptStorage } from '../../storage/script-storage';
import { createPromptGlyph } from './prompt-glyph';
import { createNoteGlyph } from './note-glyph';

/**
 * Capture position and size of a glyph element relative to its canvas container
 */
function captureLayout(container: HTMLElement, element: HTMLElement) {
    const canvasRect = container.getBoundingClientRect();
    const elRect = element.getBoundingClientRect();
    return {
        x: Math.round(elRect.left - canvasRect.left),
        y: Math.round(elRect.top - canvasRect.top),
        width: Math.round(elRect.width),
        height: Math.round(elRect.height),
    };
}

/**
 * Convert a note glyph to a prompt glyph
 */
export async function convertNoteToPrompt(container: HTMLElement, glyphId: string): Promise<boolean> {
    const noteElement = container.querySelector(`[data-glyph-id="${glyphId}"]`) as HTMLElement | null;
    if (!noteElement) {
        log.error(SEG.GLYPH, `[Note→Prompt] Note glyph ${glyphId} not found in container`);
        return false;
    }

    const { x, y, width, height } = captureLayout(container, noteElement);

    // Load note content
    const storage = getScriptStorage();
    const noteContent = await storage.load(glyphId) ?? '';

    // Create new prompt glyph
    const promptGlyph: Glyph = {
        id: `prompt-${crypto.randomUUID()}`,
        title: 'Prompt',
        symbol: SO,
        x, y, width, height,
        renderContent: () => {
            const el = document.createElement('div');
            el.textContent = 'Prompt glyph';
            return el;
        }
    };

    const promptElement = await createPromptGlyph(promptGlyph);

    // Transfer content
    await storage.save(promptGlyph.id, noteContent);
    await storage.delete(glyphId);

    const textarea = promptElement.querySelector('textarea');
    if (textarea) {
        textarea.value = noteContent;
    }

    // Atomic swap: remove old, add new in same tick
    uiState.removeCanvasGlyph(glyphId);
    noteElement.remove();
    container.appendChild(promptElement);
    uiState.addCanvasGlyph({
        id: promptGlyph.id,
        symbol: SO,
        x, y, width, height,
    });

    log.info(SEG.GLYPH, `[Note→Prompt] Converted ${glyphId} → ${promptGlyph.id}`);
    return true;
}

/**
 * Convert a result glyph to a note glyph
 *
 * Captures the execution output text and creates a note containing it.
 */
export async function convertResultToNote(container: HTMLElement, glyphId: string): Promise<boolean> {
    const resultElement = container.querySelector(`[data-glyph-id="${glyphId}"]`) as HTMLElement | null;
    if (!resultElement) {
        log.error(SEG.GLYPH, `[Result→Note] Result glyph ${glyphId} not found in container`);
        return false;
    }

    const { x, y, width, height } = captureLayout(container, resultElement);

    // Extract text content from the result output
    const outputEl = resultElement.querySelector('.result-glyph-output');
    const outputText = outputEl?.textContent?.trim() ?? '';

    // Create new note glyph
    const noteGlyph: Glyph = {
        id: `note-${crypto.randomUUID()}`,
        title: 'Note',
        symbol: Prose,
        x, y, width, height,
        renderContent: () => {
            const el = document.createElement('div');
            el.textContent = 'Note glyph';
            return el;
        }
    };

    // Save content before rendering (createNoteGlyph loads from storage)
    const storage = getScriptStorage();
    await storage.save(noteGlyph.id, outputText);

    const noteElement = await createNoteGlyph(noteGlyph);

    // Atomic swap: remove old, add new in same tick
    uiState.removeCanvasGlyph(glyphId);
    resultElement.remove();
    container.appendChild(noteElement);
    uiState.addCanvasGlyph({
        id: noteGlyph.id,
        symbol: Prose,
        x, y, width, height,
    });

    log.info(SEG.GLYPH, `[Result→Note] Converted ${glyphId} → ${noteGlyph.id}`);
    return true;
}
