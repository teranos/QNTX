/**
 * Shared follow-up input zone for elements that accept conversational follow-ups.
 *
 * Used by result-element and stream-element. Builds the DOM, handles input,
 * collects attachments, and either runs the default API flow (result element)
 * or delegates to a custom onExecute (stream element).
 */

import type { Element } from '@teranos/elements';
import { log, SEG } from '../../logger';
import { apiFetch } from '../../client';
import { assertOk, jsonBody } from '../../http-utils';
import { canvasSyncQueue } from '../../api/canvas-sync';
import { uiState } from '../../state/ui';
import { Doc, Prose } from '../../sym';
import { preventDrag } from '@teranos/elements';
import { setResponseState } from './response-state';
import { autoMeldResultBelow } from './meld/auto-meld-result';
import { findCompositionByElement, extractElementIds } from '../../state/compositions';
import { createResultElement, type ExecutionResult } from './result-element';

/** UI controls exposed to custom onExecute callbacks */
export interface FollowUpControls {
    /** Show success state: timing, re-enable input */
    success: (elapsedMs: number) => void;
    /** Show error state */
    error: (message: string) => void;
}

export interface FollowUpRequest {
    /** The user's follow-up text */
    text: string;
    /** Full template with melded note texts prepended */
    template: string;
    /** System prompt from the parent element */
    systemPrompt: string;
    /** Resolved model name */
    model?: string;
    /** Resolved provider name */
    provider?: string;
    /** Attached file IDs from melded doc elements */
    fileIds: string[];
    /** Parent element ID */
    itemId: string;
    /** Composition edges from local state — sent to backend for conversation assembly */
    compositionEdges?: Array<{ from: string; to: string; direction: string; position: number }>;
}

export interface FollowUpConfig {
    /** The parent element DOM element */
    element: HTMLElement;
    /** Element metadata */
    item: Element;
    /** Returns the text to use as system_prompt for the follow-up */
    getSystemPrompt: () => string;
    /** Returns model name at call time (may be set after construction) */
    getModel?: () => string | undefined;
    /** Returns provider name at call time (may be set after construction) */
    getProvider?: () => string | undefined;
    /** Log label for debug messages */
    logLabel: string;
    /**
     * Custom execution handler. If provided, completely replaces the default
     * API call + result spawning. The callback receives the prepared request
     * and UI controls for managing input state.
     *
     * Use this when the caller needs to own the request lifecycle
     * (e.g., spawning a stream element before the API call fires).
     */
    onExecute?: (request: FollowUpRequest, controls: FollowUpControls) => void;
}

export interface FollowUpResult {
    parentElement: HTMLElement;
    parentItem: Element;
    result: ExecutionResult;
    model?: string;
    provider?: string;
    prompt: string;
    logLabel: string;
}

/**
 * Create and return a follow-up input zone element.
 * Append it to the parent element element.
 */
export function createFollowUpZone(config: FollowUpConfig): HTMLElement {
    const { element, item: item, getSystemPrompt, logLabel } = config;

    const followupZone = document.createElement('div');
    followupZone.className = 'result-followup-zone';

    const followupInput = document.createElement('textarea');
    followupInput.placeholder = 'Follow up…';
    followupInput.rows = 1;
    preventDrag(followupInput);

    function autoResize() {
        followupInput.style.height = 'auto';
        followupInput.style.height = `${followupInput.scrollHeight}px`;
    }
    followupInput.addEventListener('input', autoResize);

    const followupStatus = document.createElement('span');
    followupStatus.className = 'followup-status';

    // UI controls shared with custom onExecute
    const controls: FollowUpControls = {
        success(elapsedMs: number) {
            followupStatus.textContent = `${(elapsedMs / 1000).toFixed(2)}s`;
            isExecuting = false;
            followupInput.value = '';
            followupInput.style.height = 'auto';
            followupInput.disabled = false;
            followupZone.classList.remove('has-error');
            setResponseState(element, null);
        },
        error(message: string) {
            followupStatus.textContent = message;
            isExecuting = false;
            followupInput.disabled = false;
            followupZone.classList.add('has-error');
            setResponseState(element, 'error');
        },
    };

    let isExecuting = false;
    followupInput.addEventListener('keydown', (e) => {
        if (e.key === 'Enter' && !e.shiftKey) {
            e.preventDefault();
            const text = followupInput.value.trim();
            if (!text || isExecuting) return;

            isExecuting = true;
            followupInput.disabled = true;
            followupStatus.textContent = 'Running…';

            // Collect attachments from melded elements
            // TODO [TS-4]: Extract shared collectMeldedAttachments(elementId) — identical
            // block in prompt-element.ts. Part of the same execute→spawn pipeline as TS-5.
            const fileIds: string[] = [];
            const noteTexts: string[] = [];
            const comp = findCompositionByElement(item.id);
            if (comp) {
                const memberIds = extractElementIds(comp.edges);
                for (const mid of memberIds) {
                    if (mid === item.id) continue;
                    const g = uiState.getCanvasElement(mid);
                    if (!g?.content) continue;

                    if (g.symbol === Doc) {
                        try {
                            const meta = JSON.parse(g.content);
                            if (meta.fileId && meta.ext) {
                                fileIds.push(meta.fileId + meta.ext);
                            }
                        } catch (err) {
                            // A doc dropped here silently leaves the prompt
                            // missing context nobody chose to omit.
                            log.warn(SEG.ELEMENT, `Malformed doc element ${g.id} left out of follow-up context:`, err);
                        }
                    } else if (g.symbol === Prose) {
                        noteTexts.push(g.content);
                    }
                }
            }

            let template = text;
            if (noteTexts.length > 0) {
                template = noteTexts.join('\n\n') + '\n\n' + text;
            }

            const model = config.getModel?.();
            const provider = config.getProvider?.();

            const request: FollowUpRequest = {
                text,
                template,
                systemPrompt: getSystemPrompt(),
                model,
                provider,
                fileIds,
                itemId: item.id,
                compositionEdges: comp?.edges,
            };

            if (config.onExecute) {
                config.onExecute(request, controls);
                return;
            }

            // Default: fire API call and spawn result element
            defaultExecute(request, controls, element, item, logLabel).catch((err: unknown) => log.error(SEG.ELEMENT, `[${logLabel}] execute failed:`, err));
        }
    });

    followupZone.appendChild(followupInput);
    followupZone.appendChild(followupStatus);
    return followupZone;
}

/**
 * Default execution: POST to /api/prompt/direct, spawn result element below.
 *
 * TODO: Use the streaming path (like executeStreamFollowUp in result-element.ts)
 * so follow-ups from py/static results also receive token signals and sampler colors.
 * Currently wraps the full response as ExecutionResult.stdout — no streaming, no signals.
 */
async function defaultExecute(
    request: FollowUpRequest,
    controls: FollowUpControls,
    element: HTMLElement,
    item: Element,
    logLabel: string,
): Promise<void> {
    // Ensure composition edges are persisted before the API call.
    // The backend's ConversationAssembler needs the composition in the DB
    // to trace meld edges and build multi-turn conversation history.
    await canvasSyncQueue.flush();

    const body: Record<string, unknown> = {
        template: request.template,
        system_prompt: request.systemPrompt,
        element_id: request.itemId,
    };
    if (request.model) body.model = request.model;
    if (request.provider) body.provider = request.provider;
    if (request.fileIds.length > 0) body.file_ids = request.fileIds;

    const startTime = Date.now();

    apiFetch('/api/prompt/direct', jsonBody('POST', body))
        .then(async (response) => {
            await assertOk(response, 'Prompt API error');
            return response.json();
        })
        .then((data: any) => {
            const elapsedMs = Date.now() - startTime;
            controls.success(elapsedMs);

            const followupResult: ExecutionResult = {
                success: !data.error,
                stdout: data.response ?? '',
                stderr: '',
                result: null,
                error: data.error ?? null,
                duration_ms: elapsedMs,
            };

            spawnFollowUpResult({
                parentElement: element,
                parentItem: item,
                result: followupResult,
                model: request.model,
                provider: request.provider,
                prompt: request.text,
                logLabel,
            });
        })
        .catch((err) => {
            const errMsg = err instanceof Error ? err.message : String(err);
            const attachmentInfo = request.fileIds.length > 0
                ? ` (${request.fileIds.length} file${request.fileIds.length > 1 ? 's' : ''} attached)` : '';
            controls.error(`Failed${attachmentInfo}: ${errMsg}`);
            log.error(SEG.ELEMENT, `[${logLabel}] Follow-up failed for ${item.id}${attachmentInfo}: ${errMsg}`);
        });
}

/**
 * Spawn a result element below the parent.
 *
 * TODO [TS-5]: Extract shared spawnResultBelow — this pattern is repeated
 * in prompt-element.ts and canvas-workspace-builder.ts.
 */
export function spawnFollowUpResult(data: FollowUpResult): void {
    const { parentElement, parentItem, result, model, provider, prompt, logLabel } = data;

    const parentRect = parentElement.getBoundingClientRect();
    const canvas = parentElement.closest('.canvas-workspace') as HTMLElement;
    if (!canvas) {
        log.error(SEG.ELEMENT, `[${logLabel}] Cannot spawn follow-up: no canvas-workspace ancestor for ${parentItem.id}`);
        return;
    }
    const canvasRect = canvas.getBoundingClientRect();

    const rx = parentRect.left - canvasRect.left;
    const ry = parentRect.bottom - canvasRect.top;

    const resultElementId = `result-${crypto.randomUUID()}`;
    const resultItem: Element = {
        id: resultElementId,
        title: 'Follow-up Result',
        symbol: 'result',
        x: rx,
        y: ry,
        width: Math.round(parentRect.width),
        renderContent: () => document.createElement('div'),
    };

    const promptConfig = { model, provider };
    const resultElement = createResultElement(resultItem, result, promptConfig, prompt);
    canvas.appendChild(resultElement);

    const parentElementId = parentElement.dataset.elementId;
    if (parentElementId) {
        autoMeldResultBelow(
            parentElement, parentElementId, parentElement.dataset.symbol ?? 'element',
            logLabel, resultElement, resultElementId, `${logLabel}FollowUp`,
        );
    }

    uiState.addCanvasElement({
        id: resultElementId,
        symbol: 'result',
        x: rx,
        y: ry,
        width: Math.round(parentRect.width),
        height: Math.round(resultElement.getBoundingClientRect().height) || 200,
        content: JSON.stringify({ result, promptConfig, prompt }),
    });

    log.debug(SEG.ELEMENT, `[${logLabel}] Spawned follow-up ${resultElementId} below ${parentItem.id}`);
}
