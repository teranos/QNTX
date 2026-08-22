/**
 * The brow — the node's status line in the iPhone's unsafe top region.
 *
 * "There's this one character high row area between the top of the iPhone
 *  and the magic island... a special status line we place there and halfway
 *  magic island."
 *
 * The band sits level with the Dynamic Island, flanking it in the two ears;
 * the sliver above the island carries a hairline that goes solid when
 * anything on the row is unwell. Items come from /statusline?format=json —
 * the node decides what is worth saying; this file is only the palette for
 * one more surface. One line, always: an item that does not fit is dropped
 * whole, never clipped.
 *
 * The shell's only part is granting the territory: status bar hidden,
 * webview edge-to-edge. The brow mounts when that headroom exists.
 */

import { apiFetch } from './client';
import { isTauri } from './tauri-notifications';
import { log, SEG } from './logger';

// ── The row's shape (server/statusline_handlers.go) ─────────────────

export interface StatusItem {
    name: string;
    note?: string;
    glyph: string; // '+' well, '!' unwell
}

const GLYPH_WELL = '+';

// ── Geometry ────────────────────────────────────────────────────────
//
// The island's box is not exposed to web content, but its relation to the
// safe area is stable across the island families: the safe line sits 11pt
// below a 37pt-tall island, so the band's top falls out of the inset alone.
// Width is the one number taken on faith: 126pt, centered.

const ISLAND_HEIGHT = 37;
const ISLAND_BELOW_GAP = 11;
const ISLAND_HALF_WIDTH = 63;
const EAR_MARGIN = 8;

// An island device grants at least this much headroom; anything less is a
// notch, a status bar, or a desktop — no brow.
const MIN_HEADROOM = 44;

export interface BrowGeometry {
    /** Top of the band that sits level with the island. */
    bandTop: number;
    bandHeight: number;
    /** The ears end/begin here — the island's column, plus margin. */
    gapLeft: number;
    gapRight: number;
    /** The sliver between the screen's top edge and the island. */
    sliverHeight: number;
}

export function browGeometry(insetPx: number, viewportWidth: number): BrowGeometry {
    const bandTop = Math.max(0, insetPx - ISLAND_BELOW_GAP - ISLAND_HEIGHT);
    return {
        bandTop,
        bandHeight: ISLAND_HEIGHT,
        gapLeft: Math.floor(viewportWidth / 2 - ISLAND_HALF_WIDTH - EAR_MARGIN),
        gapRight: Math.ceil(viewportWidth / 2 + ISLAND_HALF_WIDTH + EAR_MARGIN),
        sliverHeight: bandTop,
    };
}

/** The brow exists where the shell granted headroom over an island. */
export function shouldMountBrow(insetPx: number, inShell: boolean, forced = false): boolean {
    if (forced) return true;
    return inShell && insetPx >= MIN_HEADROOM;
}

/** Measure env(safe-area-inset-top) through a probe element. */
export function measureTopInset(doc: Document): number {
    const probe = doc.createElement('div');
    probe.style.position = 'fixed';
    probe.style.top = '0';
    probe.style.paddingTop = 'env(safe-area-inset-top)';
    probe.style.visibility = 'hidden';
    doc.body.appendChild(probe);
    const inset = parseFloat(getComputedStyle(probe).paddingTop) || 0;
    probe.remove();
    return inset;
}

// ── Rendering ───────────────────────────────────────────────────────

function renderItem(item: StatusItem): HTMLElement {
    const el = document.createElement('span');
    el.className = item.glyph === GLYPH_WELL ? 'brow-item brow-well' : 'brow-item brow-unwell';
    el.title = item.note ? `${item.name} ${item.note}` : item.name;

    const glyph = document.createElement('span');
    glyph.className = 'brow-glyph';
    glyph.textContent = item.glyph;
    el.appendChild(glyph);

    const name = document.createElement('span');
    name.textContent = item.name;
    el.appendChild(name);

    if (item.note) {
        const note = document.createElement('span');
        note.className = 'brow-note';
        note.textContent = item.note;
        el.appendChild(note);
    }
    return el;
}

/**
 * Fill an ear, then drop whole trailing items until the line fits.
 * Nothing is ever clipped mid-item.
 */
function fillEar(ear: HTMLElement, items: StatusItem[]): void {
    ear.textContent = '';
    for (const item of items) {
        ear.appendChild(renderItem(item));
    }
    while (ear.lastElementChild && ear.scrollWidth > ear.clientWidth) {
        ear.lastElementChild.remove();
    }
}

export interface BrowElements {
    root: HTMLElement;
    leftEar: HTMLElement;
    rightEar: HTMLElement;
    sliver: HTMLElement;
}

/** Build the brow's DOM from measured geometry. */
export function buildBrow(insetPx: number, viewportWidth: number): BrowElements {
    const g = browGeometry(insetPx, viewportWidth);

    const root = document.createElement('div');
    root.className = 'brow';
    root.style.height = `${insetPx}px`;

    const sliver = document.createElement('div');
    sliver.className = 'brow-sliver';
    sliver.style.top = `${Math.max(0, g.sliverHeight - 4)}px`;
    sliver.style.left = `${g.gapLeft + EAR_MARGIN}px`;
    sliver.style.width = `${g.gapRight - g.gapLeft - 2 * EAR_MARGIN}px`;
    root.appendChild(sliver);

    const leftEar = document.createElement('div');
    leftEar.className = 'brow-ear brow-ear-left';
    leftEar.style.top = `${g.bandTop}px`;
    leftEar.style.height = `${g.bandHeight}px`;
    leftEar.style.width = `${g.gapLeft}px`;
    root.appendChild(leftEar);

    const rightEar = document.createElement('div');
    rightEar.className = 'brow-ear brow-ear-right';
    rightEar.style.top = `${g.bandTop}px`;
    rightEar.style.height = `${g.bandHeight}px`;
    rightEar.style.width = `${viewportWidth - g.gapRight}px`;
    root.appendChild(rightEar);

    return { root, leftEar, rightEar, sliver };
}

/** Paint one response onto the brow: first item left, the rest right. */
export function paintBrow(brow: BrowElements, items: StatusItem[]): void {
    fillEar(brow.leftEar, items.slice(0, 1));
    fillEar(brow.rightEar, items.slice(1));
    const unwell = items.some((it) => it.glyph !== GLYPH_WELL);
    brow.sliver.classList.toggle('brow-sliver-unwell', unwell);
}

// ── Lifecycle ───────────────────────────────────────────────────────

const POLL_MS = 1000; // the heartbeat cadence the access log expects of /statusline

async function fetchItems(): Promise<StatusItem[]> {
    const response = await apiFetch('/statusline?format=json');
    if (!response.ok) {
        throw new Error(`/statusline answered ${response.status} ${response.statusText}`);
    }
    const body = await response.json() as { items: StatusItem[] };
    return body.items ?? [];
}

/**
 * Mount the brow if this display has the territory for it.
 * Returns a teardown, or null where no brow belongs.
 */
export function initBrow(doc: Document = document): (() => void) | null {
    const forced = new URLSearchParams(doc.defaultView?.location.search ?? '').has('brow');
    const inset = measureTopInset(doc);
    if (!shouldMountBrow(inset, isTauri(), forced)) return null;

    const brow = buildBrow(forced && inset < MIN_HEADROOM ? 59 : inset, doc.defaultView?.innerWidth ?? 393);
    doc.body.appendChild(brow.root);
    log.info(SEG.UI, `[Brow] Mounted with ${inset}px headroom`);

    let timer: ReturnType<typeof setInterval> | null = null;

    const tick = async () => {
        try {
            paintBrow(brow, await fetchItems());
        } catch (err) {
            log.warn(SEG.UI, `[Brow] Status fetch failed: ${err instanceof Error ? err.message : String(err)}`);
        }
    };

    const start = () => {
        if (timer === null) {
            void tick();
            timer = setInterval(() => void tick(), POLL_MS);
        }
    };
    const stop = () => {
        if (timer !== null) {
            clearInterval(timer);
            timer = null;
        }
    };

    // The heartbeat pauses when nobody is looking
    const onVisibility = () => (doc.hidden ? stop() : start());
    doc.addEventListener('visibilitychange', onVisibility);
    start();

    return () => {
        stop();
        doc.removeEventListener('visibilitychange', onVisibility);
        brow.root.remove();
    };
}
