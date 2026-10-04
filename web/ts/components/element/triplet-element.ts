/**
 * Triplet Element (⫶) — the primary attestation interaction surface
 *
 * Groups all attestations sharing the same subject + predicate + context
 * into one browsable element. Individual attestations (differing timestamps,
 * actors, attributes) are navigable inside via a pager.
 *
 * Opened via double-click on grouped result rows in AX or SE elements.
 * Falls back to attestation element (+) for lone ungroupable attestations.
 */

import type { Element } from '@teranos/elements';
import { wireExpandToWindow, canvasPlaced, preventDrag, createSymbolSpan, settleSymbolSpan, renderSparkline } from '@teranos/elements';
import type { Attestation } from '../../generated/proto/plugin/grpc/protocol/atsstore';
import { Triplet, AX } from '../../sym';
import { renderTriple } from './attestation-triple';
import { renderAttestationAttrs, parseAttributes } from './attestation-attrs';
import { spawnAttestationElement } from './attestation-element';
import { log, SEG } from '../../logger';
import { spawnOnCanvasDragging } from './spawn-on-canvas';
import { renderPager } from '../pager';
import { el } from '../../html-utils';
import { windowOf, seriesOf, labelsOf, formatIn, bucketStart, type Window } from '../sparkline';

// Quiet blue-grey — lighter, subtle blue touch, easy on the eyes
const TRIPLET = '#96a4b0';
const TRIPLET_KEYWORD = '#6e7a84';
const TRIPLET_VALUE = '#b0bcc6';
const TRIPLET_DIM = '#5e6a74';
const TRIPLET_BG = 'rgba(30, 35, 42, 0.95)';

/** Build the triplet key from an attestation */
export function tripletKey(att: Attestation): string {
    const s = (att.subjects || []).slice().sort().join(',');
    const p = (att.predicates || []).slice().sort().join(',');
    const c = (att.contexts || []).slice().sort().join(',');
    return `${s}|${p}|${c}`;
}

/** Group attestations by triplet key */
export function groupByTriplet(attestations: Attestation[]): Map<string, Attestation[]> {
    const groups = new Map<string, Attestation[]>();
    for (const att of attestations) {
        const key = tripletKey(att);
        const existing = groups.get(key);
        if (existing) {
            existing.push(att);
        } else {
            groups.set(key, [att]);
        }
    }
    return groups;
}


/** Format timestamp for display */
function formatTs(value: unknown): string {
    if (!value) return '';
    try {
        if (typeof value === 'string') return new Date(value).toLocaleString();
        if (typeof value === 'number') {
            const ms = value < 1e12 ? value * 1000 : value;
            return new Date(ms).toLocaleString();
        }
    } catch (notADate) {
        // The raw value stays visible; nothing is hidden by not dating it.
    }
    return String(value);
}

/** A timestamp in milliseconds. The type says a number, but a watcher streams
 *  the attestation with an ISO string (`2026-09-11T18:24:16Z`). */
function timeOf(value: unknown): number | null {
    if (typeof value === 'number') {
        if (value <= 0) return null;
        return value < 1e12 ? value * 1000 : value;
    }
    if (typeof value === 'string') {
        const t = Date.parse(value);
        return Number.isNaN(t) ? null : t;
    }
    return null;
}

/** Collect summary stats for the triplet meta pill */
function collectTripletMeta(attestations: Attestation[]) {
    const actors = new Set<string>();
    const sources = new Set<string>();
    const timestamps: number[] = [];
    const actorToAtts = new Map<string, Attestation[]>();
    const sourceToAtts = new Map<string, Attestation[]>();

    for (const att of attestations) {
        if (att.actors) {
            for (const a of att.actors) {
                actors.add(a);
                const list = actorToAtts.get(a);
                if (list) list.push(att); else actorToAtts.set(a, [att]);
            }
        }
        if (att.source) {
            sources.add(att.source);
            const list = sourceToAtts.get(att.source);
            if (list) list.push(att); else sourceToAtts.set(att.source, [att]);
        }
        const t = timeOf(att.timestamp);
        if (t !== null) timestamps.push(t);
    }

    let timeRange = '';
    if (timestamps.length > 0) {
        const min = Math.min(...timestamps);
        const max = Math.max(...timestamps);
        timeRange = min === max ? formatTs(min) : `${formatTs(min)} — ${formatTs(max)}`;
    }

    return { actors, sources, timestamps, actorToAtts, sourceToAtts, timeRange };
}

/**
 * "The axis of time is more useful than a tally": when the triple was attested
 * and when last, rather than how many times. Null when no attestation is dated.
 */
export function timeAxis(attestations: Attestation[], w?: Window, now: number = Date.now()): HTMLElement | null {
    const { timestamps } = collectTripletMeta(attestations);
    if (timestamps.length === 0) return null;
    if (!w) w = windowOf(timestamps, now);
    const axis = el('span', {
        class: 'triplet-time',
        style: { display: 'inline-flex', alignItems: 'center', gap: '6px', flexShrink: '0', marginLeft: 'auto' },
    });
    axis.dataset.window = windowKey(w);
    const spark = el('span', { class: 'sparkline', style: { display: 'inline-flex' } });
    spark.innerHTML = renderSparkline(seriesOf(timestamps, w), labelsOf(w));
    axis.appendChild(spark);
    axis.appendChild(el('span', {
        class: 'sparkline-last',
        text: formatIn(Math.max(...timestamps), w.unit),
        style: { color: TRIPLET_DIM, fontSize: '10px', fontFamily: 'monospace', fontVariantNumeric: 'tabular-nums' },
    }));
    return axis;
}

/** Two windows with the same key draw the same buckets. */
function windowKey(w: Window): string {
    return `${bucketStart(w.start, w.unit)}|${bucketStart(w.end, w.unit)}|${w.unit}`;
}

/**
 * One window for every triplet line in a result list, from the earliest
 * attestation the list holds to now, so a day sits at the same place on every
 * line and the list reads down. A line drawn in another window is redrawn.
 */
export function fitTimeAxes(container: HTMLElement, now: number = Date.now()): void {
    const rows = Array.from(container.querySelectorAll<HTMLElement>('[data-triplet-attestations]'));
    const groups = rows.map(row => JSON.parse(row.dataset.tripletAttestations || '[]') as Attestation[]);
    let start = Infinity;
    for (const group of groups) {
        for (const t of collectTripletMeta(group).timestamps) {
            if (t < start) start = t;
        }
    }
    if (start === Infinity) return;
    const w = windowOf([start], now);
    const key = windowKey(w);
    rows.forEach((row, i) => {
        const axis = row.querySelector<HTMLElement>('.triplet-time');
        if (!axis || axis.dataset.window === key) return;
        const fitted = timeAxis(groups[i], w, now);
        if (fitted) axis.replaceWith(fitted);
    });
}

/**
 * Build the triplet meta pill — progressive disclosure:
 * Level 1: summary counts (hover pill to see)
 * Level 2: list of items (hover a summary line)
 * Level 3: highlight item, click to spawn attestation element
 */
function buildTripletMetaPill(attestations: Attestation[]): HTMLElement | null {
    const meta = collectTripletMeta(attestations);
    if (meta.actors.size === 0 && meta.sources.size === 0 && meta.timestamps.length === 0) return null;

    const pill = el('div', { class: 'as-meta-pill' });
    const popover = el('div', {
        class: 'meta-popover as-meta-popover',
        style: { whiteSpace: 'normal', minWidth: '160px', maxWidth: '320px' },
    });

    // Summary lines
    const summaryLines: { label: string; items: Map<string, Attestation[]> }[] = [];
    if (meta.actors.size > 0) {
        summaryLines.push({ label: `${meta.actors.size} actor${meta.actors.size !== 1 ? 's' : ''}`, items: meta.actorToAtts });
    }
    if (meta.sources.size > 0) {
        summaryLines.push({ label: `${meta.sources.size} source${meta.sources.size !== 1 ? 's' : ''}`, items: meta.sourceToAtts });
    }

    for (const { label, items } of summaryLines) {
        const section = el('div', { style: { padding: '2px 0' } });
        const entries = [...items.entries()];
        const small = entries.length <= 5;

        // Header — only show count label when there are overflow items to expand
        if (!small) {
            section.appendChild(el('div', {
                text: label,
                style: { color: TRIPLET_DIM, fontSize: '11px', fontFamily: 'monospace', marginBottom: '2px' },
            }));
        }

        // Build a clickable item row
        const makeItem = (name: string, atts: Attestation[]): HTMLElement => {
            const item = el('div', {
                text: name,
                style: {
                    fontSize: '10px', color: TRIPLET_DIM, padding: '1px 0',
                    cursor: 'pointer', wordBreak: 'break-word',
                    transition: 'color 0.1s',
                },
            });
            item.addEventListener('mouseenter', () => { item.style.color = TRIPLET_VALUE; });
            item.addEventListener('mouseleave', () => { item.style.color = TRIPLET_DIM; });
            item.addEventListener('click', (e) => {
                e.stopPropagation();
                spawnAttestationElement(atts[0], e.clientX, e.clientY);
            });
            preventDrag(item);
            return item;
        };

        // Show first 5 (or all if <= 5) directly
        const visible = entries.slice(0, 5);
        const overflow = entries.slice(5);

        const list = el('div', {
            style: { marginLeft: small ? '0' : '8px', borderLeft: small ? 'none' : '1px solid ' + TRIPLET_DIM, paddingLeft: small ? '0' : '6px' },
        });
        for (const [name, atts] of visible) {
            list.appendChild(makeItem(name, atts));
        }
        section.appendChild(list);

        // Overflow items — hidden until hover on the section
        if (overflow.length > 0) {
            const moreLabel = el('div', {
                text: `+${overflow.length} more`,
                style: {
                    fontSize: '9px', color: TRIPLET_DIM, marginLeft: '8px',
                    paddingLeft: '6px', cursor: 'pointer', fontStyle: 'italic',
                },
            });
            const overflowList = el('div', {
                style: { display: 'none', marginLeft: '8px', borderLeft: '1px solid ' + TRIPLET_DIM, paddingLeft: '6px' },
            });
            for (const [name, atts] of overflow) {
                overflowList.appendChild(makeItem(name, atts));
            }
            moreLabel.addEventListener('mouseenter', () => {
                overflowList.style.display = 'block';
                moreLabel.style.display = 'none';
            });
            overflowList.addEventListener('mouseleave', () => {
                overflowList.style.display = 'none';
                moreLabel.style.display = 'block';
            });
            section.appendChild(moreLabel);
            section.appendChild(overflowList);
        }

        popover.appendChild(section);
    }

    // Time range (non-interactive)
    if (meta.timeRange) {
        popover.appendChild(el('div', {
            text: meta.timeRange,
            style: { fontSize: '10px', color: TRIPLET_DIM, padding: '2px 0', fontFamily: 'monospace' },
        }));
    }

    pill.appendChild(popover);
    return pill;
}

/** Build browsable content for a group of attestations */
function buildTripletContent(attestations: Attestation[]): HTMLElement {
    const container = el('div', {
        style: { padding: '8px 12px', fontFamily: 'monospace', fontSize: '12px' },
    });

    if (attestations.length === 0) return container;

    // Sort by timestamp, newest first
    const sorted = [...attestations].sort((a, b) => {
        const ta = a.timestamp || 0;
        const tb = b.timestamp || 0;
        return tb - ta;
    });

    // One attestation at a time. The paging is the shared one
    // (components/pager.ts); what an attestation looks like is this file's.
    renderPager(container, sorted, (detail, att) => {

        // Metadata: actor, timestamp, id
        const meta: string[] = [];
        if (att.actors && att.actors.length > 0) {
            meta.push(`by ${att.actors.join(', ')}`);
        }
        if (att.timestamp) {
            meta.push(formatTs(att.timestamp));
        }
        if (meta.length > 0) {
            detail.appendChild(el('div', {
                text: meta.join(' · '),
                style: { fontSize: '10px', color: TRIPLET_DIM, marginBottom: '6px' },
            }));
        }

        // ASID
        if (att.id) {
            detail.appendChild(el('div', {
                text: att.id,
                style: { fontSize: '9px', color: TRIPLET_DIM, marginBottom: '6px', wordBreak: 'break-word' },
            }));
        }

        // Attributes — full rich rendering (FASTA, structure, arrays, etc.)
        const attrs = parseAttributes(att);
        if (attrs) {
            const attrDiv = renderAttestationAttrs(attrs);
            attrDiv.style.borderTop = '1px solid var(--border)';
            attrDiv.style.paddingTop = '6px';
            detail.appendChild(attrDiv);
        }
    }, { mute: TRIPLET_DIM });

    return container;
}

// ─── Canvas element ────────────────────────────────────────────

/** Create a Triplet element for canvas placement */
export function createTripletElement(item: Element): HTMLElement {
    let attestations: Attestation[] = [];
    try {
        if (item.content) {
            const parsed = JSON.parse(item.content);
            attestations = Array.isArray(parsed) ? parsed : [parsed];
        }
    } catch (err) {
        log.warn(SEG.ELEMENT, `[TripletElement] Failed to parse content for ${item.id}:`, err);
    }

    const representative = attestations[0] || null;

    // Title bar: ⫶ + triple + when
    const titleBar = el('div', {
        class: 'title-bar title-bar--auto',
        style: { position: 'relative' },
    });

    const symbolEl = item.symbolElement ? settleSymbolSpan(item.symbolElement) : createSymbolSpan(Triplet);
    Object.assign(symbolEl.style, { fontWeight: 'bold', color: TRIPLET });
    titleBar.appendChild(symbolEl);

    if (representative) {
        const tripleText = renderTriple(representative, {
            palette: { value: TRIPLET_VALUE, keyword: TRIPLET_KEYWORD },
            showWatcherEyes: true,
            showAsPrefix: true,
            onKeywordClick: (axQuery, e) => {
                spawnOnCanvasDragging({
                    symbol: AX,
                    prefix: 'ax',
                    title: 'AX Query',
                    content: axQuery,
                    fallbackWidth: 400,
                    fallbackHeight: 200,
                }, e.clientX, e.clientY);
            },
        });
        titleBar.appendChild(tripleText);
    }

    const axis = timeAxis(attestations);
    if (axis) {
        axis.style.marginLeft = '6px';
        titleBar.appendChild(axis);
    }

    const expandBtn = el('button', {
        class: 'titlebar-btn',
        text: '\u2B06',
        style: { flexShrink: '0', marginLeft: 'auto' },
    });
    expandBtn.title = 'Expand to window';
    preventDrag(expandBtn);
    titleBar.appendChild(expandBtn);

    // Meta pill — progressive disclosure: summary → list → spawn
    if (attestations.length > 0) {
        const metaPill = buildTripletMetaPill(attestations);
        if (metaPill) titleBar.appendChild(metaPill);
    }

    const hasContent = attestations.length > 0;

    const { element } = canvasPlaced({
        item: item,
        className: 'canvas-triplet-element',
        defaults: { x: 200, y: 200, width: 420, height: hasContent ? 240 : 28 },
        resizable: hasContent,
        useMinHeight: true,
        logLabel: 'TripletElement',
    });
    element.style.minWidth = '200px';
    element.appendChild(titleBar);

    if (hasContent) {
        const content = el('div', {
            class: 'content-area',
            style: {
                backgroundColor: TRIPLET_BG,
                borderTop: '1px solid var(--border)',
                overflow: 'auto',
            },
        });
        content.appendChild(buildTripletContent(attestations));
        element.appendChild(content);
    }

    const title = representative
        ? `${representative.subjects?.join(', ') || '?'} is ${representative.predicates?.join(', ') || '?'}`
        : 'Triplet';

    wireExpandToWindow({
        element,
        expandBtn,
        elementId: item.id,
        title,
        symbol: Triplet,
        renderContent: () => {
            const outer = el('div');
            const wrapper = el('div', { class: 'element-content' });
            outer.appendChild(wrapper);
            wrapper.appendChild(buildTripletContent(attestations));
            return outer;
        },
        logLabel: 'TripletElement',
    });

    log.debug(SEG.ELEMENT, `[TripletElement] Created ${item.id} (${attestations.length} attestations)`);
    return element;
}

// ─── Spawn helpers ───────────────────────────────────────────

/** Spawn a triplet element on the canvas from grouped attestations */
export function spawnTripletElement(attestations: Attestation[], mouseX?: number, mouseY?: number): void {
    const representative = attestations[0];
    const title = representative
        ? `${representative.subjects?.join(', ') || '?'} is ${representative.predicates?.join(', ') || '?'}`
        : 'Triplet';

    spawnOnCanvasDragging({
        symbol: Triplet,
        prefix: 'triplet',
        title,
        content: JSON.stringify(attestations),
        fallbackWidth: 420,
        fallbackHeight: 240,
    }, mouseX || window.innerWidth / 2, mouseY || window.innerHeight / 2);
}

// ─── Result line rendering (for AX/SE) ──────────────────────

/** Render a triplet group as a one-line summary for result lists */
export function renderTripletResultLine(attestations: Attestation[], now: number = Date.now()): HTMLElement {
    const representative = attestations[0];

    const item = el('div', {
        class: 'ax-element-result-item has-tooltip',
        style: {
            padding: '8px', marginBottom: '4px',
            backgroundColor: 'rgba(30, 40, 50, 0.4)',
            borderRadius: '2px', cursor: 'pointer',
        },
    });

    item.dataset.attestation = JSON.stringify(attestations);
    item.addEventListener('dblclick', (e) => {
        e.stopPropagation();
        spawnTripletElement(attestations, e.clientX, e.clientY);
    });

    const text = el('div', {
        style: {
            display: 'flex', alignItems: 'baseline', gap: '6px',
            fontSize: '11px', fontFamily: 'monospace',
            wordBreak: 'break-word', overflowWrap: 'break-word',
        },
    });

    // Symbol
    text.appendChild(el('span', {
        text: Triplet,
        style: { color: TRIPLET, fontWeight: 'bold', flexShrink: '0' },
    }));

    // Triple text
    const tripleSpan = renderTriple(representative, {
        tag: 'span',
        fontSize: '11px',
        palette: { value: TRIPLET_VALUE, keyword: TRIPLET_KEYWORD },
        showWatcherEyes: true,
    });
    text.appendChild(tripleSpan);

    const axis = timeAxis(attestations, undefined, now);
    if (axis) {
        text.appendChild(axis);
        item.dataset.tooltip = collectTripletMeta(attestations).timeRange;
    }

    item.appendChild(text);

    return item;
}
