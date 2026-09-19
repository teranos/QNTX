/**
 * Prompt Element - LLM prompt template editor on canvas
 *
 * Simple prompt editor with:
 * - Template textarea with YAML frontmatter for model/temperature/max_tokens config
 * - Play button for one-shot execution (testing)
 * - Inline result display
 *
 * Future Vision:
 * - AX elements (separate) flow attestations to Prompt elements via watchers
 * - Watchers keep executing prompts as matching attestations arrive
 * - For now: simple one-shot execution for testing
 *
 * TODO: Migrate to ElementUI SDK (like py-element and ts-element).
 */

import type { Element } from '@teranos/elements';
import { SO, Doc, Prose } from '../../sym';
import { log, SEG } from '../../logger';
import { apiFetch } from '../../client';
import { jsonBody } from '../../http-utils';
import { preventDrag, storeCleanup } from '@teranos/elements';
import { canvasPlaced } from '@teranos/elements';
import { autoMeldResultBelow } from './meld/auto-meld-result';
import { uiState } from '../../state/ui';
import { createAutoSave } from './element-autosave';
import { tooltip } from '../tooltip';
import { findCompositionByElement, extractElementIds } from '../../state/compositions';
import { createResultElement, getResponseTokenCount, unsubscribeStream, populateStaticContent } from './result-element';

/**
 * Prompt element execution status
 */
interface PromptElementStatus {
    state: 'idle' | 'running' | 'success' | 'error';
    message?: string;
    timestamp?: number;
}

/**
 * Save prompt element status to localStorage
 */
function savePromptStatus(elementId: string, status: PromptElementStatus): void {
    const key = `prompt-status-${elementId}`;
    localStorage.setItem(key, JSON.stringify(status));
}

/**
 * Load prompt element status from localStorage
 */
function loadPromptStatus(elementId: string): PromptElementStatus | null {
    const key = `prompt-status-${elementId}`;
    const stored = localStorage.getItem(key);
    if (!stored) return null;

    try {
        return JSON.parse(stored);
    } catch (e) {
        log.error(SEG.ELEMENT, `[Prompt] Failed to parse stored status for ${elementId}:`, e);
        return null;
    }
}


export const PROMPT_DEFAULT_TEMPLATE = '---\nmodel: "anthropic/claude-haiku-4.5"\ntemperature: 0.7\nmax_tokens: 1000\n---\nWrite a haiku about quantum computing.\n';

/**
 * Create a prompt element with template editor on canvas
 */
export async function createPromptElement(item: Element): Promise<HTMLElement> {
    const element = document.createElement('div');
    await setupPromptElement(element, item);
    return element;
}

/**
 * Populate an element as a prompt element.
 * Can be called on a fresh element (createPromptElement) or an existing one (conversion).
 * Caller must runCleanup() and clear children before calling on an existing element.
 */
export async function setupPromptElement(element: HTMLElement, item: Element): Promise<void> {
    // Load saved template from canvas state
    const existingElement = uiState.getCanvasElement(item.id);
    const savedTemplate = existingElement?.content ?? PROMPT_DEFAULT_TEMPLATE;

    // Load saved status
    const savedStatus = loadPromptStatus(item.id) ?? { state: 'idle' };

    // Reset inline styles (important when repopulating after conversion)
    element.style.cssText = '';

    // Template textarea (declared early for play button reference)
    const textarea = document.createElement('textarea');
    textarea.placeholder = '---\nmodel: "anthropic/claude-haiku-4.5"\n---\nYour prompt here...';
    textarea.value = savedTemplate;
    textarea.style.flex = '1';
    textarea.style.padding = '8px';
    textarea.style.fontSize = '13px';
    textarea.style.fontFamily = 'var(--font-mono)';
    textarea.style.backgroundColor = 'var(--bg-almost-black)';
    textarea.style.color = 'var(--accent-lavender)';
    textarea.style.border = '1px solid var(--border)';
    textarea.style.borderRadius = '4px';
    textarea.style.resize = 'none';

    // Auto-save template with debouncing
    const { save, cancel: cancelAutoSave } = createAutoSave(item.id, () => textarea.value, 'Prompt Element');
    textarea.addEventListener('input', () => save());

    // Cmd/Ctrl+Enter to execute prompt
    textarea.addEventListener('keydown', (e) => {
        if (e.key === 'Enter' && (e.metaKey || e.ctrlKey)) {
            e.preventDefault();
            playBtn.click();
        }
    });

    preventDrag(textarea);

    // Status display
    const statusSection = document.createElement('div');
    statusSection.className = 'prompt-status-section';
    statusSection.style.display = 'none';
    statusSection.style.padding = '4px 8px';
    statusSection.style.fontSize = '11px';
    statusSection.style.fontFamily = 'var(--font-mono)';
    statusSection.style.whiteSpace = 'pre-wrap'; // Allow wrapping, preserve formatting
    statusSection.style.wordBreak = 'break-word'; // Break long words if needed
    statusSection.style.overflowWrap = 'anywhere'; // Allow breaking anywhere to prevent overflow
    statusSection.style.maxWidth = '100%';

    function updateStatus(status: PromptElementStatus): void {
        switch (status.state) {
            case 'running':
                element.style.backgroundColor = 'var(--element-status-running-bg)';
                break;
            case 'success':
                element.style.backgroundColor = 'var(--element-status-success-bg)';
                break;
            case 'error':
                element.style.backgroundColor = 'var(--element-status-error-bg)';
                break;
            default:
                element.style.backgroundColor = item.color ?? '';
        }

        if (status.state !== 'idle' && status.message) {
            statusSection.style.display = 'block';
            statusSection.textContent = status.message;

            switch (status.state) {
                case 'running':
                    statusSection.style.color = 'var(--element-status-running-text)';
                    statusSection.style.backgroundColor = 'var(--element-status-running-section-bg)';
                    break;
                case 'success':
                    statusSection.style.color = 'var(--element-status-success-text)';
                    statusSection.style.backgroundColor = 'var(--element-status-success-section-bg)';
                    break;
                case 'error':
                    statusSection.style.color = 'var(--element-status-error-text)';
                    statusSection.style.backgroundColor = 'var(--element-status-error-section-bg)';
                    break;
            }
        } else {
            statusSection.style.display = 'none';
        }

        savePromptStatus(item.id, status);
        log.debug(SEG.ELEMENT, `[Prompt Element] Updated status for ${item.id}:`, status);
    }

    // Apply saved status on load
    updateStatus(savedStatus);

    // Title bar elements
    const playBtn = document.createElement('button');
    playBtn.textContent = '▶';
    playBtn.className = 'titlebar-btn has-tooltip';
    playBtn.dataset.tooltip = 'Execute prompt';

    playBtn.addEventListener('click', async (e) => {
        e.stopPropagation();
        const template = textarea.value.trim();

        if (!template) {
            log.debug(SEG.ELEMENT, '[Prompt] No template provided');
            return;
        }

        // Detect if template has {{variables}}
        const hasVariables = /\{\{[^}]+\}\}/.test(template);

        if (hasVariables) {
            // Template needs attestation data - show message
            updateStatus({
                state: 'error',
                message: 'Template has {{variables}} - connect to AX element (coming soon)',
                timestamp: Date.now(),
            });
            return;
        }

        log.debug(SEG.ELEMENT, `[Prompt] Executing direct (no variables)`);

        const startTime = Date.now();

        updateStatus({
            state: 'running',
            message: 'Running...',
            timestamp: startTime,
        });

        try {
            // Collect attachments from melded elements
            // TODO [TS-4]: Extract shared collectMeldedAttachments(elementId) — identical
            // block in element-followup.ts. Part of the same execute→spawn pipeline as TS-5.
            const fileIds: string[] = [];
            const noteTexts: string[] = [];
            const comp = findCompositionByElement(item.id);
            if (comp) {
                const memberIds = extractElementIds(comp.edges);
                for (const mid of memberIds) {
                    if (mid === item.id) continue; // skip self
                    const g = uiState.getCanvasElement(mid);
                    if (!g?.content) continue;

                    if (g.symbol === Doc) {
                        try {
                            const meta = JSON.parse(g.content);
                            if (meta.fileId && meta.ext) {
                                fileIds.push(meta.fileId + meta.ext);
                            }
                        } catch (e) { log.warn(SEG.ELEMENT, `[Prompt] Failed to parse doc metadata for ${mid}:`, e); }
                    } else if (g.symbol === Prose) {
                        noteTexts.push(g.content);
                    }
                }
            }

            // Prepend melded note texts as context
            let finalTemplate = template;
            if (noteTexts.length > 0) {
                finalTemplate = noteTexts.join('\n\n') + '\n\n' + template;
            }

            // Spawn response element below prompt before fetch starts
            const responseElement = spawnResponseBelow(element, item.id, template);

            const response = await apiFetch('/api/prompt/direct', jsonBody('POST', {
                template: finalTemplate,
                element_id: item.id,
                ...(fileIds.length > 0 && { file_ids: fileIds }),
            }));

            if (!response.ok) {
                const errorText = await response.text();
                if (responseElement) {
                    unsubscribeStream(item.id);
                    responseElement.remove();
                }
                throw new Error(`API error: ${response.status} - ${errorText}`);
            }

            const data = await response.json() as any;
            const elapsedMs = Date.now() - startTime;
            const elapsedSeconds = (elapsedMs / 1000).toFixed(2);

            updateStatus({
                state: data.error ? 'error' : 'success',
                message: data.error ? 'Failed' : `${elapsedSeconds}s`,
                timestamp: Date.now(),
            });

            if (data.error) {
                if (responseElement) {
                    unsubscribeStream(item.id);
                    responseElement.remove();
                }
                return;
            }

            // If no tokens arrived (non-streaming provider), populate with response text
            const tokenCount = responseElement ? getResponseTokenCount(responseElement) : 0;
            if (tokenCount === 0 && data.response && responseElement) {
                populateStaticContent(responseElement, data.response);
            }
            log.debug(SEG.ELEMENT, `[Prompt] Response complete, ${tokenCount} tokens`);

        } catch (error) {
            log.error(SEG.ELEMENT, '[Prompt] Execution failed:', error);
            const errorMsg = error instanceof Error ? error.message : String(error);
            updateStatus({
                state: 'error',
                message: `Failed: ${errorMsg}`,
                timestamp: Date.now(),
            });
        }
    });

    canvasPlaced({
        element,
        item: item,
        className: 'canvas-prompt-element',
        defaults: { x: 200, y: 200, width: 420, height: 340 },
        titleBar: { label: `${SO} Prompt`, actions: [playBtn] },
        resizable: { minWidth: 280, minHeight: 200 },
        logLabel: 'PromptElement',
    });

    // Style the label span created by canvasPlaced
    const labelSpan = element.querySelector('.title-bar > span:first-child') as HTMLElement;
    if (labelSpan) {
        labelSpan.style.fontSize = '16px';
        labelSpan.style.color = 'var(--accent-lavender)';
        labelSpan.style.fontWeight = 'bold';
        labelSpan.style.flex = '1';
    }

    // Content area
    const content = document.createElement('div');
    content.style.flex = '1';
    content.style.padding = '8px';
    content.style.display = 'flex';
    content.style.flexDirection = 'column';
    content.style.overflow = 'hidden';

    content.appendChild(textarea);
    content.appendChild(statusSection);

    element.appendChild(content);

    // Save initial template if new element
    if (!existingElement?.content) {
        const canvasElement = uiState.getCanvasElement(item.id);
        if (canvasElement) {
            uiState.addCanvasElement({ ...canvasElement, content: PROMPT_DEFAULT_TEMPLATE });
        }
    }

    // Register cleanup for conversions (drag/resize handled by canvasPlaced)
    storeCleanup(element, () => {
        cancelAutoSave();
    });

    // Attach tooltip support
    tooltip.attach(element);

    /**
     * Spawn a result element below this prompt element in streaming mode.
     * Returns the element, or null if canvas not found.
     *
     * TODO [TS-5]: Extract shared spawnResultBelow — this pattern is repeated
     * in element-followup.ts and canvas-workspace-builder.ts.
     */
    function spawnResponseBelow(promptEl: HTMLElement, promptElementId: string, promptText?: string): HTMLElement | null {
        const promptRect = promptEl.getBoundingClientRect();
        const canvas = promptEl.closest('.canvas-workspace') as HTMLElement;
        if (!canvas) {
            log.error(SEG.ELEMENT, '[Prompt] Cannot spawn response element: no canvas-workspace ancestor');
            return null;
        }
        const canvasRect = canvas.getBoundingClientRect();

        const rx = promptRect.left - canvasRect.left;
        const ry = promptRect.bottom - canvasRect.top;

        const responseElementId = `result-${crypto.randomUUID()}`;
        const responseItem: Element = {
            id: responseElementId,
            title: 'Result',
            symbol: 'result',
            x: rx,
            y: ry,
            width: Math.round(promptRect.width),
            renderContent: () => document.createElement('div'),
        };

        const responseElement = createResultElement(responseItem, undefined, undefined, promptText, promptElementId);
        canvas.appendChild(responseElement);

        uiState.addCanvasElement({
            id: responseElementId,
            symbol: 'result',
            x: rx,
            y: ry,
            width: Math.round(promptRect.width),
            height: 200,
        });

        autoMeldResultBelow(promptEl, promptElementId, 'prompt', 'Prompt', responseElement, responseElementId, 'ResultElement');

        return responseElement;
    }
}
