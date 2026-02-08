/**
 * Glyph Conversions
 *
 * Transforms one glyph type into another, preserving the SAME DOM element.
 * Respects the axiom: "A Glyph is exactly ONE DOM element for its entire lifetime."
 *
 * Conversion pattern:
 * 1. Capture layout and content from the existing element
 * 2. Tear down old glyph internals (runCleanup + replaceChildren)
 * 3. Repopulate the same element as the new glyph type (setupXxxGlyph)
 * 4. Update uiState atomically
 */

import type { Glyph } from './glyph';
import { SO, Prose } from '@generated/sym.js';
import { log, SEG } from '../../logger';
import { uiState } from '../../state/ui';
import { getScriptStorage } from '../../storage/script-storage';
import { runCleanup } from './glyph-interaction';
import { setupPromptGlyph } from './prompt-glyph';
import { setupNoteGlyph } from './note-glyph';

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
 * Convert a note glyph to a prompt glyph (in-place mutation)
 */
export async function convertNoteToPrompt(container: HTMLElement, glyphId: string): Promise<boolean> {
    const element = container.querySelector(`[data-glyph-id="${glyphId}"]`) as HTMLElement | null;
    if (!element) {
        const existingGlyphs = Array.from(container.querySelectorAll('[data-glyph-id]'))
            .map(el => (el as HTMLElement).dataset.glyphId)
            .filter(Boolean);
        log.error(SEG.GLYPH,
            `[Note→Prompt] Note glyph ${glyphId} not found in container.${container.className} ` +
            `(${container.children.length} children, existing glyphs: ${existingGlyphs.join(', ') || 'none'})`
        );
        return false;
    }

    // Block conversion if glyph is inside a composition
    if (element.parentElement?.classList.contains('glyph-composition')) {
        log.warn(SEG.GLYPH, `[Note→Prompt] Cannot convert glyph ${glyphId} inside composition - unmeld first`);
        return false;
    }

    const { x, y, width, height } = captureLayout(container, element);

    // Load note content before teardown
    const storage = getScriptStorage();
    const noteContent = await storage.load(glyphId) ?? '';

    // Build new glyph model
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

    // Transfer content to new storage key before setup (setupPromptGlyph loads from storage)
    await storage.save(promptGlyph.id, noteContent);

    try {
        await storage.delete(glyphId);
    } catch (err) {
        log.warn(SEG.GLYPH, `[Note→Prompt] Failed to delete old note storage for ${glyphId}:`, err);
        // Continue with conversion even if delete fails - orphaned data is non-critical
    }

    // Tear down old glyph internals, repopulate as prompt
    runCleanup(element);
    element.replaceChildren();
    await setupPromptGlyph(element, promptGlyph);

    // Update state atomically
    uiState.removeCanvasGlyph(glyphId);
    uiState.addCanvasGlyph({
        id: promptGlyph.id,
        symbol: SO,
        x, y, width, height,
    });

    log.info(SEG.GLYPH, `[Note→Prompt] Converted ${glyphId} → ${promptGlyph.id} (same element)`);
    return true;
}

/**
 * Convert a result glyph to a note glyph (in-place mutation)
 *
 * Captures the execution output text and repopulates as a note.
 */
export async function convertResultToNote(container: HTMLElement, glyphId: string): Promise<boolean> {
    const element = container.querySelector(`[data-glyph-id="${glyphId}"]`) as HTMLElement | null;
    if (!element) {
        const existingGlyphs = Array.from(container.querySelectorAll('[data-glyph-id]'))
            .map(el => (el as HTMLElement).dataset.glyphId)
            .filter(Boolean);
        log.error(SEG.GLYPH,
            `[Result→Note] Result glyph ${glyphId} not found in container.${container.className} ` +
            `(${container.children.length} children, existing glyphs: ${existingGlyphs.join(', ') || 'none'})`
        );
        return false;
    }

    // Block conversion if glyph is inside a composition
    if (element.parentElement?.classList.contains('glyph-composition')) {
        log.warn(SEG.GLYPH, `[Result→Note] Cannot convert glyph ${glyphId} inside composition - unmeld first`);
        return false;
    }

    const { x, y, width, height } = captureLayout(container, element);

    // Extract text content from the result output before teardown
    const outputEl = element.querySelector('.result-glyph-output');
    const outputText = outputEl?.textContent?.trim() ?? '';

    // Build new glyph model
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

    // Save content before setup (setupNoteGlyph loads from storage)
    const storage = getScriptStorage();
    await storage.save(noteGlyph.id, outputText);

    // Tear down old glyph internals, repopulate as note
    runCleanup(element);
    element.replaceChildren();
    await setupNoteGlyph(element, noteGlyph);

    // Update state atomically
    uiState.removeCanvasGlyph(glyphId);
    uiState.addCanvasGlyph({
        id: noteGlyph.id,
        symbol: Prose,
        x, y, width, height,
    });

    log.info(SEG.GLYPH, `[Result→Note] Converted ${glyphId} → ${noteGlyph.id} (same element)`);
    return true;
}
