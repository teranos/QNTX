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
import { Triplet } from '@generated/sym.js';
import { renderTriple } from './attestation-triple';
import { renderAttestationAttrs, parseAttributes } from './attestation-glyph';
import { log, SEG } from '../../logger';
import { uiState } from '../../state/ui';
import { getGlyphTypeBySymbol } from './glyph-registry';
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
    const contentLayer = document.querySelector('.canvas-content-layer') as HTMLElement;
    if (!contentLayer) {
        log.warn(SEG.GLYPH, '[TripletGlyph] Cannot spawn: no canvas-content-layer found');
        return;
    }

    const glyphId = `triplet-${crypto.randomUUID()}`;
    const representative = attestations[0];
    const title = representative
        ? `${representative.subjects?.join(', ') || '?'} is ${representative.predicates?.join(', ') || '?'}`
        : 'Triplet';

    const glyph: Glyph = {
        id: glyphId,
        title,
        symbol: Triplet,
        x: mouseX !== undefined ? Math.round(mouseX - contentLayer.getBoundingClientRect().left + 20) : Math.round(window.innerWidth / 2 - 210),
        y: mouseY !== undefined ? Math.round(mouseY - contentLayer.getBoundingClientRect().top - 20) : Math.round(window.innerHeight / 2 - 120),
        content: JSON.stringify(attestations),
        renderContent: () => el('div'),
    };

    const entry = getGlyphTypeBySymbol(Triplet);
    if (!entry) {
        log.error(SEG.GLYPH, '[TripletGlyph] Triplet not found in glyph registry');
        return;
    }

    const glyphElement = entry.render(glyph) as HTMLElement;
    contentLayer.appendChild(glyphElement);

    const rect = glyphElement.getBoundingClientRect();
    uiState.addCanvasGlyph({
        id: glyphId,
        symbol: Triplet,
        x: glyph.x!,
        y: glyph.y!,
        width: Math.round(rect.width) || 420,
        height: Math.round(rect.height) || 240,
        content: JSON.stringify(attestations),
    });

    log.debug(SEG.GLYPH, `[TripletGlyph] Spawned ${glyphId} at (${glyph.x}, ${glyph.y})`);
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
