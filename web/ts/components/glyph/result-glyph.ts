/**
 * Result Glyph - Python execution output display
 *
 * Displays stdout, stderr, and execution results from Python code.
 * Appears below py glyphs as execution history.
 */

import type { Glyph } from './glyph';
import { log, SEG } from '../../logger';
import { uiState } from '../../state/ui';
import { canvasPlaced } from './manifestations/canvas-placed';
import { unmeldComposition } from './meld/meld-composition';
import { makeDraggable } from './glyph-interaction';

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
    // Calculate height based on content
    const lineCount = (result.stdout + result.stderr + (result.error || '')).split('\n').length;
    const minHeight = 80;
    const maxHeight = 400;
    const lineHeight = 18;
    const calculatedHeight = Math.min(maxHeight, Math.max(minHeight, lineCount * lineHeight + 60));

    // Build header first (used as custom drag handle)
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
        // TODO: Implement window manifestation morphing (tracked in #440)
        log.debug(SEG.GLYPH, '[ResultGlyph] To window clicked (not implemented)');
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
        // Check if result is in a composition
        const composition = element.closest('.melded-composition') as HTMLElement | null;
        if (composition) {
            // Unmeld composition first, then remove the result
            const unmelded = unmeldComposition(composition);
            if (unmelded) {
                // Restore drag handlers for the unmelded glyphs (excluding the result we're closing)
                for (const glyphElement of unmelded.glyphElements) {
                    const glyphId = glyphElement.getAttribute('data-glyph-id');
                    if (glyphId && glyphId !== glyph.id) {
                        const glyphObj: Glyph = {
                            id: glyphId,
                            title: glyphElement.getAttribute('data-glyph-symbol') || 'Glyph',
                            symbol: glyphElement.getAttribute('data-glyph-symbol') || undefined,
                            renderContent: () => glyphElement
                        };
                        makeDraggable(glyphElement, glyphElement, glyphObj, {
                            logLabel: 'RestoredGlyph'
                        });
                    }
                }
                log.debug(SEG.GLYPH, `[ResultGlyph] Unmelded composition before closing ${glyph.id}`);
            }
        }

        element.remove();
        uiState.removeCanvasGlyph(glyph.id);
        log.debug(SEG.GLYPH, `[ResultGlyph] Closed ${glyph.id}`);
    });

    buttonContainer.appendChild(closeBtn);
    header.appendChild(buttonContainer);

    const { element } = canvasPlaced({
        glyph,
        className: 'canvas-result-glyph',
        defaults: { x: 200, y: 200, width: 400, height: calculatedHeight },
        dragHandle: header,
        draggableOptions: { ignoreButtons: true },
        resizable: { minWidth: 200, minHeight: 80 },
        logLabel: 'ResultGlyph',
    });
    element.style.minHeight = '80px';
    element.style.borderRadius = '0 0 4px 4px';
    element.style.border = '1px solid var(--border-on-dark)';
    element.style.borderTop = 'none';
    element.style.zIndex = '1';
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
    (glyph as any).content = JSON.stringify(result);

    return element;
}

