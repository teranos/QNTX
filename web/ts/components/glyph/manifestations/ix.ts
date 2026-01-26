/**
 * IX Manifestation - Ingest input form on canvas
 *
 * The IX manifestation renders directly on the canvas grid as a compact
 * input form for entering URLs, file paths, or any ix-compatible source.
 * Unlike window/canvas manifestations, IX doesn't morph - it's always visible.
 */

import { log, SEG } from '../../../logger';
import { IX } from '@generated/sym.js';

/**
 * Render IX manifestation form
 * Returns the HTML element for the IX input form
 */
export function renderIxManifestation(): HTMLElement {
    const container = document.createElement('div');
    container.className = 'ix-manifestation';
    container.style.padding = '12px';
    container.style.display = 'flex';
    container.style.flexDirection = 'column';
    container.style.gap = '8px';
    container.style.width = '320px';
    container.style.backgroundColor = 'var(--bg-secondary)';
    container.style.border = '1px solid var(--border-color)';
    container.style.borderRadius = '4px';

    // Header with symbol
    const header = document.createElement('div');
    header.style.display = 'flex';
    header.style.alignItems = 'center';
    header.style.gap = '8px';
    header.style.marginBottom = '4px';

    const symbol = document.createElement('span');
    symbol.textContent = IX;
    symbol.style.fontSize = '20px';

    const title = document.createElement('span');
    title.textContent = 'Ingest';
    title.style.fontSize = '14px';
    title.style.fontWeight = '500';
    title.style.color = 'var(--text-primary)';

    header.appendChild(symbol);
    header.appendChild(title);

    // Textarea input
    const textarea = document.createElement('textarea');
    textarea.className = 'ix-input-textarea';
    textarea.placeholder = 'URL or file path...';
    textarea.rows = 4;
    textarea.style.width = '100%';
    textarea.style.padding = '8px';
    textarea.style.fontSize = '13px';
    textarea.style.fontFamily = 'monospace';
    textarea.style.backgroundColor = 'var(--bg-tertiary)';
    textarea.style.color = 'var(--text-primary)';
    textarea.style.border = '1px solid var(--border-color)';
    textarea.style.borderRadius = '4px';
    textarea.style.resize = 'none';
    textarea.style.boxSizing = 'border-box';

    // Prevent drag from starting on textarea
    textarea.addEventListener('mousedown', (e) => {
        e.stopPropagation();
    });

    // Execute button
    const executeBtn = document.createElement('button');
    executeBtn.textContent = 'Execute';
    executeBtn.className = 'ix-execute-button';
    executeBtn.style.padding = '6px 12px';
    executeBtn.style.fontSize = '13px';
    executeBtn.style.fontWeight = '500';
    executeBtn.style.backgroundColor = 'var(--accent-primary)';
    executeBtn.style.color = 'var(--text-primary)';
    executeBtn.style.border = 'none';
    executeBtn.style.borderRadius = '4px';
    executeBtn.style.cursor = 'pointer';
    executeBtn.style.alignSelf = 'flex-end';

    executeBtn.addEventListener('mousedown', (e) => {
        e.stopPropagation();
    });

    executeBtn.addEventListener('click', (e) => {
        e.stopPropagation();
        const input = textarea.value.trim();
        if (!input) {
            log.debug(SEG.UI, '[IX] No input provided');
            return;
        }

        log.debug(SEG.UI, `[IX] Executing: ${input}`);
        // TODO: Wire up to ix backend execution
        // For now, just log
        alert(`IX execution not yet wired up.\n\nInput: ${input}\n\nThis will be sent to the ix backend.`);
    });

    // Assemble form
    container.appendChild(header);
    container.appendChild(textarea);
    container.appendChild(executeBtn);

    return container;
}
