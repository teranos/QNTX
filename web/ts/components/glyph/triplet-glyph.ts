/**
 * Triplet Glyph (⫶) — the primary attestation interaction surface
 *
 * Groups all attestations sharing the same subject + predicate + context
 * into one browsable glyph. Individual attestations (differing timestamps,
 * actors, attributes) are navigable inside via a pager.
 *
 * Opened via double-click on grouped result rows in AX or SE glyphs.
 * Falls back to attestation glyph (+) for lone ungroupable attestations.
 */

import type { Glyph } from '@qntx/glyphs';
import { wireExpandToWindow, canvasPlaced, preventDrag } from '@qntx/glyphs';
import type { Attestation } from '../../generated/proto/plugin/grpc/protocol/atsstore';
import { Triplet, AX } from '@generated/sym.js';
import { renderTriple } from './attestation-triple';
import { renderAttestationAttrs, parseAttributes } from './attestation-attrs';
import { spawnAttestationGlyph } from './attestation-glyph';
import { log, SEG } from '../../logger';
import { spawnOnCanvas, spawnOnCanvasDragging } from './spawn-on-canvas';
import { el } from '../../html-utils';

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
    } catch { /* ignore */ }
    return String(value);
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
        if (typeof att.timestamp === 'number' && att.timestamp > 0) {
            timestamps.push(att.timestamp);
        }
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
 * Build the triplet meta pill — progressive disclosure:
 * Level 1: summary counts (hover pill to see)
 * Level 2: list of items (hover a summary line)
 * Level 3: highlight item, click to spawn attestation glyph
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
                spawnAttestationGlyph(atts[0], e.clientX, e.clientY);
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

    // Pager state
    let index = 0;

    // Navigation
    const nav = el('div', {
        style: { display: 'flex', alignItems: 'center', gap: '8px', marginBottom: '8px' },
    });

    const prevBtn = document.createElement('button');
    prevBtn.textContent = '\u25C0';
    prevBtn.style.cssText = 'background:none;border:1px solid var(--border);color:' + TRIPLET_VALUE + ';cursor:pointer;padding:2px 6px;font-size:11px;border-radius:3px';
    preventDrag(prevBtn);

    const nextBtn = document.createElement('button');
    nextBtn.textContent = '\u25B6';
    nextBtn.style.cssText = prevBtn.style.cssText;
    preventDrag(nextBtn);

    const counter = el('span', {
        style: { color: TRIPLET_DIM, fontSize: '11px', fontFamily: 'monospace' },
    });

    nav.append(prevBtn, counter, nextBtn);

    // Only show nav when there are multiple attestations
    if (sorted.length > 1) {
        container.appendChild(nav);
    }

    // Detail area
    const detail = el('div');
    container.appendChild(detail);

    const show = () => {
        const att = sorted[index];
        counter.textContent = `${index + 1} / ${sorted.length}`;
        prevBtn.style.opacity = index === 0 ? '0.3' : '1';
        nextBtn.style.opacity = index === sorted.length - 1 ? '0.3' : '1';

        detail.replaceChildren();

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
    };

    prevBtn.addEventListener('click', (e) => {
        e.stopPropagation();
        if (index > 0) { index--; show(); }
    });
    nextBtn.addEventListener('click', (e) => {
        e.stopPropagation();
        if (index < sorted.length - 1) { index++; show(); }
    });

    // Arrow key navigation
    container.tabIndex = 0;
    container.style.outline = 'none';
    container.addEventListener('keydown', (e) => {
        if (e.key === 'ArrowLeft' && index > 0) {
            index--; show(); e.preventDefault(); e.stopPropagation();
        } else if (e.key === 'ArrowRight' && index < sorted.length - 1) {
            index++; show(); e.preventDefault(); e.stopPropagation();
        }
    });

    show();
    return container;
}

// ─── Canvas glyph ────────────────────────────────────────────

/** Create a Triplet glyph for canvas placement */
export function createTripletGlyph(glyph: Glyph): HTMLElement {
    let attestations: Attestation[] = [];
    try {
        if (glyph.content) {
            const parsed = JSON.parse(glyph.content);
            attestations = Array.isArray(parsed) ? parsed : [parsed];
        }
    } catch (err) {
        log.warn(SEG.GLYPH, `[TripletGlyph] Failed to parse content for ${glyph.id}:`, err);
    }

    const representative = attestations[0] || null;

    // Title bar: ⫶ + triple + count
    const titleBar = el('div', {
        class: 'glyph-title-bar glyph-title-bar--auto',
        style: { position: 'relative' },
    });

    const symbolEl = el('span', {
        text: Triplet,
        style: { fontWeight: 'bold', flexShrink: '0', color: TRIPLET },
    });
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

    // Count badge
    if (attestations.length > 1) {
        titleBar.appendChild(el('span', {
            text: `(${attestations.length})`,
            style: {
                fontSize: '10px', color: TRIPLET_DIM, fontFamily: 'monospace',
                flexShrink: '0', marginLeft: '6px',
            },
        }));
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
        glyph,
        className: 'canvas-triplet-glyph',
        defaults: { x: 200, y: 200, width: 420, height: hasContent ? 240 : 28 },
        resizable: hasContent,
        useMinHeight: true,
        logLabel: 'TripletGlyph',
    });
    element.style.minWidth = '200px';
    element.appendChild(titleBar);

    if (hasContent) {
        const content = el('div', {
            class: 'glyph-content-area',
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
        glyphId: glyph.id,
        title,
        symbol: Triplet,
        renderContent: () => {
            const outer = el('div');
            const wrapper = el('div', { class: 'glyph-content' });
            outer.appendChild(wrapper);
            wrapper.appendChild(buildTripletContent(attestations));
            return outer;
        },
        logLabel: 'TripletGlyph',
    });

    log.debug(SEG.GLYPH, `[TripletGlyph] Created ${glyph.id} (${attestations.length} attestations)`);
    return element;
}

// ─── Spawn helpers ───────────────────────────────────────────

/** Spawn a triplet glyph on the canvas from grouped attestations */
export function spawnTripletGlyph(attestations: Attestation[], mouseX?: number, mouseY?: number): void {
    const representative = attestations[0];
    const title = representative
        ? `${representative.subjects?.join(', ') || '?'} is ${representative.predicates?.join(', ') || '?'}`
        : 'Triplet';

    spawnOnCanvas({
        symbol: Triplet,
        prefix: 'triplet',
        title,
        content: JSON.stringify(attestations),
        fallbackWidth: 420,
        fallbackHeight: 240,
        mouseX,
        mouseY,
    });
}

// ─── Result line rendering (for AX/SE) ──────────────────────

/** Render a triplet group as a one-line summary for result lists */
export function renderTripletResultLine(attestations: Attestation[]): HTMLElement {
    const representative = attestations[0];
    const count = attestations.length;

    const item = el('div', {
        class: 'ax-glyph-result-item has-tooltip',
        style: {
            padding: '8px', marginBottom: '4px',
            backgroundColor: 'rgba(30, 40, 50, 0.4)',
            borderRadius: '2px', cursor: 'pointer',
        },
    });

    item.dataset.attestation = JSON.stringify(attestations);
    item.addEventListener('dblclick', (e) => {
        e.stopPropagation();
        spawnTripletGlyph(attestations, e.clientX, e.clientY);
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

    // Count badge
    if (count > 1) {
        text.appendChild(el('span', {
            text: `(${count})`,
            style: { color: TRIPLET_DIM, fontSize: '10px', flexShrink: '0' },
        }));
    }

    item.dataset.tooltip = `${count} attestation${count !== 1 ? 's' : ''}`;
    item.appendChild(text);

    return item;
}
