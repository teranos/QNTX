/**
 * Glyph Conversions
 *
 * Transforms one glyph type into another, preserving position, size, and content.
 * Conversions animate the transition for visual continuity.
 */

import type { Glyph } from './glyph';
import { SO, Prose } from '@generated/sym.js';
import { log, SEG } from '../../logger';
import { uiState } from '../../state/ui';
import { getScriptStorage } from '../../storage/script-storage';
import { createPromptGlyph } from './prompt-glyph';
import { createNoteGlyph } from './note-glyph';
import { getMinimizeDuration } from './glyph';

/**
 * Animate a glyph conversion: fade out old element, fade in new element
 */
async function animateConversion(
    container: HTMLElement,
    oldElement: HTMLElement,
    newElement: HTMLElement,
): Promise<void> {
    const duration = getMinimizeDuration();

    if (duration === 0) {
        oldElement.remove();
        container.appendChild(newElement);
        return;
    }

    const half = duration * 0.4;

    // Fade out old
    const fadeOut = oldElement.animate(
        [
            { opacity: 1, transform: 'scale(1)' },
            { opacity: 0, transform: 'scale(0.96)' },
        ],
        { duration: half, easing: 'ease-in', fill: 'forwards' },
    );
    await fadeOut.finished;
    oldElement.remove();

    // Fade in new
    newElement.style.opacity = '0';
    container.appendChild(newElement);
    const fadeIn = newElement.animate(
        [
            { opacity: 0, transform: 'scale(0.96)' },
            { opacity: 1, transform: 'scale(1)' },
        ],
        { duration: half, easing: 'ease-out', fill: 'forwards' },
    );
    await fadeIn.finished;
    newElement.style.opacity = '';
}

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

    // Remove old glyph from state
    uiState.removeCanvasGlyph(glyphId);

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

    // Animate old → new
    await animateConversion(container, noteElement, promptElement);

    // Persist
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

    // Remove old glyph from state
    uiState.removeCanvasGlyph(glyphId);

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

    // Animate old → new
    await animateConversion(container, resultElement, noteElement);

    // Persist
    uiState.addCanvasGlyph({
        id: noteGlyph.id,
        symbol: Prose,
        x, y, width, height,
    });

    log.info(SEG.GLYPH, `[Result→Note] Converted ${glyphId} → ${noteGlyph.id}`);
    return true;
}
