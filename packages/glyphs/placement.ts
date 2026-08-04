/**
 * Where a glyph lands when nothing says where to put it.
 *
 * Spawning without a cursor position put every glyph at the centre of the
 * window, so each new one covered the last. This scores candidates against
 * what is already on the canvas and takes the emptiest.
 */

export interface Rect {
    x: number;
    y: number;
    width: number;
    height: number;
}

export interface Size {
    width: number;
    height: number;
}

export interface PlacementOpts {
    attempts?: number;
    rng?: () => number;
    margin?: number;
}

// .glyph-title-bar in css/glyph/title-bar.css.
const TITLE_BAR_HEIGHT = 32;

// Two 22px buttons with 2px margins against 8px of bar padding.
const CONTROLS_WIDTH = 64;

// .glyph-symbol is the title bar's first child, so it holds the left edge.
const SYMBOL_WIDTH = 32;

// Ordered, not measured: the symbol is what a glyph is recognised by, the
// title says which one, the rest of the bar is the handle it moves by, the
// body scrolls. The tests pin the ordering; these numbers only keep it.
const COST_SYMBOL = 120;
const COST_TITLE = 40;
const COST_BAR = 12;
const COST_BODY = 1;

const DEFAULT_ATTEMPTS = 20;
const DEFAULT_MARGIN = 24;

/**
 * What is already on screen, read from the DOM rather than from a store, so
 * every manifestation counts whoever placed it.
 */
export function occupiedRects(exclude?: Element | null): Rect[] {
    const out: Rect[] = [];
    for (const el of document.querySelectorAll('[data-glyph-id]')) {
        if (el === exclude) continue;
        const r = el.getBoundingClientRect();
        if (r.width === 0 || r.height === 0) continue;
        out.push({ x: r.left, y: r.top, width: r.width, height: r.height });
    }
    return out;
}

/** Area shared by two rects. Zero when they only touch. */
export function overlapArea(a: Rect, b: Rect): number {
    const w = Math.min(a.x + a.width, b.x + b.width) - Math.max(a.x, b.x);
    const h = Math.min(a.y + a.height, b.y + b.height) - Math.max(a.y, b.y);
    return w > 0 && h > 0 ? w * h : 0;
}

/**
 * What it costs to put `candidate` here, given what is already placed.
 *
 * Occupants are counted separately rather than unioned, so landing across two
 * glyphs is worse than landing on one.
 */
export function placementCost(candidate: Rect, occupied: Rect[]): number {
    let cost = 0;

    for (const o of occupied) {
        const total = overlapArea(candidate, o);
        if (total === 0) continue;

        const barHeight = Math.min(TITLE_BAR_HEIGHT, o.height);
        const symbolWidth = Math.min(SYMBOL_WIDTH, o.width);
        const titleEnd = Math.max(symbolWidth, o.width - CONTROLS_WIDTH);

        const symbol: Rect = { x: o.x, y: o.y, width: symbolWidth, height: barHeight };
        const title: Rect = { x: o.x, y: o.y, width: titleEnd, height: barHeight };
        const bar: Rect = { x: o.x, y: o.y, width: o.width, height: barHeight };

        const onSymbol = overlapArea(candidate, symbol);
        const onTitle = overlapArea(candidate, title) - onSymbol;
        const onBar = overlapArea(candidate, bar) - onSymbol - onTitle;
        const onBody = total - onSymbol - onTitle - onBar;

        cost += onSymbol * COST_SYMBOL
            + onTitle * COST_TITLE
            + onBar * COST_BAR
            + onBody * COST_BODY;
    }

    return cost;
}

/**
 * Try `attempts` positions and keep the cheapest. A candidate that covers
 * nothing wins outright, so the search stops there.
 *
 * Random rather than a grid: a grid lands new glyphs on the same few points,
 * which is the problem being fixed one step removed.
 */
export function findPlacement(
    size: Size,
    occupied: Rect[],
    bounds: Size,
    opts: PlacementOpts = {},
): { x: number; y: number } {
    const attempts = opts.attempts ?? DEFAULT_ATTEMPTS;
    const rng = opts.rng ?? Math.random;
    const margin = opts.margin ?? DEFAULT_MARGIN;

    const spanX = bounds.width - size.width - margin;
    const spanY = bounds.height - size.height - margin;

    // Nothing to choose between when it does not fit.
    if (spanX <= 0 || spanY <= 0) return { x: 0, y: 0 };

    let best = { x: margin, y: margin };
    let bestCost = Infinity;

    for (let i = 0; i < attempts; i++) {
        const x = Math.round(margin + rng() * (spanX - margin));
        const y = Math.round(margin + rng() * (spanY - margin));
        const cost = placementCost({ x, y, width: size.width, height: size.height }, occupied);

        if (cost < bestCost) {
            best = { x, y };
            bestCost = cost;
            if (cost === 0) break;
        }
    }

    return best;
}
