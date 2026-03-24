/**
 * Stream Glyph — live inference heatmap
 *
 * Renders LLM tokens in real-time via WebSocket llm_stream messages.
 * Each token is a <span> colored by confidence: high confidence blends
 * with background, low confidence glows warm (amber/orange).
 *
 * Multiplexer pattern: one WebSocket handler routes messages to many
 * stream glyph instances by job_id.
 */

import type { Glyph } from './glyph';
import type { LLMStreamMessage } from '../../../types/websocket';
import { log, SEG } from '../../logger';
import { canvasPlaced } from './manifestations/canvas-placed';
import { storeCleanup } from './glyph-interaction';
import { registerHandler, unregisterHandler } from '../../websocket';

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

// ── Stream Glyph Factory ────────────────────────────────────────────

/**
 * Create a stream glyph that renders live LLM tokens with confidence coloring.
 *
 * @param glyph - Glyph metadata (id, position, etc.)
 * @param promptGlyphId - The prompt glyph's ID, used as the WebSocket job_id key
 * @returns The DOM element (canvas-placed)
 */
export function createStreamGlyph(glyph: Glyph, promptGlyphId: string): HTMLElement {
    let tokenCount = 0;

    // Close button
    const closeBtn = document.createElement('button');
    closeBtn.className = 'titlebar-btn';
    closeBtn.textContent = '×';
    closeBtn.title = 'Close stream';
    closeBtn.addEventListener('click', () => {
        unsubscribeStream(promptGlyphId);
        element.remove();
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

    // Output container — same visual style as result glyph
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

    // Subscribe to stream messages
    subscribeStream(promptGlyphId, (msg: LLMStreamMessage) => {
        if (msg.done) {
            unsubscribeStream(promptGlyphId);
            log.debug(SEG.GLYPH, `[StreamGlyph] Stream complete for ${promptGlyphId}, ${tokenCount} tokens`);
            return;
        }

        if (!msg.content) return;

        const span = document.createElement('span');
        span.textContent = msg.content;

        if (msg.signal) {
            span.style.backgroundColor = confidenceToColor(msg.signal.confidence);

            // Store signal data for future hover popup (Step 4)
            span.dataset.confidence = String(msg.signal.confidence);
            span.dataset.entropy = String(msg.signal.entropy);
            span.dataset.topGap = String(msg.signal.top_gap);
            if (msg.signal.top_k) {
                span.dataset.topK = JSON.stringify(msg.signal.top_k);
            }
        }

        output.appendChild(span);
        tokenCount++;

        // Auto-scroll to bottom
        output.scrollTop = output.scrollHeight;
    });

    // Cleanup on removal
    storeCleanup(element, () => {
        unsubscribeStream(promptGlyphId);
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
