/**
 * Result Glyph - Python execution output display
 *
 * Displays stdout, stderr, and execution results from Python code.
 * Appears below py glyphs as execution history.
 */

import type { Glyph } from './glyph';
import { log, SEG } from '../../logger';
import { uiState } from '../../state/ui';
import { GRID_SIZE } from './grid-constants';
import { makeDraggable } from './glyph-interaction';

/**
 * Python execution result data
 */
export interface ExecutionResult {
    success: boolean;
    stdout: string;
    stderr: string;
    result: any;
    error: string | null;
    duration_ms: number;
}

/**
 * Create a result glyph showing Python execution output
 */
export function createResultGlyph(
    glyph: Glyph,
    result: ExecutionResult
): HTMLElement {
    const element = document.createElement('div');
    element.className = 'canvas-result-glyph';
    element.dataset.glyphId = glyph.id;
    if (glyph.symbol) {
        element.dataset.glyphSymbol = glyph.symbol;
    }

    const x = glyph.x ?? 200;
    const y = glyph.y ?? 200;
    const width = glyph.width ?? 400;

    // Calculate height based on content
    const lineCount = (result.stdout + result.stderr + (result.error || '')).split('\n').length;
    const minHeight = 80;
    const maxHeight = 400;
    const lineHeight = 18;
    const calculatedHeight = Math.min(maxHeight, Math.max(minHeight, lineCount * lineHeight + 60));
    const height = glyph.height ?? calculatedHeight;

    // Style - integrated look with py glyph
    element.style.position = 'absolute';
    element.style.left = `${x}px`;
    element.style.top = `${y}px`;
    element.style.width = `${width}px`;
    element.style.height = `${height}px`;
    element.style.minHeight = '80px';
    element.style.backgroundColor = '#1e1e1e'; // Solid dark background
    element.style.borderRadius = '0 0 4px 4px'; // Rounded bottom only
    element.style.border = '1px solid #3e3e3e';
    element.style.borderTop = 'none'; // Visually connects to py glyph above
    element.style.display = 'flex';
    element.style.flexDirection = 'column';
    element.style.overflow = 'hidden';
    element.style.zIndex = '1';

    // Header with buttons
    const header = document.createElement('div');
    header.className = 'result-glyph-header';
    header.style.padding = '4px 8px';
    header.style.backgroundColor = 'var(--bg-tertiary)';
    header.style.borderBottom = '1px solid var(--border-color)';
    header.style.display = 'flex';
    header.style.alignItems = 'center';
    header.style.justifyContent = 'space-between';
    header.style.fontSize = '11px';
    header.style.color = 'var(--text-secondary)';

    // Duration label
    const durationLabel = document.createElement('span');
    durationLabel.textContent = `${result.duration_ms}ms`;
    header.appendChild(durationLabel);

    // Button container
    const buttonContainer = document.createElement('div');
    buttonContainer.style.display = 'flex';
    buttonContainer.style.gap = '4px';

    // To window button
    const toWindowBtn = document.createElement('button');
    toWindowBtn.textContent = '⬆';
    toWindowBtn.title = 'Expand to window';
    toWindowBtn.style.background = 'var(--bg-hover)';
    toWindowBtn.style.border = '1px solid var(--border-color)';
    toWindowBtn.style.borderRadius = '3px';
    toWindowBtn.style.padding = '2px 6px';
    toWindowBtn.style.cursor = 'pointer';
    toWindowBtn.style.fontSize = '10px';
    toWindowBtn.style.color = 'var(--text-primary)';

    toWindowBtn.addEventListener('click', () => {
        // TODO: Implement window manifestation morphing
        log.debug(SEG.UI, '[ResultGlyph] To window clicked (not implemented)');
    });

    buttonContainer.appendChild(toWindowBtn);

    // Close button
    const closeBtn = document.createElement('button');
    closeBtn.textContent = '×';
    closeBtn.title = 'Close result';
    closeBtn.style.background = 'var(--bg-hover)';
    closeBtn.style.border = '1px solid var(--border-color)';
    closeBtn.style.borderRadius = '3px';
    closeBtn.style.padding = '2px 6px';
    closeBtn.style.cursor = 'pointer';
    closeBtn.style.fontSize = '14px';
    closeBtn.style.lineHeight = '1';
    closeBtn.style.color = 'var(--text-primary)';

    closeBtn.addEventListener('click', () => {
        element.remove();
        uiState.removeCanvasGlyph(glyph.id);
        log.debug(SEG.UI, `[ResultGlyph] Closed ${glyph.id}`);
    });

    buttonContainer.appendChild(closeBtn);
    header.appendChild(buttonContainer);
    element.appendChild(header);

    // Output container
    const outputContainer = document.createElement('div');
    outputContainer.className = 'result-glyph-output';
    outputContainer.style.flex = '1';
    outputContainer.style.overflow = 'auto';
    outputContainer.style.padding = '8px';
    outputContainer.style.fontFamily = 'monospace';
    outputContainer.style.fontSize = '12px';
    outputContainer.style.whiteSpace = 'pre-wrap';
    outputContainer.style.wordBreak = 'break-word';
    outputContainer.style.color = '#e0e0e0'; // Light text for dark background

    // Build output text
    let outputText = '';

    if (result.stdout) {
        outputText += result.stdout;
    }

    if (result.stderr) {
        const stderrSpan = document.createElement('span');
        stderrSpan.style.color = 'var(--error-color, #ff6b6b)';
        stderrSpan.textContent = result.stderr;
        outputContainer.appendChild(document.createTextNode(outputText));
        outputContainer.appendChild(stderrSpan);
        outputText = '';
    }

    if (result.error) {
        const errorSpan = document.createElement('span');
        errorSpan.style.color = 'var(--error-color, #ff6b6b)';
        errorSpan.style.fontWeight = 'bold';
        errorSpan.textContent = `\nError: ${result.error}`;
        outputContainer.appendChild(document.createTextNode(outputText));
        outputContainer.appendChild(errorSpan);
        outputText = '';
    }

    if (outputText) {
        outputContainer.appendChild(document.createTextNode(outputText));
    }

    // If no output, show placeholder
    if (!result.stdout && !result.stderr && !result.error) {
        outputContainer.textContent = '(no output)';
        outputContainer.style.color = 'var(--text-secondary)';
        outputContainer.style.fontStyle = 'italic';
    }

    element.appendChild(outputContainer);

    // Make draggable by header
    makeDraggable(element, header, glyph, { ignoreButtons: true, logLabel: 'ResultGlyph' });

    return element;
}

