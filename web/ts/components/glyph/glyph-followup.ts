/**
 * Shared follow-up input zone for glyphs that accept conversational follow-ups.
 *
 * Used by result-glyph and stream-glyph. Builds the DOM, handles the API call,
 * and spawns a result glyph below with the response.
 */

import type { Glyph } from './glyph';
import { log, SEG } from '../../logger';
import { apiFetch } from '../../api';
import { uiState } from '../../state/ui';
import { Doc, Prose } from '@generated/sym.js';
import { preventDrag } from './glyph-interaction';
import { autoMeldResultBelow } from './meld/meld-system';
import { findCompositionByGlyph, extractGlyphIds } from '../../state/compositions';
import { createResultGlyph, type ExecutionResult } from './result-glyph';

export interface FollowUpConfig {
    /** The parent glyph DOM element */
    element: HTMLElement;
    /** Glyph metadata */
    glyph: Glyph;
    /** Returns the text to use as system_prompt for the follow-up */
    getSystemPrompt: () => string;
    /** Model name (if known) */
    model?: string;
    /** Provider name (if known) */
    provider?: string;
    /** Log label for debug messages */
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

            let finalTemplate = text;
            if (noteTexts.length > 0) {
                finalTemplate = noteTexts.join('\n\n') + '\n\n' + text;
            }

            const body: Record<string, unknown> = {
                template: finalTemplate,
                system_prompt: getSystemPrompt(),
                glyph_id: glyph.id,
            };
            if (config.model) body.model = config.model;
            if (config.provider) body.provider = config.provider;
            if (fileIds.length > 0) body.file_ids = fileIds;

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
                    isExecuting = false;
                    followupInput.value = '';
                    followupInput.style.height = 'auto';
                    followupInput.disabled = false;
                    followupZone.classList.remove('has-error');
                    element.classList.remove('glyph-error');

                    const followupResult: ExecutionResult = {
                        success: !data.error,
                        stdout: data.response ?? '',
                        stderr: '',
                        result: null,
                        error: data.error ?? null,
                        duration_ms: elapsedMs,
                    };
                    spawnFollowUpResult(
                        element, glyph, followupResult,
                        { model: config.model, provider: config.provider },
                        text,
                        logLabel,
                    );
                })
                .catch((err) => {
                    isExecuting = false;
                    const errMsg = err instanceof Error ? err.message : String(err);
                    const attachmentInfo = fileIds.length > 0 ? ` (${fileIds.length} file${fileIds.length > 1 ? 's' : ''} attached)` : '';
                    followupStatus.textContent = `Failed${attachmentInfo}: ${errMsg}`;
                    followupInput.disabled = false;
                    followupZone.classList.add('has-error');
                    element.classList.add('glyph-error');
                    log.error(SEG.GLYPH, `[${logLabel}] Follow-up failed for ${glyph.id}${attachmentInfo}: ${errMsg}`);
                });
        }
    });

    followupZone.appendChild(followupInput);
    followupZone.appendChild(followupStatus);
    return followupZone;
}

/**
 * Spawn a follow-up result glyph below a parent glyph element.
 */
function spawnFollowUpResult(
    parentElement: HTMLElement,
    parentGlyph: Glyph,
    result: ExecutionResult,
    promptConfig: { model?: string; provider?: string },
    prompt: string,
    logLabel: string,
): void {
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
