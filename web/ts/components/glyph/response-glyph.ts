/**
 * Response Glyph — unified output display for LLM responses.
 *
 * Handles both live streaming (tokens arrive via WebSocket) and static
 * results (execution output, non-streaming providers). A single glyph
 * transitions from streaming → complete, gaining copy/expand buttons
 * when the stream finishes.
 *
 * Replaces the former stream-glyph.ts and result-glyph.ts.
 *
 * Token hover popup showing sampler stage data — see token-popup.ts
 * TODO(ECM): Factor entropy and top_gap into color mapping, not just confidence.
 * TODO(SSL): Signal summary logging for StreamChat path.
 * TODO(ATS): Write per-generation attestations after stream completes.
 * TODO(DIM/#749): Dim intersecting token labels when a token is selected, popover on select only.
 * TODO(CLR/#750): Use embedding dimensions (PCA 4-6) to drive particle hue — semantic color.
 */

import type { Glyph } from './glyph';
import type { LLMStreamMessage } from '../../../types/websocket';
import type { LLMTokenSignal, SamplerStageSignal } from '@generated/server';
import { log, SEG } from '../../logger';
import { canvasPlaced } from './manifestations/canvas-placed';
import { unmeldComposition } from './meld/meld-composition';
import { makeDraggable, storeCleanup, preventDrag } from './glyph-interaction';
import { morphCanvasPlacedToWindow, placeWindowOnCanvas } from './manifestations/canvas-window';
import { isInWindowState } from './dataset';
import { glyphRun } from './run';
import { autoMeldResultBelow } from './meld/meld-system';
import { uiState } from '../../state/ui';
import { registerHandler, unregisterHandler } from '../../websocket';
import { apiFetch, getBackendUrl } from '../../api';
import { canvasSyncQueue } from '../../api/canvas-sync';
import { createFollowUpZone, type FollowUpRequest, type FollowUpControls } from './glyph-followup';
import { createTokenPopup, samplerInfluenceColor } from './token-popup';

// ── Types ────────────────────────────────────────────────────────────

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

interface StreamToken {
    text: string;
    signal?: LLMTokenSignal | null;
}

interface GenerationStats {
    tokenCount: number;
    tokPerSec?: number;
    model?: string;
}

// Last nebula frame stored as base64 PNG for persistence across refresh
type NebulaFrame = string; // base64-encoded PNG data (without data: prefix)

export interface ResultGlyphContent {
    result?: ExecutionResult;
    tokens?: StreamToken[];
    model?: string;
    promptConfig?: PromptConfig;
    prompt?: string;
    followupError?: string;
    stats?: GenerationStats;
    nebulaFrame?: NebulaFrame;
}

// ── Multiplexer ──────────────────────────────────────────────────────

type StreamCallback = (msg: LLMStreamMessage) => void;

const subscribers = new Map<string, StreamCallback>();

function dispatch(msg: LLMStreamMessage): void {
    const cb = subscribers.get(msg.job_id);
    if (cb) {
        cb(msg);
    } else {
        log.debug(SEG.GLYPH, `[ResponseGlyph] No subscriber for job_id ${msg.job_id}`);
    }
}

export function subscribeStream(jobId: string, callback: StreamCallback): void {
    if (subscribers.size === 0) {
        registerHandler('llm_stream', dispatch);
        log.debug(SEG.GLYPH, '[ResponseGlyph] Registered llm_stream handler');
    }
    subscribers.set(jobId, callback);
}

export function unsubscribeStream(jobId: string): void {
    subscribers.delete(jobId);
    if (subscribers.size === 0) {
        unregisterHandler('llm_stream');
        log.debug(SEG.GLYPH, '[ResponseGlyph] Unregistered llm_stream handler');
    }
}

function renderStatsLine(container: HTMLElement, stats: GenerationStats): void {
    const el = document.createElement('div');
    el.className = 'response-glyph-stats';
    el.style.fontSize = '10px';
    el.style.color = 'var(--text-muted)';
    el.style.padding = '4px 8px 2px';
    el.style.textAlign = 'right';
    el.style.opacity = '0.6';
    const parts: string[] = [];
    if (stats.model) parts.push(stats.model);
    parts.push(`${stats.tokenCount} tokens`);
    if (stats.tokPerSec) parts.push(`${stats.tokPerSec.toFixed(1)} tok/s`);
    el.textContent = parts.join(' · ');
    container.appendChild(el);
}

// ── Confidence → Color ──────────────────────────────────────────────

/**
 * Map confidence (0–1) to a CSS background-color.
 * High confidence (>0.9): transparent. Low (<0.4): amber glow.
 */
export function confidenceToColor(confidence: number): string {
    const c = Math.max(0, Math.min(1, confidence));
    const alpha = Math.min(0.55, (1 - c) * 0.65);
    if (alpha < 0.02) return 'transparent';
    return `hsla(30, 100%, 50%, ${alpha.toFixed(3)})`;
}

interface StreamInstance {
    output: HTMLElement;
    tokens: StreamToken[];
    visible: boolean;
}

const instances = new Set<StreamInstance>();

type ColorMode = 'confidence' | 'sampler';
let colorMode: ColorMode = 'sampler';

export function toggleColorMode(): ColorMode {
    colorMode = colorMode === 'confidence' ? 'sampler' : 'confidence';
    for (const inst of instances) {
        if (inst.visible) renderSpans(inst);
    }
    return colorMode;
}

function renderSpans(inst: StreamInstance): void {
    const output = inst.output;
    output.textContent = '';
    for (let i = 0; i < inst.tokens.length; i++) {
        output.appendChild(renderToken(inst.tokens[i], i, colorMode));
    }
}

const visibilityObserver: IntersectionObserver | null =
    typeof IntersectionObserver !== 'undefined'
        ? new IntersectionObserver((entries) => {
            for (const entry of entries) {
                const inst = [...instances].find(i => i.output === entry.target);
                if (!inst) continue;
                if (entry.isIntersecting && !inst.visible) {
                    inst.visible = true;
                    renderSpans(inst);
                } else if (!entry.isIntersecting && inst.visible) {
                    inst.visible = false;
                    inst.output.textContent = '';
                }
            }
        }, { threshold: 0 })
        : null;

// ── Token rendering ─────────────────────────────────────────────────

function tokenColor(token: StreamToken, mode: ColorMode): string {
    if (!token.signal) return 'transparent';
    if (mode === 'sampler' && token.signal.sampler_stages) {
        const color = samplerInfluenceColor(token.signal.sampler_stages as SamplerStageSignal[]);
        if (color !== 'transparent') return color;
    }
    return confidenceToColor(token.signal.confidence);
}

function renderToken(token: StreamToken, tokenIndex: number, mode: ColorMode): HTMLSpanElement {
    const span = document.createElement('span');
    span.textContent = token.text;
    span.dataset.tokenIndex = String(tokenIndex);

    if (token.signal) {
        span.style.backgroundColor = tokenColor(token, mode);
        span.dataset.confidence = String(token.signal.confidence);
        span.dataset.entropy = String(token.signal.entropy);
        span.dataset.topGap = String(token.signal.top_gap);
        if (token.signal.top_k) {
            span.dataset.topK = JSON.stringify(token.signal.top_k);
        }
        if (token.signal.sampler_stages) {
            span.dataset.samplerStages = JSON.stringify(token.signal.sampler_stages);
        }
    }

    return span;
}

function collectText(tokens: StreamToken[]): string {
    let text = '';
    for (const token of tokens) {
        text += token.text;
    }
    return text;
}

// ── Static output rendering ─────────────────────────────────────────

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

// ── Response Glyph Factory ──────────────────────────────────────────

/**
 * Create a response glyph — handles both streaming and static results.
 *
 * @param glyph - Glyph metadata
 * @param result - Static execution result (null for streaming mode)
 * @param promptConfig - Model/provider config
 * @param prompt - Prompt text for header display
 * @param streamJobId - WebSocket job ID to subscribe to (enables streaming mode)
 */
export function createResponseGlyph(
    glyph: Glyph,
    result?: ExecutionResult,
    promptConfig?: PromptConfig,
    prompt?: string,
    streamJobId?: string,
): HTMLElement {
    const tokens: StreamToken[] = [];
    let streamModel: string | undefined = promptConfig?.model;
    let isStreaming = !!streamJobId;
    let streamInstance: StreamInstance | null = null;
    let popup: ReturnType<typeof createTokenPopup> | null = null;
    let savedStats: GenerationStats | undefined;

    // ── Header ──────────────────────────────────────────────────────

    const header = document.createElement('div');
    header.className = 'glyph-title-bar glyph-title-bar--auto result-glyph-header';

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

    const buttonContainer = document.createElement('div');
    buttonContainer.style.display = 'flex';
    buttonContainer.style.gap = '3px';
    buttonContainer.style.flexShrink = '0';
    buttonContainer.style.marginLeft = 'auto';

    function headerBtn(label: string, title: string): HTMLButtonElement {
        const btn = document.createElement('button');
        btn.className = 'titlebar-btn';
        btn.textContent = label;
        btn.title = title;
        return btn;
    }

    // Color toggle — visible when tokens are present
    const colorBtn = headerBtn('◐', 'Toggle coloring: confidence / sampler influence');
    colorBtn.style.display = 'none';
    colorBtn.addEventListener('click', () => {
        const mode = toggleColorMode();
        colorBtn.title = mode === 'confidence'
            ? 'Coloring: confidence (click for sampler)'
            : 'Coloring: sampler influence (click for confidence)';
    });
    buttonContainer.appendChild(colorBtn);

    // Copy — hidden during streaming
    const copyBtn = headerBtn('⎘', 'Copy to clipboard');
    if (isStreaming) copyBtn.style.display = 'none';
    copyBtn.addEventListener('click', () => {
        let text = '';
        if (prompt) text += `> ${prompt.split('\n').join('\n> ')}\n\n`;
        if (tokens.length > 0) {
            text += collectText(tokens);
        } else if (result) {
            text += result.stdout || result.error || '(no output)';
        }
        navigator.clipboard.writeText(text).then(() => {
            copyBtn.textContent = '✓';
            setTimeout(() => { copyBtn.textContent = '⎘'; }, 1500);
        });
    });
    buttonContainer.appendChild(copyBtn);

    // Expand to window — hidden during streaming
    const toWindowBtn = headerBtn('⬆', 'Expand to window');
    if (isStreaming) toWindowBtn.style.display = 'none';
    toWindowBtn.addEventListener('click', () => {
        if (isInWindowState(element)) {
            placeWindowOnCanvas(element, {
                onRestoreComplete: (el) => {
                    const contentStr = uiState.getCanvasGlyphs().find(g => g.id === glyph.id)?.content;
                    uiState.addCanvasGlyph({
                        id: glyph.id,
                        symbol: 'result',
                        x: parseInt(el.style.left),
                        y: parseInt(el.style.top),
                        width: parseInt(el.style.width),
                        height: parseInt(el.style.height),
                        content: contentStr,
                    });
                    toWindowBtn.textContent = '⬆';
                    toWindowBtn.title = 'Expand to window';
                    log.debug(SEG.GLYPH, `[ResponseGlyph] Placed on canvas ${glyph.id}`);
                },
            });
        } else {
            const canvas = element.closest('.canvas-workspace') as HTMLElement | null;
            const canvasId = (canvas?.closest('[data-canvas-id]') as HTMLElement | null)?.dataset?.canvasId ?? 'canvas-workspace';

            morphCanvasPlacedToWindow(element, {
                title: prompt || 'Result',
                canvasId,
                onClose: () => {
                    element.remove();
                    uiState.removeCanvasGlyph(glyph.id);
                    log.debug(SEG.GLYPH, `[ResponseGlyph] Closed from window ${glyph.id}`);
                },
                onMinimize: (el: HTMLElement) => {
                    glyphRun.adopt(el, {
                        id: glyph.id,
                        title: prompt || 'Result',
                        symbol: 'result',
                        renderContent: () => renderResultContent(result ?? null, tokens, promptConfig, prompt),
                        renderTitleBar: () => buildResultTitleBar(result ?? null, tokens, prompt),
                        onClose: () => {
                            log.debug(SEG.GLYPH, `[ResponseGlyph] Closed from tray ${glyph.id}`);
                        },
                    });
                    log.debug(SEG.GLYPH, `[ResponseGlyph] Minimized to tray ${glyph.id}`);
                },
                onRestoreComplete: () => {
                    log.debug(SEG.GLYPH, `[ResponseGlyph] Restored to canvas ${glyph.id}`);
                },
            });

            toWindowBtn.textContent = '↩';
            toWindowBtn.title = 'Return to canvas';
        }
    });
    buttonContainer.appendChild(toWindowBtn);

    // Close — always visible, composition-aware
    const closeBtn = headerBtn('×', 'Close');
    closeBtn.addEventListener('click', () => {
        if (streamJobId) unsubscribeStream(streamJobId);

        const composition = element.closest('.melded-composition') as HTMLElement | null;
        if (composition) {
            const unmelded = unmeldComposition(composition);
            if (unmelded) {
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
                log.debug(SEG.GLYPH, `[ResponseGlyph] Unmelded composition before closing ${glyph.id}`);
            }
        }

        element.remove();
        uiState.removeCanvasGlyph(glyph.id);
        log.debug(SEG.GLYPH, `[ResponseGlyph] Closed ${glyph.id}`);
    });
    buttonContainer.appendChild(closeBtn);

    header.appendChild(buttonContainer);

    // ── Element ─────────────────────────────────────────────────────

    const calculatedHeight = result
        ? Math.min(400, Math.max(80, (result.stdout + result.stderr + (result.error || '')).split('\n').length * 18 + 60))
        : 200;

    const { element } = canvasPlaced({
        glyph,
        className: 'canvas-result-glyph',
        defaults: { x: 200, y: 200, width: 420, height: calculatedHeight },
        dragHandle: header,
        draggableOptions: { ignoreButtons: true },
        resizable: { minWidth: 200, minHeight: 80 },
        logLabel: 'ResponseGlyph',
    });
    element.style.minHeight = '80px';
    element.style.backgroundColor = '#050208';
    element.style.borderRadius = '0 0 2px 2px';
    element.style.border = '1px solid var(--border-on-dark)';
    element.style.borderTop = 'none';
    element.style.zIndex = '1';
    header.style.position = 'relative';
    header.style.zIndex = '1';
    element.appendChild(header);

    // ── Output container ────────────────────────────────────────────

    const output = document.createElement('div');
    output.className = 'result-glyph-output glyph-content-area';
    output.style.position = 'relative';
    output.style.zIndex = '1';
    output.style.fontFamily = 'monospace';
    output.style.fontSize = '12px';
    output.style.whiteSpace = 'pre-wrap';
    output.style.wordBreak = 'break-word';
    output.style.overflowWrap = 'break-word';
    output.style.backgroundColor = 'transparent';
    output.style.color = 'var(--text-on-dark)';
    preventDrag(output); // token clicks must not trigger composition drag
    element.appendChild(output);

    // ── Nebula canvas ──────────────────────────────────────────────
    // Small inline particle view from the Metal renderer. Connects to
    // the llama-cpp plugin's WebSocket frame stream during generation.

    // Nebula renders behind the text as the glyph's background
    const nebulaCanvas = document.createElement('canvas');
    nebulaCanvas.style.cssText = 'position:absolute;top:0;left:0;width:100%;height:100%;pointer-events:none;z-index:0;display:none;';
    const nebulaCtx = nebulaCanvas.getContext('2d');

    const nebulaStatus = document.createElement('div');
    nebulaStatus.style.cssText = 'position:absolute;bottom:2px;right:6px;font:9px monospace;color:rgba(255,255,255,0.25);z-index:1;';

    let lastNebulaBitmap: ImageBitmap | null = null;

    // Resize canvas to match element pixel density, redraw last frame
    function fitNebulaCanvas(): void {
        const rect = element.getBoundingClientRect();
        nebulaCanvas.width = Math.round(rect.width * devicePixelRatio);
        nebulaCanvas.height = Math.round(rect.height * devicePixelRatio);
        if (lastNebulaBitmap && nebulaCtx) {
            nebulaCtx.drawImage(lastNebulaBitmap, 0, 0, nebulaCanvas.width, nebulaCanvas.height);
        }
    }
    const nebulaRo = new ResizeObserver(fitNebulaCanvas);

    let nebulaWs: WebSocket | null = null;
    let nebulaFpsFrames = 0;
    let nebulaFpsLast = performance.now();
    let nebulaLive = false; // true during active streaming — frames are drawn
    let nebulaScrub = false; // true while scrubbing a completed response — frames drawn

    function connectNebula(): void {
        const base = getBackendUrl().replace(/^http/, 'ws');
        nebulaWs = new WebSocket(`${base}/ws/llama-cpp`);

        nebulaWs.onopen = () => {
            if (nebulaLive) nebulaStatus.textContent = 'connected';
            log.debug(SEG.GLYPH, '[ResponseGlyph] Nebula WebSocket connected');
        };

        nebulaWs.onmessage = (event: MessageEvent) => {
            if (!nebulaLive && !nebulaScrub) return; // no active stream or scrub — ignore
            try {
                const msg = JSON.parse(event.data);
                if (msg.type !== 1 || !msg.data) return;

                const binary = atob(msg.data);
                const bytes = new Uint8Array(binary.length);
                for (let i = 0; i < binary.length; i++) {
                    bytes[i] = binary.charCodeAt(i);
                }
                const blob = new Blob([bytes], { type: 'image/png' });
                lastNebulaBase64 = msg.data; // store raw base64 for persistence
                createImageBitmap(blob).then((bmp) => {
                    if (!nebulaCtx) return;
                    if (lastNebulaBitmap) lastNebulaBitmap.close();
                    lastNebulaBitmap = bmp;
                    nebulaCtx.clearRect(0, 0, nebulaCanvas.width, nebulaCanvas.height);
                    nebulaCtx.drawImage(bmp, 0, 0, nebulaCanvas.width, nebulaCanvas.height);
                    nebulaFpsFrames++;
                    const now = performance.now();
                    if (now - nebulaFpsLast >= 1000) {
                        nebulaStatus.textContent = Math.round(nebulaFpsFrames * 1000 / (now - nebulaFpsLast)) + ' fps';
                        nebulaFpsFrames = 0;
                        nebulaFpsLast = now;
                    }
                });
            } catch (e) {
                log.error(SEG.GLYPH, '[ResponseGlyph] Nebula frame error', e);
            }
        };

        nebulaWs.onerror = () => { if (nebulaLive) nebulaStatus.textContent = 'error'; };
        nebulaWs.onclose = () => { if (nebulaLive) nebulaStatus.textContent = ''; };
    }

    // Store last frame as base64 PNG for persistence across refresh
    let lastNebulaBase64: string | null = null;

    function sendNebulaMessage(data: string): void {
        // Track scrub state — enable frame drawing during scrub on completed glyphs
        if (data.indexOf('scrub:') === 0) {
            nebulaScrub = data !== 'scrub:-1';
        }

        // Lazy reconnect — if WebSocket is closed (post-stream), reconnect
        if (!nebulaWs || nebulaWs.readyState === WebSocket.CLOSED || nebulaWs.readyState === WebSocket.CLOSING) {
            connectNebula();
            const pending = data;
            const origOnOpen = nebulaWs!.onopen;
            nebulaWs!.onopen = (ev) => {
                if (origOnOpen) (origOnOpen as (ev: Event) => void)(ev);
                const msg = { type: 1, data: btoa(pending), headers: {}, timestamp: 0 };
                nebulaWs!.send(JSON.stringify(msg));
            };
            return;
        }
        if (nebulaWs.readyState === WebSocket.OPEN) {
            const msg = { type: 1, data: btoa(data), headers: {}, timestamp: 0 };
            nebulaWs.send(JSON.stringify(msg));
        }
    }

    function closeNebula(): void {
        nebulaLive = false;
        if (nebulaWs && nebulaWs.readyState !== WebSocket.CLOSED) {
            nebulaWs.close();
        }
        nebulaWs = null;
    }

    element.style.position = 'relative';
    element.insertBefore(nebulaCanvas, element.firstChild);
    element.appendChild(nebulaStatus);
    nebulaRo.observe(element);

    // WASD + arrow camera controls — active only when a token is selected
    let nebulaNavActive = false;
    const camStep = 0.02;
    const camRotStep = 0.03;
    const keyMap: Record<string, string> = {
        w: `cam:0,${camStep},1,0,0`,
        s: `cam:0,${-camStep},1,0,0`,
        a: `cam:${-camStep},0,1,0,0`,
        d: `cam:${camStep},0,1,0,0`,
        ArrowUp: `cam:0,0,1,0,${-camRotStep}`,
        ArrowDown: `cam:0,0,1,0,${camRotStep}`,
        ArrowLeft: `cam:0,0,1,${-camRotStep},0`,
        ArrowRight: `cam:0,0,1,${camRotStep},0`,
        q: `cam:0,0,0.9,0,0`,
        e: `cam:0,0,1.1,0,0`,
        r: 'cam:r',
        Escape: 'cam:r',
    };
    function onNebulaKey(e: KeyboardEvent): void {
        if (!nebulaNavActive) return;
        if (e.target instanceof HTMLTextAreaElement || e.target instanceof HTMLInputElement) return;
        const cmd = keyMap[e.key];
        if (cmd) {
            e.preventDefault();
            sendNebulaMessage(cmd);
        }
    }
    document.addEventListener('keydown', onNebulaKey);

    // ── Restore saved content ───────────────────────────────────────

    const saved = uiState.getCanvasGlyphs().find(g => g.id === glyph.id);
    if (saved?.content && !result && !streamJobId) {
        try {
            const content = JSON.parse(saved.content) as ResultGlyphContent;
            if (content.tokens && content.tokens.length > 0) {
                // Restore from saved token data
                for (const token of content.tokens) tokens.push(token);
                streamModel = content.model;
                savedStats = content.stats;
                if (content.prompt && !prompt) prompt = content.prompt;
                log.debug(SEG.GLYPH, `[ResponseGlyph] Restored ${tokens.length} tokens for ${glyph.id}`);
            } else if (content.result) {
                // Restore from saved execution result
                result = content.result;
                promptConfig = content.promptConfig;
                if (content.prompt && !prompt) prompt = content.prompt;
            }
        } catch (e) {
            log.error(SEG.GLYPH, `[ResponseGlyph] Failed to parse saved content for ${glyph.id}:`, e);
        }
    }

    // ── Render content ──────────────────────────────────────────────

    if (tokens.length > 0) {
        // Token mode — register for DOM budget + popup
        colorBtn.style.display = '';
        streamInstance = { output, tokens, visible: true };
        instances.add(streamInstance);
        visibilityObserver?.observe(output);
        renderSpans(streamInstance);
        if (savedStats) renderStatsLine(output, savedStats);

        // Show nebula — restore last frame from persisted base64 if available
        nebulaCanvas.style.display = '';
        fitNebulaCanvas();
        if (saved?.content) {
            try {
                const restored = JSON.parse(saved.content) as ResultGlyphContent;
                if (restored.nebulaFrame) {
                    lastNebulaBase64 = restored.nebulaFrame;
                    const bin = atob(restored.nebulaFrame);
                    const arr = new Uint8Array(bin.length);
                    for (let i = 0; i < bin.length; i++) arr[i] = bin.charCodeAt(i);
                    createImageBitmap(new Blob([arr], { type: 'image/png' })).then((bmp) => {
                        if (!nebulaCtx) return;
                        lastNebulaBitmap = bmp;
                        nebulaCtx.clearRect(0, 0, nebulaCanvas.width, nebulaCanvas.height);
                        nebulaCtx.drawImage(bmp, 0, 0, nebulaCanvas.width, nebulaCanvas.height);
                    });
                }
            } catch { /* already logged above */ }
        }

        popup = createTokenPopup();
        setupTokenPopup(output, popup, (idx) => sendNebulaMessage('scrub:' + idx), (locked) => { nebulaNavActive = locked; });
    } else if (result) {
        // Static mode — render output text
        renderOutput(output, result);
        if (result.error) element.classList.add('glyph-error');
    }

    // ── Stream subscription ─────────────────────────────────────────

    if (streamJobId) {
        // Register for DOM budget before tokens arrive
        if (!streamInstance) {
            colorBtn.style.display = '';
            streamInstance = { output, tokens, visible: true };
            instances.add(streamInstance);
            visibilityObserver?.observe(output);

            popup = createTokenPopup();
            setupTokenPopup(output, popup, (idx) => sendNebulaMessage('scrub:' + idx), (locked) => { nebulaNavActive = locked; });
        }

        // Show nebula and connect to Metal renderer — live frame drawing
        nebulaCanvas.style.display = '';
        fitNebulaCanvas();
        nebulaLive = true;
        connectNebula();

        let streamStartMs = 0;

        subscribeStream(streamJobId, (msg: LLMStreamMessage) => {
            if (msg.model) streamModel = msg.model;

            if (msg.done) {
                unsubscribeStream(streamJobId!);
                isStreaming = false;
                // Reveal copy + expand buttons
                copyBtn.style.display = '';
                toWindowBtn.style.display = '';

                // Compute and persist generation stats
                const tokenCount = msg.completion_tokens || tokens.length;
                if (tokenCount > 0) {
                    const elapsedMs = streamStartMs ? performance.now() - streamStartMs : 0;
                    const tokPerSec = elapsedMs > 0 ? tokenCount / elapsedMs * 1000 : undefined;
                    savedStats = { tokenCount, tokPerSec, model: streamModel };
                    renderStatsLine(output, savedStats);
                }

                // Disconnect from Metal renderer — keep last frame as static image
                closeNebula();

                persistContent();
                log.debug(SEG.GLYPH, `[ResponseGlyph] Stream complete for ${streamJobId}, ${tokens.length} tokens`);
                return;
            }

            if (!msg.content) return;

            if (!streamStartMs) streamStartMs = performance.now();

            const token: StreamToken = { text: msg.content, signal: msg.signal };
            const tokenIndex = tokens.length;
            tokens.push(token);
            if (streamInstance!.visible) {
                output.appendChild(renderToken(token, tokenIndex, colorMode));

            }
            output.scrollTop = output.scrollHeight;
        });
    }

    // ── Follow-up zone ──────────────────────────────────────────────

    const hasTokens = tokens.length > 0 || !!streamJobId;
    const followupZone = createFollowUpZone({
        element,
        glyph,
        getSystemPrompt: () => hasTokens ? collectText(tokens) : (result?.stdout ?? ''),
        getModel: () => streamModel ?? promptConfig?.model,
        getProvider: () => promptConfig?.provider,
        logLabel: 'ResponseGlyph',
        onExecute: hasTokens
            ? (request: FollowUpRequest, controls: FollowUpControls) => {
                executeStreamFollowUp(element, glyph, request, controls);
            }
            : undefined,
    });
    element.appendChild(followupZone);

    // ── Cleanup ─────────────────────────────────────────────────────

    storeCleanup(element, () => {
        if (streamJobId) unsubscribeStream(streamJobId);
        if (streamInstance) {
            visibilityObserver?.unobserve(output);
            instances.delete(streamInstance);
        }
        popup?.destroy();
        closeNebula();
        nebulaRo.disconnect();
        document.removeEventListener('keydown', onNebulaKey);
        if (lastNebulaBitmap) { lastNebulaBitmap.close(); lastNebulaBitmap = null; }
    });

    // ── Persist helpers ─────────────────────────────────────────────

    function persistContent(): void {
        const content: ResultGlyphContent = {
            tokens: tokens.length > 0 ? tokens : undefined,
            result: result ?? undefined,
            model: streamModel,
            promptConfig,
            prompt,
            stats: savedStats,
            nebulaFrame: lastNebulaBase64 ?? undefined,
        };
        const existing = uiState.getCanvasGlyphs().find(g => g.id === glyph.id);
        if (existing) {
            uiState.addCanvasGlyph({ ...existing, content: JSON.stringify(content) });
        }
    }

    // Persist static result immediately
    if (result && !streamJobId) {
        const contentPayload: ResultGlyphContent = { result, promptConfig, prompt };
        (glyph as any).content = JSON.stringify(contentPayload);
    }

    return element;
}

// ── Token popup wiring ──────────────────────────────────────────────

function setupTokenPopup(
    output: HTMLElement,
    popup: ReturnType<typeof createTokenPopup>,
    onScrub?: (index: number) => void,
    onLockChange?: (locked: boolean) => void,
): void {
    let lockedSpan: HTMLSpanElement | null = null;

    function scrubTo(idx: number): void {
        if (onScrub) onScrub(idx);
        document.dispatchEvent(new CustomEvent('nebula-scrub', { detail: { index: idx } }));
    }

    function unlock(): void {
        if (lockedSpan) {
            lockedSpan.style.outline = '';
            lockedSpan.style.boxShadow = '';
            lockedSpan = null;
            scrubTo(-1);
            if (onLockChange) onLockChange(false);
        }
    }

    function lockToken(span: HTMLSpanElement): void {
        if (lockedSpan) {
            lockedSpan.style.outline = '';
            lockedSpan.style.boxShadow = '';
        }
        lockedSpan = span;
        lockedSpan.style.outline = '1px solid rgba(204, 85, 0, 0.65)';
        lockedSpan.style.boxShadow = '0 0 8px rgba(204, 85, 0, 0.5)';
        popup.show(span);
        if (span.dataset.tokenIndex) {
            scrubTo(parseInt(span.dataset.tokenIndex, 10));
        }
        if (onLockChange) onLockChange(true);
    }

    // Click to lock selection — click again or click outside to unlock
    output.addEventListener('click', (e: MouseEvent) => {
        const target = e.target as HTMLElement;
        if (target.tagName === 'SPAN' && target.dataset.confidence) {
            const span = target as HTMLSpanElement;
            if (lockedSpan === span) {
                unlock();
            } else {
                lockToken(span);
            }
        } else if (!target.closest('.token-popup')) {
            unlock();
        }
    });

    // Hover — only scrub if no token is locked
    output.addEventListener('mouseenter', (e: MouseEvent) => {
        if (lockedSpan) return;
        const target = e.target as HTMLElement;
        if (target.tagName === 'SPAN' && target.dataset.confidence) {
            popup.show(target as HTMLSpanElement);
            if (target.dataset.tokenIndex) {
                scrubTo(parseInt(target.dataset.tokenIndex, 10));
            }
        }
    }, true);
    output.addEventListener('mouseleave', (e: MouseEvent) => {
        if (lockedSpan) return;
        const target = e.target as HTMLElement;
        if (target.tagName === 'SPAN') {
            popup.scheduleHide();
            scrubTo(-1);
        }
    }, true);
}

// ── Public helpers ──────────────────────────────────────────────────

/**
 * Check whether a response glyph received any tokens.
 */
export function getResponseTokenCount(el: HTMLElement): number {
    const output = el.querySelector('.result-glyph-output');
    if (!output) return 0;
    return output.children.length;
}

/**
 * Populate a streaming response glyph with static text
 * (for non-streaming providers that return a full response).
 */
export function populateStaticContent(el: HTMLElement, text: string): void {
    const output = el.querySelector('.result-glyph-output');
    if (!output) return;
    const span = document.createElement('span');
    span.textContent = text;
    output.appendChild(span);
}

/**
 * Update an existing response glyph's content in place.
 */
export function updateResultGlyphContent(resultElement: HTMLElement, result: ExecutionResult): boolean {
    const output = resultElement.querySelector('.result-glyph-output') as HTMLElement | null;
    if (!output) return false;

    renderOutput(output, result);

    const glyphId = resultElement.getAttribute('data-glyph-id');
    if (glyphId) {
        const existing = uiState.getCanvasGlyphs().find(g => g.id === glyphId);
        if (existing) {
            let promptConfig: PromptConfig | undefined;
            try {
                const prev = JSON.parse(existing.content || '{}');
                promptConfig = prev.promptConfig;
            } catch { /* ignore */ }
            const contentPayload: ResultGlyphContent = { result, promptConfig };
            uiState.addCanvasGlyph({ ...existing, content: JSON.stringify(contentPayload) });
        }
    }

    return true;
}

// ── Tray restoration helpers ────────────────────────────────────────

export function buildResultTitleBar(
    execResult: ExecutionResult | null,
    tokens: StreamToken[],
    promptText?: string,
): HTMLElement {
    const header = document.createElement('div');
    header.className = 'glyph-title-bar glyph-title-bar--auto result-glyph-header';

    if (promptText) {
        const promptLabel = document.createElement('span');
        promptLabel.className = 'result-prompt-label';
        promptLabel.style.flex = '1';
        promptLabel.style.whiteSpace = 'pre-wrap';
        promptLabel.style.wordBreak = 'break-word';
        promptLabel.style.padding = '0 8px';
        promptLabel.style.color = 'var(--text-on-dark)';
        promptLabel.style.fontSize = '12px';
        promptLabel.textContent = promptText;
        header.appendChild(promptLabel);
    }

    const buttonContainer = document.createElement('div');
    buttonContainer.style.display = 'flex';
    buttonContainer.style.gap = '3px';
    buttonContainer.style.flexShrink = '0';
    buttonContainer.style.marginLeft = 'auto';

    const copyBtn = document.createElement('button');
    copyBtn.className = 'titlebar-btn';
    copyBtn.textContent = '\u2398';
    copyBtn.title = 'Copy to clipboard';
    copyBtn.addEventListener('click', () => {
        let text = '';
        if (promptText) text += `> ${promptText.split('\n').join('\n> ')}\n\n`;
        if (tokens.length > 0) {
            text += collectText(tokens);
        } else if (execResult) {
            text += execResult.stdout || execResult.error || '(no output)';
        }
        navigator.clipboard.writeText(text).then(() => {
            copyBtn.textContent = '\u2713';
            setTimeout(() => { copyBtn.textContent = '\u2398'; }, 1500);
        });
    });
    buttonContainer.appendChild(copyBtn);

    header.appendChild(buttonContainer);
    return header;
}

export function renderResultContent(
    execResult: ExecutionResult | null,
    tokens: StreamToken[],
    _config?: PromptConfig,
    promptText?: string,
): HTMLElement {
    const wrapper = document.createElement('div');
    wrapper.className = 'glyph-content';

    if (promptText) {
        const promptDiv = document.createElement('div');
        promptDiv.style.padding = '0 0 8px';
        promptDiv.style.borderBottom = '1px solid var(--border-on-dark)';
        promptDiv.style.marginBottom = '8px';
        promptDiv.style.color = 'var(--text-secondary)';
        promptDiv.style.fontSize = '12px';
        promptDiv.textContent = promptText;
        wrapper.appendChild(promptDiv);
    }

    const outputDiv = document.createElement('div');
    outputDiv.style.fontFamily = 'monospace';
    outputDiv.style.fontSize = '12px';
    outputDiv.style.whiteSpace = 'pre-wrap';
    outputDiv.style.wordBreak = 'break-word';

    if (tokens.length > 0) {
        for (let i = 0; i < tokens.length; i++) {
            outputDiv.appendChild(renderToken(tokens[i], i, colorMode));
        }
    } else if (execResult) {
        renderOutput(outputDiv, execResult);
    }

    wrapper.appendChild(outputDiv);
    return wrapper;
}

// ── Stream follow-up execution ──────────────────────────────────────

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

    const responseGlyphId = `result-${crypto.randomUUID()}`;

    uiState.addCanvasGlyph({
        id: responseGlyphId,
        symbol: 'result',
        x: sx,
        y: sy,
        width: Math.round(parentRect.width),
        height: 200,
    });

    const responseGlyph: Glyph = {
        id: responseGlyphId,
        title: 'Result',
        symbol: 'result',
        x: sx,
        y: sy,
        width: Math.round(parentRect.width),
        renderContent: () => document.createElement('div'),
    };

    const responseElement = createResponseGlyph(responseGlyph, undefined, undefined, request.text, responseGlyphId);
    canvas.appendChild(responseElement);

    const parentGlyphId = parentElement.dataset.glyphId;
    if (parentGlyphId) {
        autoMeldResultBelow(
            parentElement, parentGlyphId, 'result',
            'ResponseGlyph', responseElement, responseGlyphId, 'ResponseFollowUp',
        );
    }

    await canvasSyncQueue.flush();

    const body: Record<string, unknown> = {
        template: request.template,
        system_prompt: request.systemPrompt,
        glyph_id: responseGlyphId,
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
                unsubscribeStream(responseGlyphId);
                responseElement.remove();
                uiState.removeCanvasGlyph(responseGlyphId);
                controls.error(`Failed: ${data.error}`);
                return;
            }

            const tokenCount = getResponseTokenCount(responseElement);
            if (tokenCount === 0 && data.response) {
                populateStaticContent(responseElement, data.response);
            }

            log.debug(SEG.GLYPH, `[ResponseGlyph] Follow-up complete, ${tokenCount} streamed tokens`);
        })
        .catch((err) => {
            unsubscribeStream(request.glyphId);
            responseElement.remove();
            uiState.removeCanvasGlyph(responseGlyphId);

            const errMsg = err instanceof Error ? err.message : String(err);
            controls.error(`Failed: ${errMsg}`);
            log.error(SEG.GLYPH, `[ResponseGlyph] Follow-up failed for ${parentGlyph.id}: ${errMsg}`);
        });
}

