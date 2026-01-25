/**
 * IX Glyph - Ingest data from external sources
 *
 * Provides a form for entering URLs, file paths, or any ix-compatible input.
 * Executes ingestion and creates attestations from the imported data.
 */

import type { Glyph } from './glyph';
import { IX } from '@generated/sym.js';
import { log, SEG } from '../../logger';

/**
 * Create an IX glyph with input form
 */
export function createIxGlyph(gridX: number, gridY: number): Glyph {
    return {
        id: `ix-${crypto.randomUUID()}`,
        title: 'Ingest',
        symbol: IX,
        gridX,
        gridY,
        renderContent: () => createIxInputForm()
    };
}

/**
 * Create input form for IX glyph
 * Allows entering URLs, file paths, or any ix-compatible input
 */
function createIxInputForm(): HTMLElement {
    const container = document.createElement('div');
    container.className = 'ix-input-form';
    container.style.padding = '16px';
    container.style.display = 'flex';
    container.style.flexDirection = 'column';
    container.style.gap = '12px';
    container.style.minWidth = '400px';

    // Label
    const label = document.createElement('label');
    label.textContent = 'Source:';
    label.style.fontSize = '14px';
    label.style.fontWeight = '500';
    label.style.color = 'var(--text-primary)';

    // Textarea input
    const textarea = document.createElement('textarea');
    textarea.className = 'ix-input-textarea';
    textarea.placeholder = 'Enter URL, file path, or data source...\n\nExamples:\n• https://api.example.com/data\n• file:///path/to/data.json\n• /local/path/to/file';
    textarea.rows = 6;
    textarea.style.width = '100%';
    textarea.style.padding = '8px';
    textarea.style.fontSize = '14px';
    textarea.style.fontFamily = 'monospace';
    textarea.style.backgroundColor = 'var(--bg-tertiary)';
    textarea.style.color = 'var(--text-primary)';
    textarea.style.border = '1px solid var(--border-color)';
    textarea.style.borderRadius = '4px';
    textarea.style.resize = 'vertical';

    // Button container
    const buttonContainer = document.createElement('div');
    buttonContainer.style.display = 'flex';
    buttonContainer.style.gap = '8px';
    buttonContainer.style.justifyContent = 'flex-end';

    // Execute button
    const executeBtn = document.createElement('button');
    executeBtn.textContent = 'Execute';
    executeBtn.className = 'ix-execute-button';
    executeBtn.style.padding = '8px 16px';
    executeBtn.style.fontSize = '14px';
    executeBtn.style.fontWeight = '500';
    executeBtn.style.backgroundColor = 'var(--accent-primary)';
    executeBtn.style.color = 'var(--text-primary)';
    executeBtn.style.border = 'none';
    executeBtn.style.borderRadius = '4px';
    executeBtn.style.cursor = 'pointer';

    executeBtn.addEventListener('click', () => {
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
    container.appendChild(label);
    container.appendChild(textarea);
    buttonContainer.appendChild(executeBtn);
    container.appendChild(buttonContainer);

    return container;
}
