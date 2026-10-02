// Transcript Element — one session Ground recorded, read as what was said and done.
// "It's really the Svelte UI part that I want to rehome into QNTX"
// The node derives it (transcripts read); this draws what it answers.

// "Transcripts in Transcript / List of sessions in ground"
// "It opens as window, but can be placed onto the canvas"

import type { Element } from '@teranos/elements';
import {
    tray, canvasPlaced, wireExpandToWindow, preventDrag, createSymbolSpan, settleSymbolSpan,
    getForm, setForm, teardownWindowDrag, removeWindowControls, makeDraggable, makeResizable, storeCleanup, createCorner,
} from '@teranos/elements';
import { apiJson } from '../../client';
import { escapeHtml } from '../../html-utils';
import { log, SEG } from '../../logger';
import { Transcript as TranscriptSym } from '../../sym';
import { spawnAttestationAsWindow } from './attestation-element';
import { screenToCanvas } from './canvas/canvas-pan';
import { uiState } from '../../state/ui';
import type { Attestation } from '../../generated/proto/plugin/grpc/protocol/atsstore';

// One thing said or done in a session, naming the attestation it was read from (protocol.Turn).
export interface Turn {
    at: string;
    speaker: string;
    text: string;
    of: string;
}

// One session as transcripts read answers it (protocol.Transcript).
export interface TranscriptRead {
    session: string;
    subjects: string[];
    started: string;
    ended: string;
    turns: Turn[];
    folded: number;
}

// The node answers at most this many rows to one ask.
const MOST = 1000;

// ─── Time spacers ─────────────────────────────────────────────

// Loom's spacers: the first three blocks are 18px and an hour each; from the
// fourth, a block is 1px smaller and covers twice the time, until 5px.
export function timeSpacers(prevMs: number, curMs: number): number[] {
    if (prevMs === 0) return [];
    let remaining = Math.max(0, curMs - prevMs) / (60 * 60 * 1000);
    const out: number[] = [];
    let block = 1;
    let blockHours = 1;
    while (remaining >= blockHours) {
        const px = block <= 3 ? 18 : 18 - (block - 3);
        if (px <= 5) break;
        out.push(px);
        remaining -= blockHours;
        block++;
        if (block > 3) blockHours *= 2;
    }
    return out;
}

// ─── Turns ────────────────────────────────────────────────────

function between(text: string, mark: string, open: string, close: string): string {
    let out = '';
    let pos = 0;
    while (pos < text.length) {
        const start = text.indexOf(mark, pos);
        if (start < 0) { out += text.substring(pos); break; }
        const end = text.indexOf(mark, start + mark.length);
        if (end < 0) { out += text.substring(pos); break; }
        out += text.substring(pos, start) + open + text.substring(start + mark.length, end) + close;
        pos = end + mark.length;
    }
    return out;
}

// Loom's minimal markdown for what the assistant said: code blocks, bold, inline code.
export function renderAssistant(text: string): string {
    let out = '';
    let inCode = false;
    for (const line of text.split('\n')) {
        if (line.startsWith('```')) {
            out += inCode ? '</code></pre>' : '<pre class="tr-code"><code>';
            inCode = !inCode;
            continue;
        }
        if (inCode) { out += escapeHtml(line) + '\n'; continue; }
        out += between(between(escapeHtml(line), '**', '<b>', '</b>'), '`', '<code class="tr-inline-code">', '</code>') + '\n';
    }
    if (inCode) out += '</code></pre>';
    return out;
}

const SMALL = new Set(['tool', 'edit', 'read', 'search', 'write']);
const MARKER = new Set(['session', 'compaction', 'agent', 'task', 'rite']);

function weightOf(speaker: string): string {
    if (SMALL.has(speaker)) return 'small';
    if (MARKER.has(speaker)) return 'marker';
    return speaker;
}

export function when(at: string): string {
    const d = new Date(Date.parse(at));
    return d.toLocaleString('en', { month: 'short' }) + ' ' + d.getDate() + ' ' +
        String(d.getHours()).padStart(2, '0') + ':' + String(d.getMinutes()).padStart(2, '0');
}

function turnRow(turn: Turn, onOpen: (turn: Turn) => void): HTMLElement {
    const row = document.createElement('div');
    row.className = `tr-turn tr-${weightOf(turn.speaker)} tr-sp-${turn.speaker}`;
    row.title = turn.of;
    row.dataset.of = turn.of;
    const speaker = document.createElement('span');
    speaker.className = 'tr-speaker';
    speaker.textContent = `[${turn.speaker}]`;
    const text = document.createElement('span');
    text.className = 'tr-text';
    if (turn.speaker === 'assistant') text.innerHTML = renderAssistant(turn.text);
    else text.textContent = turn.text;
    row.append(speaker, text);
    row.addEventListener('click', (e) => {
        e.stopPropagation();
        onOpen(turn);
    });
    return row;
}

// ─── Warp ─────────────────────────────────────────────────────

const GAP_MS = 12 * 60 * 60 * 1000;
const GAP_WEIGHT = 3;

// Loom's warp, as the transcript's own scrollbar: one segment per turn, a gap
// past twelve hours weighs three, the wheel zooms, a press or a drag scrolls.
export function warpFor(turns: Turn[], column: HTMLElement): HTMLElement {
    const warp = document.createElement('div');
    warp.className = 'tr-warp';
    const lanes = document.createElement('div');
    lanes.className = 'tr-warp-lanes';
    warp.appendChild(lanes);

    const items: Array<{ weight: number; turn?: Turn }> = [];
    for (let i = 0; i < turns.length; i++) {
        if (i > 0 && Date.parse(turns[i].at) - Date.parse(turns[i - 1].at) > GAP_MS) items.push({ weight: GAP_WEIGHT });
        items.push({ weight: 1, turn: turns[i] });
    }
    const total = items.reduce((s, i) => s + i.weight, 0) || 1;
    for (const item of items) {
        const seg = document.createElement('div');
        seg.style.height = `${(item.weight / total) * 100}%`;
        if (!item.turn) {
            seg.className = 'tr-warp-seg tr-warp-gap';
        } else {
            const seam = item.turn.speaker === 'session' || item.turn.speaker === 'compaction' ? ` tr-seam-${item.turn.speaker}` : '';
            seg.className = `tr-warp-seg tr-sp-${item.turn.speaker}${seam}`;
            if (item.turn.speaker === 'tool') seg.textContent = '◆';
            if (item.turn.speaker === 'ground') seg.textContent = '●';
        }
        lanes.appendChild(seg);
    }

    const view = document.createElement('div');
    view.className = 'tr-warp-view';
    warp.appendChild(view);

    let zoom = 1;
    let scrollFraction = 0;
    let viewHeight = 100;
    const laneTranslate = () => ((50 - viewHeight / 2) - scrollFraction * (zoom * 100 - viewHeight)) / zoom;
    const draw = () => {
        viewHeight = (column.clientHeight / (column.scrollHeight || 1)) * 100;
        scrollFraction = column.scrollTop / (column.scrollHeight - column.clientHeight || 1);
        lanes.style.height = `${zoom * 100}%`;
        lanes.style.transform = `translateY(${laneTranslate()}%)`;
        view.style.display = viewHeight < 100 ? '' : 'none';
        view.style.top = `${50 - (viewHeight * zoom) / 2}%`;
        view.style.height = `${viewHeight * zoom}%`;
    };
    const scrollTo = (e: PointerEvent, smooth: boolean) => {
        const rect = warp.getBoundingClientRect();
        const clickFrac = (e.clientY - rect.top) / (rect.height || 1);
        const contentFrac = (clickFrac - (laneTranslate() / 100) * zoom) / zoom;
        const target = Math.max(0, Math.min(1, contentFrac)) * (column.scrollHeight - column.clientHeight);
        column.scrollTo({ top: target, behavior: smooth ? 'smooth' : 'instant' });
    };
    let dragging = false;
    warp.addEventListener('pointerdown', (e) => {
        dragging = true;
        warp.setPointerCapture?.(e.pointerId);
        scrollTo(e, true);
    });
    warp.addEventListener('pointermove', (e) => { if (dragging) scrollTo(e, false); });
    warp.addEventListener('pointerup', () => { dragging = false; });
    warp.addEventListener('wheel', (e) => {
        if (Math.abs(e.deltaX) > Math.abs(e.deltaY)) return;
        e.preventDefault();
        zoom = Math.max(1, Math.min(10, zoom + (e.deltaY > 0 ? 0.3 : -0.3)));
        draw();
    }, { passive: false });
    column.addEventListener('scroll', draw);
    requestAnimationFrame(draw);
    return warp;
}

// ─── Views ────────────────────────────────────────────────────

/** One session drawn into body: its turns in order, the gaps between them, and the warp. */
export function renderTranscript(body: HTMLElement, read: TranscriptRead, onOpen: (turn: Turn) => void): void {
    body.replaceChildren();
    const head = document.createElement('div');
    head.className = 'tr-head';
    head.textContent = `${read.session.substring(0, 8)}  ${read.turns.length > 0 ? when(read.turns[0].at) : ''}  ${read.turns.length}t`;
    const rows = document.createElement('div');
    rows.className = 'tr-body';
    const column = document.createElement('div');
    column.className = 'tr-col';
    let prev = 0;
    for (const turn of read.turns) {
        const at = Date.parse(turn.at);
        for (const px of timeSpacers(prev, at)) {
            const spacer = document.createElement('div');
            spacer.className = 'tr-spacer';
            spacer.style.height = `${px}px`;
            column.appendChild(spacer);
        }
        prev = at;
        column.appendChild(turnRow(turn, onOpen));
    }
    rows.append(column, warpFor(read.turns, column));
    body.append(head, rows);
}

// ─── Element ──────────────────────────────────────────────────

function said(body: HTMLElement, text: string): void {
    const line = document.createElement('div');
    line.className = 'tr-said';
    line.textContent = text;
    body.replaceChildren(line);
}

// A turn names the attestation it was read from; pressing it opens that one.
async function openTurn(session: string, turn: Turn): Promise<void> {
    const held = await apiJson<Attestation[]>(`/api/attestations?context=${encodeURIComponent('session:' + session)}&limit=${MOST}`);
    const as = held.find(a => a.id === turn.of);
    if (!as) {
        log.error(SEG.ELEMENT, `[Transcript] turn ${turn.of} names an attestation session ${session} no longer answers`);
        return;
    }
    spawnAttestationAsWindow(as);
}

const SIZE = { width: 520, height: 640 };

// Reads one session into body, or says why it could not.
function readInto(body: HTMLElement, session: string): void {
    said(body, 'Reading session ' + session + '…');
    apiJson<{ transcripts: TranscriptRead[] }>(`/api/transcripts?session=${encodeURIComponent(session)}`)
        .then(answer => {
            const read = answer.transcripts[0];
            if (!read) { said(body, 'No session ' + session + ' is in this namespace.'); return; }
            renderTranscript(body, read, (turn) => {
                openTurn(session, turn).catch((err: unknown) => log.error(SEG.ELEMENT, `[Transcript] turn ${turn.of} did not open:`, err));
            });
        })
        .catch((err: unknown) => said(body, 'Could not read session ' + session + ': ' + String(err)));
}

function transcriptBody(session: string): HTMLElement {
    const body = document.createElement('div');
    body.className = 'content-area tr';
    // A turn is pressed, not dragged: the title bar is what moves the element.
    preventDrag(body);
    readInto(body, session);
    return body;
}

function titleOf(session: string): string {
    return `Transcript ${session.substring(0, 8)}`;
}

/** A Transcript placed on the canvas: the session it holds is its content. */
export function createTranscriptElement(item: Element): HTMLElement {
    const session = item.content ?? '';

    const titleBar = document.createElement('div');
    titleBar.className = 'title-bar';
    const symbol = item.symbolElement ? settleSymbolSpan(item.symbolElement) : createSymbolSpan(TranscriptSym);
    const label = document.createElement('span');
    label.textContent = titleOf(session);
    const expandBtn = document.createElement('button');
    expandBtn.className = 'titlebar-btn';
    expandBtn.textContent = '⬆';
    expandBtn.title = 'Expand to window';
    expandBtn.style.marginLeft = 'auto';
    preventDrag(expandBtn);
    titleBar.append(symbol, label, expandBtn);

    const { element } = canvasPlaced({
        item,
        className: 'canvas-transcript-element',
        defaults: { x: 200, y: 200, ...SIZE },
        resizable: true,
        logLabel: 'Transcript',
    });
    element.appendChild(titleBar);
    const body = transcriptBody(session);
    element.appendChild(body);

    wireExpandToWindow({
        element,
        expandBtn,
        elementId: item.id,
        title: titleOf(session),
        symbol: TranscriptSym,
        renderContent: () => body,
        logLabel: 'Transcript',
    });

    return element;
}

/** Opens one session as a Transcript window, which can be placed onto the canvas. Called from Ground. */
export function openTranscriptElement(session: string): void {
    const itemId = `transcript-${session}`;
    if (tray.has(itemId)) {
        tray.open(itemId);
        return;
    }
    if (document.querySelector(`[data-element-id="${itemId}"]`)) {
        log.debug(SEG.ELEMENT, `[Transcript] session ${session} is already open as ${itemId}`);
        return;
    }

    tray.add({
        id: itemId,
        title: titleOf(session),
        symbol: TranscriptSym,
        onClose: () => { tray.remove(itemId); },
        renderTitleBar: () => windowTitleBar(session, itemId),
        renderContent: () => {
            const body = transcriptBody(session);
            // A window is what its content measures; the canvas sizes a placed one.
            body.classList.add('tr-natural');
            return body;
        },
    } satisfies Element);

    tray.open(itemId);
}

function windowTitleBar(session: string, itemId: string): HTMLElement {
    const titleBar = document.createElement('div');
    titleBar.className = 'title-bar';
    const label = document.createElement('span');
    label.textContent = titleOf(session);
    const placeBtn = document.createElement('button');
    placeBtn.className = 'titlebar-btn';
    placeBtn.textContent = '⬇';
    placeBtn.title = 'Place on canvas';
    placeBtn.style.marginLeft = 'auto';
    preventDrag(placeBtn);
    titleBar.append(createSymbolSpan(TranscriptSym), label, placeBtn);

    placeBtn.addEventListener('click', (e) => {
        // The tray's own press on the element would open it again.
        e.stopPropagation();
        const element = placeBtn.closest('[data-element-id]') as HTMLElement | null;
        if (!element) return;
        placeOnCanvas(element, session, itemId, placeBtn);
    });
    return titleBar;
}

// The window becomes the canvas element: the same DOM element, reparented
// into the canvas, the way an attestation window is placed.
function placeOnCanvas(element: HTMLElement, session: string, itemId: string, placeBtn: HTMLElement): void {
    const form = getForm(element);
    if (form !== 'window' && form !== 'canvasExpanded') return;

    const canvasEl = document.querySelector('.canvas-workspace') as HTMLElement | null;
    if (!canvasEl) {
        log.warn(SEG.ELEMENT, `[Transcript] no canvas workspace to place ${itemId} on`);
        return;
    }
    const canvasId = canvasEl.dataset.canvasId ?? 'canvas-workspace';
    const contentLayer = canvasEl.querySelector('.canvas-content-layer') as HTMLElement | null;
    if (!contentLayer) {
        log.warn(SEG.ELEMENT, `[Transcript] canvas ${canvasId} has no content layer to place ${itemId} in`);
        return;
    }

    const windowRect = element.getBoundingClientRect();
    const canvasRect = canvasEl.getBoundingClientRect();
    const at = screenToCanvas(canvasId, windowRect.left - canvasRect.left, windowRect.top - canvasRect.top);
    const x = Math.round(at.x);
    const y = Math.round(at.y);

    teardownWindowDrag(element);
    const resizeObserver = (element as any).__resizeObserver as ResizeObserver | undefined;
    if (resizeObserver) {
        resizeObserver.disconnect();
        delete (element as any).__resizeObserver;
    }
    const titleBar = element.querySelector('.title-bar') as HTMLElement | null;
    if (titleBar) removeWindowControls(titleBar);
    const contentDiv = element.querySelector('.canvas-window-content');
    if (contentDiv) {
        while (contentDiv.firstChild) element.appendChild(contentDiv.firstChild);
        contentDiv.remove();
    }

    setForm(element, 'canvasPlaced');
    element.remove();
    element.style.cssText = '';
    // Detached first, so the tray letting go of it does not remove it again.
    if (tray.has(itemId)) tray.remove(itemId);

    element.style.position = 'absolute';
    element.style.left = `${x}px`;
    element.style.top = `${y}px`;
    element.style.width = `${SIZE.width}px`;
    element.style.height = `${SIZE.height}px`;
    element.classList.add('canvas-element', 'canvas-transcript-element');
    contentLayer.appendChild(element);

    const body = element.querySelector('.tr') as HTMLElement | null;
    body?.classList.remove('tr-natural');
    const item: Element = {
        id: itemId,
        title: titleOf(session),
        symbol: TranscriptSym,
        x,
        y,
        content: session,
        renderContent: () => body ?? transcriptBody(session),
    };
    if (titleBar) storeCleanup(element, makeDraggable(element, titleBar, item, { logLabel: 'Transcript' }));
    const corner = createCorner();
    element.appendChild(corner);
    storeCleanup(element, makeResizable(element, corner, item, { logLabel: 'Transcript' }));

    uiState.addCanvasElement({ id: itemId, symbol: TranscriptSym, x, y, ...SIZE, content: session });

    // The button now lifts it back into a window.
    const expandBtn = document.createElement('button');
    expandBtn.className = 'titlebar-btn';
    expandBtn.textContent = '⬆';
    expandBtn.title = 'Expand to window';
    expandBtn.style.marginLeft = 'auto';
    preventDrag(expandBtn);
    placeBtn.replaceWith(expandBtn);
    wireExpandToWindow({
        element,
        expandBtn,
        elementId: itemId,
        title: titleOf(session),
        symbol: TranscriptSym,
        renderContent: () => body ?? transcriptBody(session),
        logLabel: 'Transcript',
        stopPropagation: true,
    });

    log.debug(SEG.ELEMENT, `[Transcript] placed ${itemId} on canvas ${canvasId} at (${x}, ${y})`);
}
