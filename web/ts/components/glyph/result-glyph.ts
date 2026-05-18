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
 * ECM: Entropy drives hue (orange→purple), confidence drives alpha. Data-driven.
 * DIM(#749): Token labels dim based on nebula brightness behind them when a token is selected.
 * TODO(CLR/#750): Use embedding dimensions (PCA 4-6) to drive particle hue — semantic color.
 */

import type { Glyph } from '@qntx/glyphs';
import type { LLMStreamMessage } from '../../../types/websocket';
import type { LLMTokenSignal, SamplerStageSignal } from '@generated/server';
import { log, SEG } from '../../logger';
import { canvasPlaced } from '@qntx/glyphs';
import { unmeldComposition, makeDraggable, storeCleanup, preventDrag, wireExpandToWindow } from '@qntx/glyphs';
import { autoMeldResultBelow } from './meld/auto-meld-result';
import { uiState } from '../../state/ui';
import { registerHandler, unregisterHandler } from '../../websocket';
import { apiFetch, getBackendUrl } from '../../api';
import { canvasSyncQueue } from '../../api/canvas-sync';
import { createFollowUpZone, type FollowUpRequest, type FollowUpControls } from './glyph-followup';
import { createTokenPopup, samplerInfluenceColor } from './token-popup';
import { createNebulaNav, type NebulaNavHandle } from './nebula-nav';
import { el } from '../../html-utils';

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
        log.debug(SEG.GLYPH, `[ResultGlyph] No subscriber for job_id ${msg.job_id}`);
    }
}

export function subscribeStream(jobId: string, callback: StreamCallback): void {
    if (subscribers.size === 0) {
        registerHandler('llm_stream', dispatch);
        log.debug(SEG.GLYPH, '[ResultGlyph] Registered llm_stream handler');
    }
    subscribers.set(jobId, callback);
}

export function unsubscribeStream(jobId: string): void {
    subscribers.delete(jobId);
    if (subscribers.size === 0) {
        unregisterHandler('llm_stream');
        log.debug(SEG.GLYPH, '[ResultGlyph] Unregistered llm_stream handler');
    }
}

function renderStatsLine(container: HTMLElement, stats: GenerationStats): void {
    const parts: string[] = [];
    if (stats.model) parts.push(stats.model);
    parts.push(`${stats.tokenCount} tokens`);
    if (stats.tokPerSec) parts.push(`${stats.tokPerSec.toFixed(1)} tok/s`);
    const statsEl = el('div', {
        class: 'result-glyph-stats',
        text: parts.join(' · '),
        style: { fontSize: '10px', color: 'var(--text-muted)', padding: '4px 8px 2px', textAlign: 'right', opacity: '0.6' },
    });
    container.appendChild(statsEl);
}

// ── Signal → Color ──────────────────────────────────────────────────

/**
 * Map confidence + entropy to a CSS background-color.
 * Alpha: confidence drives visibility (low confidence = more opaque).
 * Hue: entropy drives color (low entropy = orange "close race",
 *       high entropy = purple "lost in noise").
 */
function signalToColor(confidence: number, entropy: number): string {
    const c = Math.max(0, Math.min(1, confidence));
    const alpha = Math.min(0.55, (1 - c) * 0.65);
    if (alpha < 0.02) return 'transparent';
    // Calibrated from 13K tokens (ATS attestations, April 2026).
    // P50 entropy = 1.67, range 1.5 covers up to ~3.2 bits.
    // May need recalibration for different models or prompt styles.
    const ent = Math.max(0, Math.min(1, (entropy - 1.67) / 1.5));
    const hue = 30 - ent * 110;
    return `hsla(${hue.toFixed(0)}, 100%, 50%, ${alpha.toFixed(3)})`;
}


interface StreamInstance {
    output: HTMLElement;
    tokens: StreamToken[];
    visible: boolean;
}

const instances = new Set<StreamInstance>();

type ColorMode = 'confidence' | 'sampler' | 'none';
let colorMode: ColorMode = 'confidence';

export function toggleColorMode(): ColorMode {
    const cycle: ColorMode[] = ['confidence', 'sampler', 'none'];
    colorMode = cycle[(cycle.indexOf(colorMode) + 1) % cycle.length];
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
    if (!token.signal || mode === 'none') return 'transparent';
    if (mode === 'sampler' && token.signal.sampler_stages) {
        const color = samplerInfluenceColor(token.signal.sampler_stages as SamplerStageSignal[]);
        if (color !== 'transparent') return color;
    }
    return signalToColor(token.signal.confidence, token.signal.entropy);
}

function renderToken(token: StreamToken, tokenIndex: number, mode: ColorMode): HTMLSpanElement {
    const span = el('span', { text: token.text });
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
        const stderrSpan = el('span', {
            text: result.stderr,
            style: { color: 'var(--glyph-status-error-text)' },
        });
        container.appendChild(document.createTextNode(outputText));
        container.appendChild(stderrSpan);
        outputText = '';
    }

    if (result.error) {
        const errorSpan = el('span', {
            text: `\nError: ${result.error}`,
            style: { color: 'var(--glyph-status-error-text)', fontWeight: 'bold' },
        });
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
export function createResultGlyph(
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

    const header = el('div', { class: 'glyph-title-bar glyph-title-bar--auto result-glyph-header' });

    if (prompt) {
        const promptLabel = el('span', {
            class: 'result-prompt-label',
            text: prompt,
            style: {
                flex: '1', whiteSpace: 'pre-wrap', wordBreak: 'break-word',
                padding: '0 8px', color: 'var(--text-on-dark)', fontSize: '12px',
            },
        });
        header.appendChild(promptLabel);
    }

    const buttonContainer = el('div', {
        style: { display: 'flex', gap: '3px', flexShrink: '0', marginLeft: 'auto' },
    });

    function headerBtn(label: string, title: string): HTMLButtonElement {
        const btn = el('button', { class: 'titlebar-btn', text: label });
        btn.title = title;
        return btn;
    }

    // Color toggle — visible when tokens are present
    const colorBtn = headerBtn('◐', 'Coloring: signal');
    colorBtn.style.display = 'none';
    colorBtn.addEventListener('click', () => {
        const mode = toggleColorMode();
        const labels: Record<ColorMode, string> = {
            confidence: 'Coloring: signal',
            sampler: 'Coloring: sampler influence',
            none: 'Coloring: off',
        };
        colorBtn.title = labels[mode];
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

    // Expand to window — hidden during streaming (wired after element is created below)
    const toWindowBtn = headerBtn('⬆', 'Expand to window');
    if (isStreaming) toWindowBtn.style.display = 'none';
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
                log.debug(SEG.GLYPH, `[ResultGlyph] Unmelded composition before closing ${glyph.id}`);
            }
        }

        element.remove();
        uiState.removeCanvasGlyph(glyph.id);
        log.debug(SEG.GLYPH, `[ResultGlyph] Closed ${glyph.id}`);
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
        logLabel: 'ResultGlyph',
    });
    element.style.minHeight = '80px';
    element.style.borderRadius = '0 0 2px 2px';
    element.style.border = '1px solid var(--border-on-dark)';
    element.style.borderTop = 'none';
    element.style.zIndex = '1';
    header.style.position = 'relative';
    header.style.zIndex = '1';
    element.appendChild(header);

    // Wire expand-to-window (deferred until element exists)
    wireExpandToWindow({
        element,
        expandBtn: toWindowBtn,
        glyphId: glyph.id,
        title: prompt || 'Result',
        symbol: 'result',
        renderContent: () => renderResultContent(result ?? null, tokens, promptConfig, prompt),
        logLabel: 'ResultGlyph',
        adoptExtras: { renderTitleBar: () => buildResultTitleBar(result ?? null, tokens, prompt) },
        onRestoreToCanvas: (el) => {
            const contentStr = uiState.getCanvasGlyph(glyph.id)?.content;
            uiState.addCanvasGlyph({
                id: glyph.id,
                symbol: 'result',
                x: parseInt(el.style.left),
                y: parseInt(el.style.top),
                width: parseInt(el.style.width),
                height: parseInt(el.style.height),
                content: contentStr,
            });
        },
    });

    // ── Output container ────────────────────────────────────────────

    const output = el('div', {
        class: 'result-glyph-output glyph-content-area',
        style: {
            position: 'relative', zIndex: '1', fontFamily: 'monospace', fontSize: '12px',
            whiteSpace: 'pre-wrap', wordBreak: 'break-word', overflowWrap: 'break-word',
            backgroundColor: 'transparent', color: 'var(--text-on-dark)',
        },
    });
    preventDrag(output); // token clicks must not trigger composition drag
    element.appendChild(output);

    // ── Nebula canvas ──────────────────────────────────────────────
    // Small inline particle view from the Metal renderer. Connects to

    // Nebula renders behind the text as the glyph's background
    const nebulaCanvas = el('canvas', {
        style: {
            position: 'absolute', top: '0', left: '0', width: '100%', height: '100%',
            pointerEvents: 'none', zIndex: '0', display: 'none',
        },
    });
    const nebulaCtx = nebulaCanvas.getContext('2d');

    const nebulaStatus = el('div', {
        style: {
            position: 'absolute', bottom: '2px', right: '6px',
            font: '9px monospace', color: 'rgba(255,255,255,0.25)', zIndex: '1',
        },
    });

    let lastNebulaBitmap: ImageBitmap | null = null;

    // Resize canvas to match element pixel density, redraw last frame
    function fitNebulaCanvas(): void {
        if (!element.isConnected) return;
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
        nebulaWs = new WebSocket(`${base}/ws/scry`);

        nebulaWs.onopen = () => {
            if (nebulaLive) nebulaStatus.textContent = 'connected';
            log.debug(SEG.GLYPH, '[ResultGlyph] Nebula WebSocket connected');
        };

        nebulaWs.onmessage = (event: MessageEvent) => {
            if (!nebulaLive && !nebulaScrub) return; // no active stream or scrub — ignore
            try {
                const msg = JSON.parse(event.data);
                if (msg.type !== 1 || !msg.data) return;

                const decoded = atob(msg.data);

                // Pick response: picked:tokenId,tokenText
                if (decoded.indexOf('picked:') === 0) {
                    if (nebulaNav) nebulaNav.handlePickResponse(decoded.substring(7));
                    return;
                }

                // Fork responses
                if (decoded.indexOf('forked:') === 0) {
                    const branchId = parseInt(decoded.substring(7), 10);
                    log.debug(SEG.GLYPH, `[ResultGlyph] Fork started: branch ${branchId}`);
                    // TODO: no visual link between fork point and fork text in the DOM
                    const forkMarker = el('div', {
                        class: 'fork-marker',
                        text: '\u2500 fork ' + branchId,
                        style: {
                            borderTop: '1px solid rgba(100,180,255,0.4)', margin: '6px 0 4px',
                            padding: '2px 0 0', font: '9px monospace', color: 'rgba(100,180,255,0.5)',
                        },
                    });
                    output.appendChild(forkMarker);
                    output.scrollTop = output.scrollHeight;
                    // Enable frame drawing for fork generation
                    nebulaScrub = true;
                    return;
                }
                if (decoded.indexOf('fork_token:') === 0) {
                    // TODO: fork spans lack data-token-index/data-confidence — not scrub-navigable or dimmable
                    const tokenText = decoded.substring(11);
                    const span = el('span', {
                        text: tokenText,
                        style: { color: 'rgba(140,190,255,0.9)' },
                    });
                    output.appendChild(span);
                    output.scrollTop = output.scrollHeight;
                    return;
                }
                if (decoded.indexOf('fork_done:') === 0) {
                    const branchId = parseInt(decoded.substring(10), 10);
                    log.debug(SEG.GLYPH, `[ResultGlyph] Fork complete: branch ${branchId}`);
                    return;
                }

                const bytes = new Uint8Array(decoded.length);
                for (let i = 0; i < decoded.length; i++) {
                    bytes[i] = decoded.charCodeAt(i);
                }
                const blob = new Blob([bytes], { type: 'image/png' });
                lastNebulaBase64 = msg.data; // store raw base64 for persistence
                createImageBitmap(blob).then((bmp) => {
                    if (!nebulaCtx) return;
                    if (lastNebulaBitmap) lastNebulaBitmap.close();
                    lastNebulaBitmap = bmp;
                    nebulaCtx.clearRect(0, 0, nebulaCanvas.width, nebulaCanvas.height);
                    nebulaCtx.drawImage(bmp, 0, 0, nebulaCanvas.width, nebulaCanvas.height);
                    if (nebulaScrub && nebulaNav) nebulaNav.applyDim();
                    nebulaFpsFrames++;
                    const now = performance.now();
                    if (now - nebulaFpsLast >= 1000) {
                        nebulaStatus.textContent = Math.round(nebulaFpsFrames * 1000 / (now - nebulaFpsLast)) + ' fps';
                        nebulaFpsFrames = 0;
                        nebulaFpsLast = now;
                    }
                });
            } catch (e) {
                log.error(SEG.GLYPH, '[ResultGlyph] Nebula frame error', e);
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

    element.insertBefore(nebulaCanvas, element.firstChild);
    element.appendChild(nebulaStatus);
    nebulaRo.observe(element);

    // ── Nebula navigation (camera, pick, dim) ─────────────────────
    let nebulaNav: NebulaNavHandle | null = null;
    if (nebulaCtx) {
        nebulaNav = createNebulaNav({
            element, output, canvas: nebulaCanvas, ctx: nebulaCtx,
            sendMessage: sendNebulaMessage,
        });
    }

    // ── Restore saved content ───────────────────────────────────────

    const saved = uiState.getCanvasGlyph(glyph.id);
    if (saved?.content && !result && !streamJobId) {
        try {
            const content = JSON.parse(saved.content) as ResultGlyphContent;
            if (content.tokens && content.tokens.length > 0) {
                // Restore from saved token data
                for (const token of content.tokens) tokens.push(token);
                streamModel = content.model;
                savedStats = content.stats;
                if (content.prompt && !prompt) prompt = content.prompt;
                log.debug(SEG.GLYPH, `[ResultGlyph] Restored ${tokens.length} tokens for ${glyph.id}`);
            } else if (content.result) {
                // Restore from saved execution result
                result = content.result;
                promptConfig = content.promptConfig;
                if (content.prompt && !prompt) prompt = content.prompt;
            }
        } catch (e) {
            log.error(SEG.GLYPH, `[ResultGlyph] Failed to parse saved content for ${glyph.id}:`, e);
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
        const nav = setupTokenPopup(output, popup, (idx) => sendNebulaMessage('scrub:' + idx), (locked, span) => { if (nebulaNav) { nebulaNav.setNavActive(locked); nebulaNav.setSelectedSpan(span); } element.style.cursor = locked ? 'none' : ''; if (!locked && nebulaNav) { nebulaNav.clearDim(); sendNebulaMessage('mouse:-1,-1'); } }, (focused) => { if (nebulaNav) { nebulaNav.setExamine(focused); sendNebulaMessage('examine:' + (focused ? '1' : '0')); if (focused) nebulaNav.applyDim(); else nebulaNav.applyDim(); } });
        if (nebulaNav) nebulaNav.setTokenNav(nav);
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
            const nav = setupTokenPopup(output, popup, (idx) => sendNebulaMessage('scrub:' + idx), (locked, span) => { if (nebulaNav) { nebulaNav.setNavActive(locked); nebulaNav.setSelectedSpan(span); } element.style.cursor = locked ? 'none' : ''; if (!locked && nebulaNav) { nebulaNav.clearDim(); sendNebulaMessage('mouse:-1,-1'); } }, (focused) => { if (nebulaNav) { nebulaNav.setExamine(focused); sendNebulaMessage('examine:' + (focused ? '1' : '0')); if (focused) nebulaNav.applyDim(); else nebulaNav.applyDim(); } });
        if (nebulaNav) nebulaNav.setTokenNav(nav);
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
                log.debug(SEG.GLYPH, `[ResultGlyph] Stream complete for ${streamJobId}, ${tokens.length} tokens`);
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
        logLabel: 'ResultGlyph',
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
        if (nebulaNav) nebulaNav.destroy();
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
        const existing = uiState.getCanvasGlyph(glyph.id);
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
    onLockChange?: (locked: boolean, span: HTMLElement | null) => void,
    onExamineChange?: (focused: boolean) => void,
): { unlock: () => void; navigate: (dir: number) => void } {
    let lockedSpan: HTMLSpanElement | null = null;
    let focused = false;  // red = focus mode (single keyframe isolation)

    function scrubTo(idx: number): void {
        if (onScrub) onScrub(idx);
        document.dispatchEvent(new CustomEvent('nebula-scrub', { detail: { index: idx } }));
    }

    function unlock(): void {
        if (focused) {
            focused = false;
            if (onExamineChange) onExamineChange(false);
        }
        if (lockedSpan) {
            lockedSpan.style.outline = '';
            lockedSpan.style.boxShadow = '';
            lockedSpan = null;
            scrubTo(-1);
            if (onLockChange) onLockChange(false, null);
        }
    }

    // Navigate to adjacent token span. Preserves orange/red mode.
    function navigate(direction: number): void {
        if (!lockedSpan) return;
        const spans = output.querySelectorAll('span[data-confidence]');
        let idx = -1;
        for (let i = 0; i < spans.length; i++) {
            if (spans[i] === lockedSpan) { idx = i; break; }
        }
        if (idx < 0) return;
        const next = idx + direction;
        if (next < 0 || next >= spans.length) return;
        const wasFocused = focused;
        lockToken(spans[next] as HTMLSpanElement);
        if (wasFocused) examineToken();
    }

    function lockToken(span: HTMLSpanElement): void {
        if (lockedSpan) {
            lockedSpan.style.outline = '';
            lockedSpan.style.boxShadow = '';
        }
        focused = false;
        if (onExamineChange) onExamineChange(false);
        lockedSpan = span;
        lockedSpan.style.outline = '1px solid rgba(255, 160, 40, 0.8)';
        lockedSpan.style.boxShadow = 'none';
        popup.show(span);
        if (span.dataset.tokenIndex) {
            scrubTo(parseInt(span.dataset.tokenIndex, 10));
        }
        if (onLockChange) onLockChange(true, span);
    }

    function examineToken(): void {
        if (!lockedSpan) return;
        focused = true;
        lockedSpan.style.outline = '1px solid rgba(240, 50, 50, 0.85)';
        lockedSpan.style.boxShadow = 'none';
        if (onExamineChange) onExamineChange(true);
    }

    // Click: span → lock (orange). Same span again → focus (red). Again → unlock.
    output.addEventListener('click', (e: MouseEvent) => {
        const target = e.target as HTMLElement;
        if (target.tagName === 'SPAN' && target.dataset.confidence) {
            const span = target as HTMLSpanElement;
            if (lockedSpan === span) {
                if (focused) {
                    unlock();
                } else {
                    examineToken();
                }
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

    return { unlock, navigate };
}

// ── Public helpers ──────────────────────────────────────────────────

/**
 * Check whether a response glyph received any tokens.
 */
export function getResponseTokenCount(glyphEl: HTMLElement): number {
    const output = glyphEl.querySelector('.result-glyph-output');
    if (!output) return 0;
    return output.children.length;
}

/**
 * Populate a streaming response glyph with static text
 * (for non-streaming providers that return a full response).
 */
export function populateStaticContent(glyphEl: HTMLElement, text: string): void {
    const output = glyphEl.querySelector('.result-glyph-output');
    if (!output) return;
    output.appendChild(el('span', { text }));
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
        const existing = uiState.getCanvasGlyph(glyphId);
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
    const header = el('div', { class: 'glyph-title-bar glyph-title-bar--auto result-glyph-header' });

    if (promptText) {
        const promptLabel = el('span', {
            class: 'result-prompt-label',
            text: promptText,
            style: {
                flex: '1', whiteSpace: 'pre-wrap', wordBreak: 'break-word',
                padding: '0 8px', color: 'var(--text-on-dark)', fontSize: '12px',
            },
        });
        header.appendChild(promptLabel);
    }

    const buttonContainer = el('div', {
        style: { display: 'flex', gap: '3px', flexShrink: '0', marginLeft: 'auto' },
    });

    const copyBtn = el('button', { class: 'titlebar-btn', text: '\u2398' });
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
    const wrapper = el('div', { class: 'glyph-content' });

    if (promptText) {
        const promptDiv = el('div', {
            text: promptText,
            style: {
                padding: '0 0 8px', borderBottom: '1px solid var(--border-on-dark)',
                marginBottom: '8px', color: 'var(--text-secondary)', fontSize: '12px',
            },
        });
        wrapper.appendChild(promptDiv);
    }

    const outputDiv = el('div', {
        style: { fontFamily: 'monospace', fontSize: '12px', whiteSpace: 'pre-wrap', wordBreak: 'break-word' },
    });

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
        renderContent: () => el('div'),
    };

    const responseElement = createResultGlyph(responseGlyph, undefined, undefined, request.text, responseGlyphId);
    canvas.appendChild(responseElement);

    const parentGlyphId = parentElement.dataset.glyphId;
    if (parentGlyphId) {
        autoMeldResultBelow(
            parentElement, parentGlyphId, 'result',
            'ResultGlyph', responseElement, responseGlyphId, 'ResponseFollowUp',
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

            log.debug(SEG.GLYPH, `[ResultGlyph] Follow-up complete, ${tokenCount} streamed tokens`);
        })
        .catch((err) => {
            unsubscribeStream(request.glyphId);
            responseElement.remove();
            uiState.removeCanvasGlyph(responseGlyphId);

            const errMsg = err instanceof Error ? err.message : String(err);
            controls.error(`Failed: ${errMsg}`);
            log.error(SEG.GLYPH, `[ResultGlyph] Follow-up failed for ${parentGlyph.id}: ${errMsg}`);
        });
}

