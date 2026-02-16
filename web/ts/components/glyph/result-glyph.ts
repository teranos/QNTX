/**
 * Result Glyph - Execution output display
 *
 * Displays stdout, stderr, and execution results from glyph execution.
 * Appears below executable glyphs as output.
 */

import type { Glyph } from './glyph';
import { log, SEG } from '../../logger';
import { apiFetch } from '../../api';
import { uiState } from '../../state/ui';
import { canvasPlaced } from './manifestations/canvas-placed';
import { unmeldComposition } from './meld/meld-composition';
import { autoMeldResultBelow } from './meld/meld-system';
import { makeDraggable, preventDrag, storeCleanup } from './glyph-interaction';

/**
 * Glyph execution result data
 */
export interface ExecutionResult {
    success: boolean;
    stdout: string;
    stderr: string;
    result: unknown;
    error: string | null;
    duration_ms: number;
}

export interface PromptConfig {
    model?: string;
    temperature?: number;
    maxTokens?: number;
    provider?: string;
}

export interface ResultGlyphContent {
    result: ExecutionResult;
    promptConfig?: PromptConfig;
    prompt?: string;
}

/**
 * Create a result glyph showing execution output
 */
export function createResultGlyph(
    glyph: Glyph,
    result: ExecutionResult,
    promptConfig?: PromptConfig,
    prompt?: string
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
    header.style.backgroundColor = '#333433';
    header.style.borderBottom = '1px solid var(--border-color)';
    header.style.display = 'flex';
    header.style.alignItems = 'flex-start';
    header.style.justifyContent = 'space-between';
    header.style.fontSize = '11px';
    header.style.color = 'var(--text-secondary)';

    // Prompt label — full multiline prompt text
    if (prompt) {
        const promptLabel = document.createElement('span');
        promptLabel.className = 'result-prompt-label';
        promptLabel.style.flex = '1';
        promptLabel.style.whiteSpace = 'pre-wrap';
        promptLabel.style.wordBreak = 'break-word';
        promptLabel.style.padding = '0 8px';
        promptLabel.style.color = 'var(--text-on-dark)';
        promptLabel.style.fontSize = '12px';
        promptLabel.textContent = prompt;
        header.appendChild(promptLabel);
    }

    // Button container
    const buttonContainer = document.createElement('div');
    buttonContainer.style.display = 'flex';
    buttonContainer.style.gap = '3px';
    buttonContainer.style.flexShrink = '0';

    function headerBtn(label: string, title: string, fontSize = '11px'): HTMLButtonElement {
        const btn = document.createElement('button');
        btn.textContent = label;
        btn.title = title;
        btn.style.background = '#4a4b4a';
        btn.style.border = '1px solid #555';
        btn.style.borderRadius = '2px';
        btn.style.padding = '2px 5px';
        btn.style.cursor = 'pointer';
        btn.style.fontSize = fontSize;
        btn.style.lineHeight = '1';
        btn.style.color = '#ccc';
        btn.style.transition = 'background 0.15s, border-color 0.15s';
        btn.addEventListener('mouseenter', () => {
            btn.style.background = '#5e5f5e';
            btn.style.borderColor = '#6a6a6a';
        });
        btn.addEventListener('mouseleave', () => {
            btn.style.background = '#4a4b4a';
            btn.style.borderColor = '#555';
        });
        return btn;
    }

    // Copy button — copies prompt + result to clipboard
    const copyBtn = headerBtn('⎘', 'Copy to clipboard', '12px');

    copyBtn.addEventListener('click', () => {
        let text = '';
        if (prompt) text += `> ${prompt.replace(/\n/g, '\n> ')}\n\n`;
        text += result.stdout || result.error || '(no output)';
        navigator.clipboard.writeText(text).then(() => {
            copyBtn.textContent = '✓';
            setTimeout(() => { copyBtn.textContent = '⎘'; }, 1500);
        });
    });

    buttonContainer.appendChild(copyBtn);

    // To window button
    const toWindowBtn = headerBtn('⬆', 'Expand to window', '10px');

    toWindowBtn.addEventListener('click', () => {
        // TODO: Implement window manifestation morphing (tracked in #440)
        log.debug(SEG.GLYPH, '[ResultGlyph] To window clicked (not implemented)');
    });

    buttonContainer.appendChild(toWindowBtn);

    // Close button
    const closeBtn = headerBtn('×', 'Close result', '13px');

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
    element.style.borderRadius = '0 0 2px 2px';
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

    // Follow-up input zone — hidden until hover/focus
    const followupZone = document.createElement('div');
    followupZone.className = 'result-followup-zone';

    const followupInput = document.createElement('textarea');
    followupInput.placeholder = 'Follow up…';
    followupInput.rows = 1;
    preventDrag(followupInput);

    // Auto-resize textarea as content grows
    function autoResize() {
        followupInput.style.height = 'auto';
        followupInput.style.height = `${followupInput.scrollHeight}px`;
    }
    followupInput.addEventListener('input', autoResize);

    const followupStatus = document.createElement('span');
    followupStatus.className = 'followup-status';

    followupInput.addEventListener('keydown', (e) => {
        if (e.key === 'Enter' && !e.shiftKey) {
            e.preventDefault();
            const text = followupInput.value.trim();
            if (!text) return;

            followupInput.disabled = true;
            followupStatus.textContent = 'Running…';

            const body: Record<string, unknown> = {
                template: text,
                system_prompt: result.stdout,
                glyph_id: glyph.id,
            };
            if (promptConfig?.model) body.model = promptConfig.model;
            if (promptConfig?.provider) body.provider = promptConfig.provider;

            const startTime = Date.now();

            apiFetch('/api/prompt/direct', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify(body),
            })
                .then(async (response) => {
                    if (!response.ok) {
                        const errorText = await response.text();
                        throw new Error(`API error: ${response.status} - ${errorText}`);
                    }
                    return response.json();
                })
                .then((data: any) => {
                    const elapsedMs = Date.now() - startTime;
                    followupStatus.textContent = `${(elapsedMs / 1000).toFixed(2)}s`;
                    followupInput.value = '';
                    followupInput.style.height = 'auto';
                    followupInput.disabled = false;

                    const followupResult: ExecutionResult = {
                        success: !data.error,
                        stdout: data.response ?? '',
                        stderr: '',
                        result: null,
                        error: data.error ?? null,
                        duration_ms: elapsedMs,
                    };
                    spawnFollowUpResult(element, glyph, followupResult, promptConfig, text);
                })
                .catch((err) => {
                    const msg = err instanceof Error ? err.message : String(err);
                    followupStatus.textContent = `Failed: ${msg}`;
                    followupInput.disabled = false;
                    log.error(SEG.GLYPH, `[ResultGlyph] Follow-up failed for ${glyph.id}:`, err);
                });
        }
    });

    followupZone.appendChild(followupInput);
    followupZone.appendChild(followupStatus);
    element.appendChild(followupZone);

    // Register cleanup
    storeCleanup(element, () => {});

    // Ensure result data is attached to glyph object for drag persistence
    const contentPayload: ResultGlyphContent = { result, ...(promptConfig && { promptConfig }), ...(prompt && { prompt }) };
    (glyph as any).content = JSON.stringify(contentPayload);

    return element;
}

/**
 * Spawn a follow-up result glyph below a parent result glyph.
 * Mirrors prompt-glyph's spawnResultGlyph pattern.
 */
function spawnFollowUpResult(
    parentElement: HTMLElement,
    parentGlyph: Glyph,
    result: ExecutionResult,
    promptConfig?: PromptConfig,
    prompt?: string
): void {
    const parentRect = parentElement.getBoundingClientRect();
    const canvas = parentElement.closest('.canvas-workspace') as HTMLElement;
    if (!canvas) {
        log.error(SEG.GLYPH, '[ResultGlyph] Cannot spawn follow-up: no canvas-workspace ancestor');
        return;
    }
    const canvasRect = canvas.getBoundingClientRect();

    const rx = parentRect.left - canvasRect.left;
    const ry = parentRect.bottom - canvasRect.top;

    const resultGlyphId = `result-${crypto.randomUUID()}`;
    const resultGlyph: Glyph = {
        id: resultGlyphId,
        title: 'Follow-up Result',
        symbol: 'result',
        x: rx,
        y: ry,
        width: Math.round(parentRect.width),
        renderContent: () => document.createElement('div')
    };

    const resultElement = createResultGlyph(resultGlyph, result, promptConfig, prompt);
    canvas.appendChild(resultElement);

    const resultRect = resultElement.getBoundingClientRect();
    const contentPayload: ResultGlyphContent = { result, ...(promptConfig && { promptConfig }), ...(prompt && { prompt }) };
    uiState.addCanvasGlyph({
        id: resultGlyphId,
        symbol: 'result',
        x: rx,
        y: ry,
        width: Math.round(resultRect.width),
        height: Math.round(resultRect.height),
        content: JSON.stringify(contentPayload),
    });

    // Auto-meld below parent result
    const parentGlyphId = parentElement.dataset.glyphId;
    if (parentGlyphId) {
        autoMeldResultBelow(parentElement, parentGlyphId, 'result', 'Result', resultElement, resultGlyphId, 'ResultGlyph');
    }

    log.debug(SEG.GLYPH, `[ResultGlyph] Spawned follow-up ${resultGlyphId} below ${parentGlyph.id}`);
}

/**
 * Render execution result output into a container element.
 * Reused by createResultGlyph and updateResultGlyphContent.
 */
function renderOutput(container: HTMLElement, result: ExecutionResult): void {
    container.innerHTML = '';
    container.style.color = 'var(--text-on-dark)';
    container.style.fontStyle = '';

    let outputText = '';

    if (result.stdout) {
        outputText += result.stdout;
    }

    if (result.stderr) {
        const stderrSpan = document.createElement('span');
        stderrSpan.style.color = 'var(--glyph-status-error-text)';
        stderrSpan.textContent = result.stderr;
        container.appendChild(document.createTextNode(outputText));
        container.appendChild(stderrSpan);
        outputText = '';
    }

    if (result.error) {
        const errorSpan = document.createElement('span');
        errorSpan.style.color = 'var(--glyph-status-error-text)';
        errorSpan.style.fontWeight = 'bold';
        errorSpan.textContent = `\nError: ${result.error}`;
        container.appendChild(document.createTextNode(outputText));
        container.appendChild(errorSpan);
        outputText = '';
    }

    if (outputText) {
        container.appendChild(document.createTextNode(outputText));
    }

    if (!result.stdout && !result.stderr && !result.error) {
        container.textContent = '(no output)';
        container.style.color = 'var(--text-secondary)';
        container.style.fontStyle = 'italic';
    }
}

/**
 * Update an existing result glyph's content in place.
 * Returns true if the update succeeded, false if the element structure wasn't found.
 */
export function updateResultGlyphContent(resultElement: HTMLElement, result: ExecutionResult): boolean {
    const output = resultElement.querySelector('.result-glyph-output') as HTMLElement | null;
    if (!output) return false;

    renderOutput(output, result);

    // Update persisted content (preserve promptConfig and prompt if present)
    const glyphId = resultElement.getAttribute('data-glyph-id');
    if (glyphId) {
        const existing = uiState.getCanvasGlyphs().find(g => g.id === glyphId);
        if (existing) {
            let promptConfig: PromptConfig | undefined;
            try {
                const prev = JSON.parse(existing.content || '{}');
                promptConfig = prev.promptConfig;
            } catch { /* ignore */ }
            const contentPayload: ResultGlyphContent = { result, ...(promptConfig && { promptConfig }) };
            uiState.addCanvasGlyph({ ...existing, content: JSON.stringify(contentPayload) });
        }
    }

    return true;
}

