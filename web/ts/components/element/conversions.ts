/**
 * Element Conversions
 *
 * Transforms one element type into another, preserving the SAME DOM element.
 * Respects the axiom: "A Element is exactly ONE DOM element for its entire lifetime."
 *
 * Conversion pattern:
 * 1. Capture layout and content from the existing element
 * 2. Tear down old element internals (runCleanup + replaceChildren)
 * 3. Repopulate the same element as the new element type (setupXxxElement)
 * 4. Update uiState atomically
 */

import type { Element } from '@teranos/elements';
import { SO, Prose } from '../../sym';
import { log, SEG } from '../../logger';
import { uiState } from '../../state/ui';
import { runCleanup } from '@teranos/elements';
import { setupPromptElement } from './prompt-element';
import { setupNoteElement } from './note-element';

/**
 * Capture position and size of an element element relative to its canvas container
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
 * Convert a note element to a prompt element (in-place mutation)
 */
export async function convertNoteToPrompt(container: HTMLElement, itemId: string): Promise<boolean> {
    const element = container.querySelector(`[data-element-id="${itemId}"]`) as HTMLElement | null;
    if (!element) {
        const existingElements = Array.from(container.querySelectorAll('[data-element-id]'))
            .map(el => (el as HTMLElement).dataset.elementId)
            .filter(Boolean);
        log.error(SEG.ELEMENT,
            `[Note→Prompt] Note element ${itemId} not found in container.${container.className} ` +
            `(${container.children.length} children, existing elements: ${existingElements.join(', ') || 'none'})`
        );
        return false;
    }

    // Block conversion if element is inside a composition
    // Uses .closest() to handle elements nested in sub-containers within compositions
    if (element.closest('.melded-composition')) {
        log.warn(SEG.ELEMENT, `[Note→Prompt] Cannot convert element ${itemId} inside composition - unmeld first`);
        return false;
    }

    const { x, y, width, height } = captureLayout(container, element);

    // Load note content from canvas state before teardown
    const existingElement = uiState.getCanvasElement(itemId);
    const noteContent = existingElement?.content ?? '';

    // Build new element model
    const promptElement: Element = {
        id: `prompt-${crypto.randomUUID()}`,
        title: 'Prompt',
        symbol: SO,
        x, y, width, height,
        renderContent: () => {
            const el = document.createElement('div');
            el.textContent = 'Prompt element';
            return el;
        }
    };

    // Add to uiState BEFORE setupPromptElement (it reads content from uiState)
    uiState.addCanvasElement({
        id: promptElement.id,
        symbol: SO,
        x, y, width, height,
        content: noteContent,
    });

    // Tear down old element internals, repopulate as prompt
    runCleanup(element);
    element.replaceChildren();
    await setupPromptElement(element, promptElement);

    // Update state atomically — remove old, keep the already-added new element
    uiState.removeCanvasElement(itemId);

    log.info(SEG.ELEMENT, `[Note→Prompt] Converted ${itemId} → ${promptElement.id} (same element)`);
    return true;
}

/**
 * Convert a result element to a note element (in-place mutation)
 *
 * Captures the execution output text and repopulates as a note.
 */
export async function convertResultToNote(container: HTMLElement, itemId: string): Promise<boolean> {
    const element = container.querySelector(`[data-element-id="${itemId}"]`) as HTMLElement | null;
    if (!element) {
        const existingElements = Array.from(container.querySelectorAll('[data-element-id]'))
            .map(el => (el as HTMLElement).dataset.elementId)
            .filter(Boolean);
        log.error(SEG.ELEMENT,
            `[Result→Note] Result element ${itemId} not found in container.${container.className} ` +
            `(${container.children.length} children, existing elements: ${existingElements.join(', ') || 'none'})`
        );
        return false;
    }

    // Block conversion if element is inside a composition
    // Uses .closest() to handle elements nested in sub-containers within compositions
    if (element.closest('.melded-composition')) {
        log.warn(SEG.ELEMENT, `[Result→Note] Cannot convert element ${itemId} inside composition - unmeld first`);
        return false;
    }

    const { x, y, width, height } = captureLayout(container, element);

    // Extract text content from the result output before teardown
    const outputEl = element.querySelector('.result-element-output');
    const outputText = outputEl?.textContent?.trim() ?? '';

    // Build new element model
    const noteElement: Element = {
        id: `note-${crypto.randomUUID()}`,
        title: 'Note',
        symbol: Prose,
        x, y, width, height,
        renderContent: () => {
            const el = document.createElement('div');
            el.textContent = 'Note element';
            return el;
        }
    };

    // Add to uiState BEFORE setupNoteElement (it reads content from uiState)
    uiState.addCanvasElement({
        id: noteElement.id,
        symbol: Prose,
        x, y, width, height,
        content: outputText,
    });

    // Tear down old element internals, repopulate as note
    runCleanup(element);
    element.replaceChildren();
    await setupNoteElement(element, noteElement);

    // Remove old element from state
    uiState.removeCanvasElement(itemId);

    log.info(SEG.ELEMENT, `[Result→Note] Converted ${itemId} → ${noteElement.id} (same element)`);
    return true;
}
