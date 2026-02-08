/**
 * Result Glyph - Python execution output display
 *
 * Displays stdout, stderr, and execution results from Python code.
 * Appears below py glyphs as execution history.
 */

import type { Glyph } from './glyph';
import { log, SEG } from '../../logger';
import { uiState } from '../../state/ui';
import { applyCanvasGlyphLayout, makeDraggable, storeCleanup } from './glyph-interaction';
import { morphCanvasGlyphToWindow } from './manifestations/morph';

/**
 * Python execution result data
 */
export interface ExecutionResult {
    success: boolean;
    stdout: string;
    stderr: string;
    result: unknown;
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
    element.className = 'canvas-result-glyph canvas-glyph';
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
    applyCanvasGlyphLayout(element, { x, y, width, height });
    element.style.minHeight = '80px';
    // No background on parent - let header and content provide backgrounds
    element.style.borderRadius = '0 0 4px 4px';
    element.style.border = '1px solid var(--border-on-dark)';
    element.style.borderTop = 'none';
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
        // Guard: already in window manifestation
        if (element.dataset.manifestation === 'window') return;

        const container = element.parentElement;
        if (!container) return;

        morphCanvasGlyphToWindow(element, glyph, container, {
            title: `Result · ${result.duration_ms}ms`,
            width: 600,
            height: 400,
            onClose: () => {
                uiState.removeCanvasGlyph(glyph.id);
                log.debug(SEG.GLYPH, `[ResultGlyph] Closed window for ${glyph.id}`);
            },
            onRestore: (el) => {
                // Re-setup canvas drag handler on the header
                const header = el.querySelector('.result-glyph-header') as HTMLElement;
                if (header) {
                    const cleanup = makeDraggable(el, header, glyph, {
                        ignoreButtons: true,
                        logLabel: 'ResultGlyph'
                    });
                    storeCleanup(el, cleanup);
                }
            }
        });
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
        log.debug(SEG.GLYPH, `[ResultGlyph] Closed ${glyph.id}`);
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
    outputContainer.style.backgroundColor = 'rgba(10, 10, 10, 0.85)'; // 15% transparency
    outputContainer.style.color = 'var(--text-on-dark)';

    // Build output text
    let outputText = '';

    if (result.stdout) {
        outputText += result.stdout;
    }

    if (result.stderr) {
        const stderrSpan = document.createElement('span');
        stderrSpan.style.color = 'var(--glyph-status-error-text)';
        stderrSpan.textContent = result.stderr;
        outputContainer.appendChild(document.createTextNode(outputText));
        outputContainer.appendChild(stderrSpan);
        outputText = '';
    }

    if (result.error) {
        const errorSpan = document.createElement('span');
        errorSpan.style.color = 'var(--glyph-status-error-text)';
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

    // Ensure result data is attached to glyph object for drag persistence
    (glyph as any).result = result;

    // Make draggable by header
    const cleanupDrag = makeDraggable(element, header, glyph, { ignoreButtons: true, logLabel: 'ResultGlyph' });

    // Register cleanup for conversions
    storeCleanup(element, cleanupDrag);

    return element;
}

