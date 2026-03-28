/**
 * Stream Glyph — live inference heatmap
 *
 * Renders LLM tokens in real-time via WebSocket llm_stream messages.
 * Each token is a <span> colored by confidence: high confidence blends
 * with background, low confidence glows warm (amber/orange).
 *
 * Multiplexer pattern: one WebSocket handler routes messages to many
 * stream glyph instances by job_id.
 *
 * Persists token data to canvas state so content survives page refresh.
 *
 * Token hover popup showing signal data and top-K candidates — see token-popup.ts
 * TODO(CPY): Copy button (result glyph has one, stream glyph doesn't)
 * TODO(ECM): Factor entropy and top_gap into color mapping, not just confidence.
 *   Currently only confidence drives the amber heatmap. Entropy and top_gap are
 *   captured in data-* attributes but unused in rendering.
 * TODO(WMS): Window morph support (separate PR)
 * TODO(SSL): Signal summary logging — StreamChat path has no post-generation
 *   summary (Chat path logs entropy avg/max, confidence avg/min). Add equivalent.
 * TODO(ATS): Write per-generation attestations with signal attributes to ATS
 *   after stream completes. See inference-internals.md checklist.
 */

import type { Glyph } from './glyph';
import type { LLMStreamMessage } from '../../../types/websocket';
import type { LLMTokenSignal } from '@generated/server';
import { log, SEG } from '../../logger';
import { canvasPlaced } from './manifestations/canvas-placed';
import { storeCleanup } from './glyph-interaction';
import { autoMeldResultBelow } from './meld/meld-system';
import { uiState } from '../../state/ui';
import { registerHandler, unregisterHandler } from '../../websocket';
import { apiFetch } from '../../api';
import { canvasSyncQueue } from '../../api/canvas-sync';
import { createFollowUpZone, type FollowUpRequest, type FollowUpControls } from './glyph-followup';
import { createTokenPopup } from './token-popup';

// ── Multiplexer ─────────────────────────────────────────────────────

type StreamCallback = (msg: LLMStreamMessage) => void;

const subscribers = new Map<string, StreamCallback>();

function dispatch(msg: LLMStreamMessage): void {
    const cb = subscribers.get(msg.job_id);
    if (cb) {
        cb(msg);
    } else {
        log.debug(SEG.GLYPH, `[StreamGlyph] No subscriber for job_id ${msg.job_id}`);
    }
}

export function subscribeStream(jobId: string, callback: StreamCallback): void {
    if (subscribers.size === 0) {
        registerHandler('llm_stream', dispatch);
        log.debug(SEG.GLYPH, '[StreamGlyph] Registered llm_stream handler');
    }
    subscribers.set(jobId, callback);
}

export function unsubscribeStream(jobId: string): void {
    subscribers.delete(jobId);
    if (subscribers.size === 0) {
        unregisterHandler('llm_stream');
        log.debug(SEG.GLYPH, '[StreamGlyph] Unregistered llm_stream handler');
    }
}

// ── Confidence → Color ──────────────────────────────────────────────

/**
 * Map confidence (0–1) to a CSS background-color.
 *
 * High confidence (>0.9): transparent — token blends with background.
 * Medium (0.4–0.9): subtle warm tint, increasing opacity.
 * Low (<0.4): amber/orange glow.
 *
 * Linear interpolation: alpha = (1 - confidence) clamped to [0, 0.55].
 * Hue 30 (amber) at full intensity, fading to transparent.
 */
export function confidenceToColor(confidence: number): string {
    const c = Math.max(0, Math.min(1, confidence));
    const alpha = Math.min(0.55, (1 - c) * 0.65);
    if (alpha < 0.02) return 'transparent';
    return `hsla(30, 100%, 50%, ${alpha.toFixed(3)})`;
}

// ── Persisted token data ────────────────────────────────────────────

interface StreamToken {
    text: string;
    signal?: LLMTokenSignal | null;
}

export interface StreamGlyphContent {
    tokens: StreamToken[];
    model?: string;
    prompt?: string;
}

// ── DOM budget ──────────────────────────────────────────────────────

/** Global max token spans across ALL stream glyphs. */
const GLOBAL_DOM_BUDGET = 2500;

interface StreamInstance {
    output: HTMLElement;
    tokens: StreamToken[];
    visible: boolean;
}

const instances = new Set<StreamInstance>();

/** Count total spans across all visible stream glyphs */
function globalSpanCount(): number {
    let count = 0;
    for (const inst of instances) {
        if (inst.visible) count += inst.output.children.length;
    }
    return count;
}

/** Evict oldest spans globally until under budget */
function evictToFit(): void {
    while (globalSpanCount() > GLOBAL_DOM_BUDGET) {
        // Find the visible instance with the most spans
        let largest: StreamInstance | null = null;
        let largestCount = 0;
        for (const inst of instances) {
            if (inst.visible && inst.output.children.length > largestCount) {
                largest = inst;
                largestCount = inst.output.children.length;
            }
        }
        if (!largest || largestCount === 0) break;
        largest.output.removeChild(largest.output.children[0]);
    }
}

/** Render spans for a stream instance (up to its share of the budget) */
function renderSpans(inst: StreamInstance): void {
    const output = inst.output;
    output.textContent = '';
    const visibleCount = [...instances].filter(i => i.visible).length;
    const perGlyph = Math.floor(GLOBAL_DOM_BUDGET / Math.max(1, visibleCount));
    const start = Math.max(0, inst.tokens.length - perGlyph);
    for (let i = start; i < inst.tokens.length; i++) {
        output.appendChild(renderToken(inst.tokens[i], i));
    }
}

/** Shared IntersectionObserver — clears spans when off-screen, restores on-screen */
const visibilityObserver: IntersectionObserver | null =
    typeof IntersectionObserver !== 'undefined'
        ? new IntersectionObserver((entries) => {
            for (const entry of entries) {
                const inst = [...instances].find(i => i.output === entry.target);
                if (!inst) continue;

                if (entry.isIntersecting && !inst.visible) {
                    inst.visible = true;
                    renderSpans(inst);
                    evictToFit();
                } else if (!entry.isIntersecting && inst.visible) {
                    inst.visible = false;
                    inst.output.textContent = '';
                }
            }
        }, { threshold: 0 })
        : null;

// ── Token rendering ─────────────────────────────────────────────────

function renderToken(token: StreamToken, tokenIndex: number): HTMLSpanElement {
    const span = document.createElement('span');
    span.textContent = token.text;
    span.dataset.tokenIndex = String(tokenIndex);

    if (token.signal) {
        span.style.backgroundColor = confidenceToColor(token.signal.confidence);
        span.dataset.confidence = String(token.signal.confidence);
        span.dataset.entropy = String(token.signal.entropy);
        span.dataset.topGap = String(token.signal.top_gap);
        if (token.signal.top_k) {
            span.dataset.topK = JSON.stringify(token.signal.top_k);
        }
    }

    return span;
}

/** Collect all text from the tokens array (DOM may be capped) */
function collectText(tokens: StreamToken[]): string {
    let text = '';
    for (const token of tokens) {
        text += token.text;
    }
    return text;
}

// ── Stream Glyph Factory ────────────────────────────────────────────

/**
 * Create a stream glyph that renders live LLM tokens with confidence coloring.
 *
 * @param glyph - Glyph metadata (id, position, etc.)
 * @param promptGlyphId - The prompt glyph's ID, used as the WebSocket job_id key.
 *                         Empty string for restored glyphs (no active stream).
 * @param promptText - Optional prompt text to display in the header.
 * @returns The DOM element (canvas-placed)
 */
export function createStreamGlyph(glyph: Glyph, promptGlyphId: string, promptText?: string): HTMLElement {
    const tokens: StreamToken[] = [];
    let streamModel: string | undefined;

    // Close button
    const closeBtn = document.createElement('button');
    closeBtn.className = 'titlebar-btn';
    closeBtn.textContent = '×';
    closeBtn.title = 'Close stream';
    closeBtn.addEventListener('click', () => {
        if (promptGlyphId) unsubscribeStream(promptGlyphId);
        element.remove();
        uiState.removeCanvasGlyph(glyph.id);
    });

    const { element } = canvasPlaced({
        glyph,
        className: 'canvas-stream-glyph',
        defaults: { x: 200, y: 200, width: 420, height: 200 },
        titleBar: { label: 'Stream', actions: [closeBtn] },
        resizable: { minWidth: 200, minHeight: 80 },
        logLabel: 'StreamGlyph',
    });

    element.style.backgroundColor = 'transparent';
    element.style.borderRadius = '0 0 2px 2px';
    element.style.border = '1px solid var(--border-on-dark)';
    element.style.borderTop = 'none';
    element.style.zIndex = '1';

    // Restore saved tokens if this glyph has persisted content
    const saved = uiState.getCanvasGlyphs().find(g => g.id === glyph.id);
    if (saved?.content) {
        try {
            const content = JSON.parse(saved.content) as StreamGlyphContent;
            streamModel = content.model;
            if (content.prompt && !promptText) promptText = content.prompt;
            for (const token of content.tokens) {
                tokens.push(token);
            }
            log.debug(SEG.GLYPH, `[StreamGlyph] Restored ${content.tokens.length} tokens for ${glyph.id}`);
        } catch (e) {
            log.error(SEG.GLYPH, `[StreamGlyph] Failed to parse saved content for ${glyph.id}:`, e);
        }
    }

    // Prompt text header (like result glyph's prompt label)
    if (promptText) {
        const promptLabel = document.createElement('div');
        promptLabel.className = 'stream-prompt-label';
        promptLabel.style.padding = '4px 8px';
        promptLabel.style.fontSize = '12px';
        promptLabel.style.fontFamily = 'monospace';
        promptLabel.style.color = 'var(--text-secondary)';
        promptLabel.style.borderBottom = '1px solid var(--border-on-dark)';
        promptLabel.style.whiteSpace = 'pre-wrap';
        promptLabel.style.wordBreak = 'break-word';
        promptLabel.textContent = promptText;
        element.appendChild(promptLabel);
    }

    // Output container
    const output = document.createElement('div');
    output.className = 'stream-glyph-output glyph-content-area';
    output.style.fontFamily = 'monospace';
    output.style.fontSize = '12px';
    output.style.whiteSpace = 'pre-wrap';
    output.style.wordBreak = 'break-word';
    output.style.overflowWrap = 'break-word';
    output.style.backgroundColor = 'rgba(10, 10, 10, 0.85)';
    output.style.color = 'var(--text-on-dark)';
    element.appendChild(output);

    // Register this instance for global DOM budget management
    const instance: StreamInstance = { output, tokens, visible: true };
    instances.add(instance);
    visibilityObserver?.observe(output);

    // Render restored tokens (share of global budget)
    renderSpans(instance);
    evictToFit();

    // Token hover popup + nebula scrub — event delegation on the output container
    const popup = createTokenPopup();
    output.addEventListener('mouseenter', (e: MouseEvent) => {
        const target = e.target as HTMLElement;
        if (target.tagName === 'SPAN' && target.dataset.confidence) {
            popup.show(target as HTMLSpanElement);
            if (target.dataset.tokenIndex) {
                document.dispatchEvent(new CustomEvent('nebula-scrub', {
                    detail: { index: parseInt(target.dataset.tokenIndex, 10) },
                }));
            }
        }
    }, true);
    output.addEventListener('mouseleave', (e: MouseEvent) => {
        const target = e.target as HTMLElement;
        if (target.tagName === 'SPAN') {
            popup.hide();
            document.dispatchEvent(new CustomEvent('nebula-scrub', {
                detail: { index: -1 },
            }));
        }
    }, true);

    /** Persist current tokens to canvas state */
    function persistContent(): void {
        const content: StreamGlyphContent = { tokens, model: streamModel, prompt: promptText };
        const existing = uiState.getCanvasGlyphs().find(g => g.id === glyph.id);
        if (existing) {
            uiState.addCanvasGlyph({ ...existing, content: JSON.stringify(content) });
        }
    }

    // Subscribe to live stream (only if actively streaming)
    if (promptGlyphId) {
        subscribeStream(promptGlyphId, (msg: LLMStreamMessage) => {
            if (msg.model) streamModel = msg.model;

            if (msg.done) {
                unsubscribeStream(promptGlyphId);
                persistContent();
                log.debug(SEG.GLYPH, `[StreamGlyph] Stream complete for ${promptGlyphId}, ${tokens.length} tokens`);
                return;
            }

            if (!msg.content) return;

            const token: StreamToken = { text: msg.content, signal: msg.signal };
            const tokenIndex = tokens.length;
            tokens.push(token);
            if (instance.visible) {
                output.appendChild(renderToken(token, tokenIndex));
                evictToFit();
            }
            output.scrollTop = output.scrollHeight;
        });
    }

    // Follow-up input zone — spawns a stream glyph before the API call
    // so tokens flow in live with confidence coloring
    const followupZone = createFollowUpZone({
        element,
        glyph,
        getSystemPrompt: () => collectText(tokens),
        getModel: () => streamModel,
        logLabel: 'StreamGlyph',
        onExecute: (request: FollowUpRequest, controls: FollowUpControls) => {
            executeStreamFollowUp(element, glyph, request, controls);
        },
    });
    element.appendChild(followupZone);

    // Cleanup
    storeCleanup(element, () => {
        if (promptGlyphId) unsubscribeStream(promptGlyphId);
        visibilityObserver?.unobserve(output);
        instances.delete(instance);
        popup.destroy();
    });

    return element;
}

/**
 * Check whether a stream glyph received any tokens.
 */
export function getStreamTokenCount(streamElement: HTMLElement): number {
    const output = streamElement.querySelector('.stream-glyph-output');
    if (!output) return 0;
    return output.children.length;
}

// ── Streaming follow-up execution ───────────────────────────────────

/**
 * Execute a follow-up by spawning a stream glyph first, subscribing it,
 * then firing the API call so tokens stream in live with heatmap coloring.
 */
async function executeStreamFollowUp(
    parentElement: HTMLElement,
    parentGlyph: Glyph,
    request: FollowUpRequest,
    controls: FollowUpControls,
): Promise<void> {
    const parentRect = parentElement.getBoundingClientRect();
    const canvas = parentElement.closest('.canvas-workspace') as HTMLElement;
    if (!canvas) {
        controls.error('No canvas-workspace ancestor');
        return;
    }
    const canvasRect = canvas.getBoundingClientRect();

    const sx = parentRect.left - canvasRect.left;
    const sy = parentRect.bottom - canvasRect.top;

    const streamGlyphId = `stream-${crypto.randomUUID()}`;

    // Register in canvas state
    uiState.addCanvasGlyph({
        id: streamGlyphId,
        symbol: 'stream',
        x: sx,
        y: sy,
        width: Math.round(parentRect.width),
        height: 200,
    });

    const streamGlyph: Glyph = {
        id: streamGlyphId,
        title: 'Stream',
        symbol: 'stream',
        x: sx,
        y: sy,
        width: Math.round(parentRect.width),
        renderContent: () => document.createElement('div'),
    };

    // Spawn stream glyph and subscribe it BEFORE firing the API call.
    // The new stream glyph's own ID is used as both the subscription key
    // and the glyph_id in the request — the backend keys llm_stream messages
    // by the glyph_id from the request body.
    const streamElement = createStreamGlyph(streamGlyph, streamGlyphId, request.text);
    canvas.appendChild(streamElement);

    const parentGlyphId = parentElement.dataset.glyphId;
    if (parentGlyphId) {
        autoMeldResultBelow(
            parentElement, parentGlyphId, 'stream',
            'StreamGlyph', streamElement, streamGlyphId, 'StreamFollowUp',
        );
    }

    // Ensure the composition (with the new stream glyph edge) is persisted to the DB
    // BEFORE firing the API call. The backend's ConversationAssembler traces meld edges
    // to build multi-turn history — it needs the composition to exist in the DB.
    await canvasSyncQueue.flush();

    // Fire the API call — tokens will stream in via WebSocket
    // glyph_id = streamGlyphId so backend's llm_stream job_id matches the subscription
    const body: Record<string, unknown> = {
        template: request.template,
        system_prompt: request.systemPrompt,
        glyph_id: streamGlyphId,
    };
    if (parentGlyphId) body.parent_glyph_id = parentGlyphId;
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

            if (data.error) {
                // Remove stream glyph on API-level error
                unsubscribeStream(streamGlyphId);
                streamElement.remove();
                uiState.removeCanvasGlyph(streamGlyphId);
                controls.error(`Failed: ${data.error}`);
                return;
            }

            // If stream glyph got no tokens (non-streaming provider), populate from response
            const tokenCount = getStreamTokenCount(streamElement);
            if (tokenCount === 0 && data.response) {
                const output = streamElement.querySelector('.stream-glyph-output');
                if (output) {
                    const span = document.createElement('span');
                    span.textContent = data.response;
                    output.appendChild(span);
                }
            }

            log.debug(SEG.GLYPH, `[StreamGlyph] Follow-up complete, ${tokenCount} streamed tokens`);
        })
        .catch((err) => {
            // Remove stream glyph on network error
            unsubscribeStream(request.glyphId);
            streamElement.remove();
            uiState.removeCanvasGlyph(streamGlyphId);

            const errMsg = err instanceof Error ? err.message : String(err);
            controls.error(`Failed: ${errMsg}`);
            log.error(SEG.GLYPH, `[StreamGlyph] Follow-up failed for ${parentGlyph.id}: ${errMsg}`);
        });
}
