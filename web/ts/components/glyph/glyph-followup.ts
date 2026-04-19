/**
 * Shared follow-up input zone for glyphs that accept conversational follow-ups.
 *
 * Used by result-glyph and stream-glyph. Builds the DOM, handles input,
 * collects attachments, and either runs the default API flow (result glyph)
 * or delegates to a custom onExecute (stream glyph).
 */

import type { Glyph } from '@qntx/glyphs';
import { log, SEG } from '../../logger';
import { apiFetch } from '../../api';
import { canvasSyncQueue } from '../../api/canvas-sync';
import { uiState } from '../../state/ui';
import { Doc, Prose } from '@generated/sym.js';
import { preventDrag } from '@qntx/glyphs';
import { autoMeldResultBelow } from './meld/meld-system';
import { findCompositionByGlyph, extractGlyphIds } from '../../state/compositions';
import { createResultGlyph, type ExecutionResult } from './result-glyph';

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
    /** System prompt from the parent glyph */
    systemPrompt: string;
    /** Resolved model name */
    model?: string;
    /** Resolved provider name */
    provider?: string;
    /** Attached file IDs from melded doc glyphs */
    fileIds: string[];
    /** Parent glyph ID */
    glyphId: string;
    /** Composition edges from local state — sent to backend for conversation assembly */
    compositionEdges?: Array<{ from: string; to: string; direction: string; position: number }>;
}

export interface FollowUpConfig {
    /** The parent glyph DOM element */
    element: HTMLElement;
    /** Glyph metadata */
    glyph: Glyph;
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
     * (e.g., spawning a stream glyph before the API call fires).
     */
    onExecute?: (request: FollowUpRequest, controls: FollowUpControls) => void;
}

export interface FollowUpResult {
    parentElement: HTMLElement;
    parentGlyph: Glyph;
    result: ExecutionResult;
    model?: string;
    provider?: string;
    prompt: string;
    logLabel: string;
}

/**
 * Create and return a follow-up input zone element.
 * Append it to the parent glyph element.
 */
export function createFollowUpZone(config: FollowUpConfig): HTMLElement {
    const { element, glyph, getSystemPrompt, logLabel } = config;

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
            element.classList.remove('glyph-error');
        },
        error(message: string) {
            followupStatus.textContent = message;
            isExecuting = false;
            followupInput.disabled = false;
            followupZone.classList.add('has-error');
            element.classList.add('glyph-error');
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

            // Collect attachments from melded glyphs
            // TODO [TS-4]: Extract shared collectMeldedAttachments(glyphId) — identical
            // block in prompt-glyph.ts. Part of the same execute→spawn pipeline as TS-5.
            const fileIds: string[] = [];
            const noteTexts: string[] = [];
            const comp = findCompositionByGlyph(glyph.id);
            if (comp) {
                const memberIds = extractGlyphIds(comp.edges);
                for (const mid of memberIds) {
                    if (mid === glyph.id) continue;
                    const g = uiState.getCanvasGlyphs().find(cg => cg.id === mid);
                    if (!g?.content) continue;

                    if (g.symbol === Doc) {
                        try {
                            const meta = JSON.parse(g.content);
                            if (meta.fileId && meta.ext) {
                                fileIds.push(meta.fileId + meta.ext);
                            }
                        } catch { /* skip malformed */ }
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
                glyphId: glyph.id,
                compositionEdges: comp?.edges,
            };

            if (config.onExecute) {
                config.onExecute(request, controls);
                return;
            }

            // Default: fire API call and spawn result glyph
            defaultExecute(request, controls, element, glyph, logLabel);
        }
    });

    followupZone.appendChild(followupInput);
    followupZone.appendChild(followupStatus);
    return followupZone;
}

/**
 * Default execution: POST to /api/prompt/direct, spawn result glyph below.
 *
 * TODO: Use the streaming path (like executeStreamFollowUp in result-glyph.ts)
 * so follow-ups from py/static results also receive token signals and sampler colors.
 * Currently wraps the full response as ExecutionResult.stdout — no streaming, no signals.
 */
async function defaultExecute(
    request: FollowUpRequest,
    controls: FollowUpControls,
    element: HTMLElement,
    glyph: Glyph,
    logLabel: string,
): Promise<void> {
    // Ensure composition edges are persisted before the API call.
    // The backend's ConversationAssembler needs the composition in the DB
    // to trace meld edges and build multi-turn conversation history.
    await canvasSyncQueue.flush();

    const body: Record<string, unknown> = {
        template: request.template,
        system_prompt: request.systemPrompt,
        glyph_id: request.glyphId,
    };
    if (request.model) body.model = request.model;
    if (request.provider) body.provider = request.provider;
    if (request.fileIds.length > 0) body.file_ids = request.fileIds;

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
                parentGlyph: glyph,
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
            log.error(SEG.GLYPH, `[${logLabel}] Follow-up failed for ${glyph.id}${attachmentInfo}: ${errMsg}`);
        });
}

/**
 * Spawn a result glyph below the parent.
 *
 * TODO [TS-5]: Extract shared spawnResultBelow — this pattern is repeated
 * in prompt-glyph.ts and canvas-workspace-builder.ts.
 */
export function spawnFollowUpResult(data: FollowUpResult): void {
    const { parentElement, parentGlyph, result, model, provider, prompt, logLabel } = data;

    const parentRect = parentElement.getBoundingClientRect();
    const canvas = parentElement.closest('.canvas-workspace') as HTMLElement;
    if (!canvas) {
        log.error(SEG.GLYPH, `[${logLabel}] Cannot spawn follow-up: no canvas-workspace ancestor for ${parentGlyph.id}`);
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
        renderContent: () => document.createElement('div'),
    };

    const promptConfig = { model, provider };
    const resultElement = createResultGlyph(resultGlyph, result, promptConfig, prompt);
    canvas.appendChild(resultElement);

    const parentGlyphId = parentElement.dataset.glyphId;
    if (parentGlyphId) {
        autoMeldResultBelow(
            parentElement, parentGlyphId, parentElement.dataset.glyphSymbol ?? 'glyph',
            logLabel, resultElement, resultGlyphId, `${logLabel}FollowUp`,
        );
    }

    uiState.addCanvasGlyph({
        id: resultGlyphId,
        symbol: 'result',
        x: rx,
        y: ry,
        width: Math.round(parentRect.width),
        height: Math.round(resultElement.getBoundingClientRect().height) || 200,
        content: JSON.stringify({ result, promptConfig, prompt }),
    });

    log.debug(SEG.GLYPH, `[${logLabel}] Spawned follow-up ${resultGlyphId} below ${parentGlyph.id}`);
}
