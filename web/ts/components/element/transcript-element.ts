// Transcript Element — one session Ground recorded, read as what was said and done.
// "It's really the Svelte UI part that I want to rehome into QNTX"
// The node derives it (transcripts read); this draws what it answers.

import type { Element } from '@teranos/elements';
import { canvasPlaced, wireExpandToWindow, preventDrag, createSymbolSpan, settleSymbolSpan } from '@teranos/elements';
import { apiJson } from '../../client';
import { escapeHtml } from '../../html-utils';
import { log, SEG } from '../../logger';
import { Transcript as TranscriptSym } from '../../sym';
import { createAutoSave } from './element-autosave';
import { spawnAttestationAsWindow } from './attestation-element';
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

function when(at: string): string {
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

/** The sessions Ground recorded here, newest first, each opened by its first prompt. */
export function renderSessions(body: HTMLElement, reads: TranscriptRead[], onChoose: (session: string) => void): void {
    body.replaceChildren();
    if (reads.length === 0) {
        const none = document.createElement('div');
        none.className = 'tr-said';
        none.textContent = 'No session Ground recorded is in this namespace.';
        body.appendChild(none);
        return;
    }
    for (const read of reads) {
        const row = document.createElement('button');
        row.className = 'tr-session';
        const at = document.createElement('span');
        at.className = 'tr-when';
        at.textContent = when(read.started);
        const opening = document.createElement('span');
        opening.className = 'tr-opening';
        opening.textContent = read.turns.find(t => t.speaker === 'human')?.text ?? read.session;
        row.append(at, opening);
        row.addEventListener('click', (e) => {
            e.stopPropagation();
            onChoose(read.session);
        });
        body.appendChild(row);
    }
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

/** Create a transcript element: the sessions to choose from, or the one it holds. */
export function createTranscriptElement(item: Element): HTMLElement {
    let session = item.content ?? '';

    const titleBar = document.createElement('div');
    titleBar.className = 'title-bar';
    const symbol = item.symbolElement ? settleSymbolSpan(item.symbolElement) : createSymbolSpan(TranscriptSym);
    const label = document.createElement('span');
    label.textContent = 'Transcript';
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
        defaults: { x: 200, y: 200, width: 520, height: 640 },
        resizable: true,
        logLabel: 'Transcript',
    });
    element.appendChild(titleBar);

    const body = document.createElement('div');
    body.className = 'content-area tr';
    // A turn is pressed, not dragged: the title bar is what moves the element.
    preventDrag(body);
    element.appendChild(body);

    const { save } = createAutoSave(item.id, () => session, 'Transcript');

    const show = (id: string) => {
        said(body, 'Reading session ' + id + '…');
        apiJson<{ transcripts: TranscriptRead[] }>(`/api/transcripts?session=${encodeURIComponent(id)}`)
            .then(answer => {
                const read = answer.transcripts[0];
                if (!read) { said(body, 'No session ' + id + ' is in this namespace.'); return; }
                renderTranscript(body, read, (turn) => {
                    openTurn(id, turn).catch((err: unknown) => log.error(SEG.ELEMENT, `[Transcript] turn ${turn.of} did not open:`, err));
                });
            })
            .catch((err: unknown) => said(body, 'Could not read session ' + id + ': ' + String(err)));
    };

    if (session) {
        show(session);
    } else {
        said(body, 'Reading the sessions Ground recorded here…');
        apiJson<{ transcripts: TranscriptRead[] }>('/api/transcripts')
            .then(answer => renderSessions(body, answer.transcripts, (id) => {
                session = id;
                save();
                show(id);
            }))
            .catch((err: unknown) => said(body, 'Could not read sessions: ' + String(err)));
    }

    wireExpandToWindow({
        element,
        expandBtn,
        elementId: item.id,
        title: 'Transcript',
        symbol: TranscriptSym,
        renderContent: () => body,
        logLabel: 'Transcript',
    });

    return element;
}
