/**
 * Sigma Glyph (Σ) — report card for distilled attestations
 *
 * Sigma attestations are summaries of many observations, not single events.
 * The glyph renders as a report: big observation count, time range,
 * proportional bars for string distributions, ranges for number aggregates.
 *
 * Opened via double-click on sigma result items in AX or SE glyphs.
 */

import type { Glyph } from '@qntx/glyphs';
import { wireExpandToWindow, isInWindowState, glyphRun, canvasPlaced, preventDrag, setWindowState, teardownWindowDrag, removeWindowControls, makeDraggable, makeResizable, storeCleanup } from '@qntx/glyphs';
import type { Attestation } from '../../generated/proto/plugin/grpc/protocol/atsstore';
import { Sigma } from '@generated/sym.js';
import { log, SEG } from '../../logger';
import { screenToCanvas } from './canvas/canvas-pan';
import { uiState } from '../../state/ui';
import { getGlyphTypeBySymbol } from './glyph-registry';

// Amber palette for sigma attestations
const AMBER = '#d4a574';
const AMBER_DIM = '#8a7560';
const AMBER_VALUE = '#e8d0b4';
const AMBER_BAR = '#c49a6c';
const AMBER_BAR_BG = 'rgba(140, 110, 80, 0.25)';

interface DistillAttrs {
    _distill: boolean;
    _count: number;
    _total: number;
    _first_seen: string;
    _last_seen: string;
    _subjects_count?: number;
    _subjects_sample?: string[];
    _version?: string;
    _rust_version?: string;
    [key: string]: unknown;
}

/** Check if an attestation is a sigma (distilled) attestation */
export function isSigmaAttestation(attestation: Attestation): boolean {
    if (!attestation.attributes) return false;
    const attrs = typeof attestation.attributes === 'string'
        ? (() => { try { return JSON.parse(attestation.attributes as string); } catch { return null; } })()
        : attestation.attributes;
    return attrs?._distill === true;
}

/** Extract the predicate name, stripping distill: prefix */
function extractPredicate(attestation: Attestation): string {
    const pred = attestation.predicates?.[0] || 'unknown';
    if (pred.startsWith('distill:')) return pred.slice(8);
    return pred;
}

/** Parse distill attributes from attestation */
function parseDistillAttrs(attestation: Attestation): DistillAttrs | null {
    if (!attestation.attributes) return null;
    try {
        const attrs = typeof attestation.attributes === 'string'
            ? JSON.parse(attestation.attributes as string)
            : attestation.attributes;
        if (attrs?._distill === true) return attrs as DistillAttrs;
    } catch { /* ignore */ }
    return null;
}

/** Format a date string to a compact display */
function formatDate(iso: string): string {
    try {
        const d = new Date(iso);
        const months = ['Jan', 'Feb', 'Mar', 'Apr', 'May', 'Jun', 'Jul', 'Aug', 'Sep', 'Oct', 'Nov', 'Dec'];
        return `${months[d.getMonth()]} ${d.getDate()}`;
    } catch {
        return iso;
    }
}

/** Format a number with locale separators */
function formatNum(n: number): string {
    return n.toLocaleString();
}

/** Check if a value is a number aggregate {min, max, sum, count} */
function isNumberAggregate(v: unknown): v is { min: number; max: number; sum: number; count: number } {
    if (typeof v !== 'object' || v === null) return false;
    const o = v as Record<string, unknown>;
    return typeof o.min === 'number' && typeof o.max === 'number' && typeof o.sum === 'number' && typeof o.count === 'number';
}

/** Check if a value is a string aggregate {values, count} */
function isStringAggregate(v: unknown): v is { values: string[]; count: number } {
    if (typeof v !== 'object' || v === null) return false;
    const o = v as Record<string, unknown>;
    return Array.isArray(o.values) && typeof o.count === 'number';
}

/** Keys that are structural metadata, not domain data */
const META_KEYS = new Set([
    '_distill', '_count', '_total', '_first_seen', '_last_seen',
    '_subjects_count', '_subjects_sample', '_version', '_rust_version',
]);

// ─── Rendering ───────────────────────────────────────────────

/** Build the sigma report content (used in both canvas and window) */
function buildSigmaReport(attestation: Attestation, attrs: DistillAttrs): HTMLElement {
    const container = document.createElement('div');
    container.style.padding = '8px 12px';
    container.style.fontFamily = 'monospace';
    container.style.fontSize = '12px';

    // ── Header: big number + predicate ──
    const header = document.createElement('div');
    header.style.marginBottom = '8px';

    const bigNumber = document.createElement('div');
    bigNumber.style.fontSize = '24px';
    bigNumber.style.fontWeight = 'bold';
    bigNumber.style.color = AMBER;
    bigNumber.style.lineHeight = '1.2';
    const total = attrs._total || attrs._count || 0;
    bigNumber.textContent = `${Sigma} ${formatNum(total)}`;
    header.appendChild(bigNumber);

    const subtitle = document.createElement('div');
    subtitle.style.fontSize = '11px';
    subtitle.style.color = AMBER_DIM;
    const batchNote = attrs._count && attrs._count !== total ? ` (batch of ${attrs._count})` : '';
    subtitle.textContent = `observations${batchNote}`;
    header.appendChild(subtitle);

    const predLine = document.createElement('div');
    predLine.style.fontSize = '13px';
    predLine.style.color = AMBER_VALUE;
    predLine.style.marginTop = '2px';
    predLine.textContent = extractPredicate(attestation);
    header.appendChild(predLine);

    container.appendChild(header);

    // ── Time range ──
    if (attrs._first_seen && attrs._last_seen) {
        const timeRow = document.createElement('div');
        timeRow.style.marginBottom = '10px';
        timeRow.style.display = 'flex';
        timeRow.style.alignItems = 'center';
        timeRow.style.gap = '6px';
        timeRow.style.fontSize = '11px';
        timeRow.style.color = AMBER_DIM;

        const startLabel = document.createElement('span');
        startLabel.textContent = formatDate(attrs._first_seen);
        startLabel.style.color = AMBER_VALUE;

        const line = document.createElement('span');
        line.style.flex = '1';
        line.style.height = '1px';
        line.style.backgroundColor = AMBER_DIM;

        const endLabel = document.createElement('span');
        endLabel.textContent = formatDate(attrs._last_seen);
        endLabel.style.color = AMBER_VALUE;

        timeRow.append(startLabel, line, endLabel);
        container.appendChild(timeRow);
    }

    // ── Domain attributes (non-meta) ──
    for (const [key, value] of Object.entries(attrs)) {
        if (META_KEYS.has(key)) continue;

        const section = document.createElement('div');
        section.style.marginBottom = '8px';

        const label = document.createElement('div');
        label.style.fontSize = '10px';
        label.style.color = AMBER_DIM;
        label.style.marginBottom = '3px';
        label.textContent = key;
        section.appendChild(label);

        if (isStringAggregate(value)) {
            // String aggregate — inline tags
            const tagRow = document.createElement('div');
            tagRow.style.display = 'flex';
            tagRow.style.flexWrap = 'wrap';
            tagRow.style.gap = '4px';
            tagRow.style.alignItems = 'center';

            for (const v of value.values.slice(0, 20)) {
                const tag = document.createElement('span');
                tag.style.fontSize = '11px';
                tag.style.color = AMBER_VALUE;
                tag.style.padding = '1px 5px';
                tag.style.backgroundColor = AMBER_BAR_BG;
                tag.style.borderRadius = '2px';
                tag.textContent = String(v);
                tagRow.appendChild(tag);
            }
            if (value.count > 20) {
                const more = document.createElement('span');
                more.style.fontSize = '10px';
                more.style.color = AMBER_DIM;
                more.textContent = `+${value.count - 20} more`;
                tagRow.appendChild(more);
            }
            section.appendChild(tagRow);
        } else if (isNumberAggregate(value)) {
            // Number aggregate — range bar
            const rangeRow = document.createElement('div');
            rangeRow.style.display = 'flex';
            rangeRow.style.alignItems = 'center';
            rangeRow.style.gap = '6px';

            const minLabel = document.createElement('span');
            minLabel.style.fontSize = '11px';
            minLabel.style.color = AMBER_VALUE;
            minLabel.textContent = formatNum(value.min);

            const bar = document.createElement('div');
            bar.style.flex = '1';
            bar.style.height = '4px';
            bar.style.backgroundColor = AMBER_BAR_BG;
            bar.style.borderRadius = '2px';
            bar.style.position = 'relative';

            // Average marker
            if (value.count > 0) {
                const avg = value.sum / value.count;
                const range = value.max - value.min;
                if (range > 0) {
                    const pct = ((avg - value.min) / range) * 100;
                    const marker = document.createElement('div');
                    marker.style.position = 'absolute';
                    marker.style.left = `${pct}%`;
                    marker.style.top = '-2px';
                    marker.style.width = '2px';
                    marker.style.height = '8px';
                    marker.style.backgroundColor = AMBER_BAR;
                    marker.style.borderRadius = '1px';
                    bar.appendChild(marker);
                }
            }

            const maxLabel = document.createElement('span');
            maxLabel.style.fontSize = '11px';
            maxLabel.style.color = AMBER_VALUE;
            maxLabel.textContent = formatNum(value.max);

            const avgLabel = document.createElement('span');
            avgLabel.style.fontSize = '10px';
            avgLabel.style.color = AMBER_DIM;
            avgLabel.style.flexShrink = '0';
            const avg = value.count > 0 ? Math.round(value.sum / value.count) : 0;
            avgLabel.textContent = `avg ${formatNum(avg)}`;

            rangeRow.append(minLabel, bar, maxLabel, avgLabel);
            section.appendChild(rangeRow);
        } else if (typeof value === 'string' || typeof value === 'number' || typeof value === 'boolean') {
            // Scalar constant
            const scalar = document.createElement('div');
            scalar.style.fontSize = '11px';
            scalar.style.color = AMBER_VALUE;
            scalar.textContent = String(value);
            section.appendChild(scalar);
        } else {
            // Unknown shape — JSON fallback
            const fallback = document.createElement('div');
            fallback.style.fontSize = '11px';
            fallback.style.color = AMBER_DIM;
            fallback.style.whiteSpace = 'pre-wrap';
            fallback.style.wordBreak = 'break-word';
            fallback.textContent = JSON.stringify(value, null, 2);
            section.appendChild(fallback);
        }

        container.appendChild(section);
    }

    // ── Subjects ──
    if (attrs._subjects_count || attrs._subjects_sample) {
        const section = document.createElement('div');
        section.style.marginBottom = '8px';

        const label = document.createElement('div');
        label.style.fontSize = '10px';
        label.style.color = AMBER_DIM;
        label.style.marginBottom = '3px';
        label.textContent = `${attrs._subjects_count || 0} subjects`;
        section.appendChild(label);

        if (attrs._subjects_sample && attrs._subjects_sample.length > 0) {
            const sample = document.createElement('div');
            sample.style.fontSize = '11px';
            sample.style.color = AMBER_VALUE;
            sample.style.wordBreak = 'break-word';
            sample.textContent = attrs._subjects_sample.join(' · ');
            section.appendChild(sample);
        }

        container.appendChild(section);
    }

    // ── Version footer ──
    if (attrs._version || attrs._rust_version) {
        const footer = document.createElement('div');
        footer.style.fontSize = '9px';
        footer.style.color = AMBER_DIM;
        footer.style.marginTop = '4px';
        footer.style.textAlign = 'right';
        const parts: string[] = [];
        if (attrs._rust_version) parts.push(attrs._rust_version);
        if (attrs._version) parts.push(attrs._version.split(' ')[0]); // just version, not hash
        footer.textContent = parts.join(' · ');
        container.appendChild(footer);
    }

    return container;
}

// ─── Canvas glyph ────────────────────────────────────────────

/** Create a Sigma glyph for canvas placement */
export function createSigmaGlyph(glyph: Glyph): HTMLElement {
    let attestation: Attestation | null = null;
    try {
        if (glyph.content) attestation = JSON.parse(glyph.content);
    } catch (err) {
        log.warn(SEG.GLYPH, `[SigmaGlyph] Failed to parse content for ${glyph.id}:`, err);
    }

    const attrs = attestation ? parseDistillAttrs(attestation) : null;

    // Title bar: Σ + predicate + count
    const titleBar = document.createElement('div');
    titleBar.className = 'glyph-title-bar glyph-title-bar--auto';
    titleBar.style.position = 'relative';

    const symbolEl = document.createElement('span');
    symbolEl.textContent = Sigma;
    symbolEl.style.fontWeight = 'bold';
    symbolEl.style.flexShrink = '0';
    symbolEl.style.color = AMBER;
    titleBar.appendChild(symbolEl);

    if (attestation && attrs) {
        const titleText = document.createElement('span');
        titleText.style.fontSize = '12px';
        titleText.style.fontFamily = 'monospace';
        titleText.style.color = AMBER_VALUE;

        const total = attrs._total || attrs._count || 0;
        titleText.textContent = `${formatNum(total)} obs · ${extractPredicate(attestation)}`;
        titleBar.appendChild(titleText);
    }

    const expandBtn = document.createElement('button');
    expandBtn.className = 'titlebar-btn';
    expandBtn.textContent = '\u2B06';
    expandBtn.title = 'Expand to window';
    expandBtn.style.flexShrink = '0';
    expandBtn.style.marginLeft = 'auto';
    preventDrag(expandBtn);
    titleBar.appendChild(expandBtn);

    const { element } = canvasPlaced({
        glyph,
        className: 'canvas-sigma-glyph',
        defaults: { x: 200, y: 200, width: 380, height: 300 },
        resizable: true,
        useMinHeight: true,
        logLabel: 'SigmaGlyph',
    });
    element.style.minWidth = '200px';
    element.appendChild(titleBar);

    // Report content
    if (attestation && attrs) {
        const content = document.createElement('div');
        content.className = 'glyph-content-area';
        content.style.backgroundColor = 'rgba(25, 25, 30, 0.95)';
        content.style.borderTop = '1px solid var(--border)';
        content.style.overflow = 'auto';
        content.appendChild(buildSigmaReport(attestation, attrs));
        element.appendChild(content);
    }

    const title = attestation && attrs
        ? `${Sigma} ${formatNum(attrs._total || attrs._count || 0)} · ${extractPredicate(attestation)}`
        : 'Sigma';

    wireExpandToWindow({
        element,
        expandBtn,
        glyphId: glyph.id,
        title,
        symbol: Sigma,
        renderContent: () => {
            const outer = document.createElement('div');
            const wrapper = document.createElement('div');
            wrapper.className = 'glyph-content';
            outer.appendChild(wrapper);
            if (attestation && attrs) {
                wrapper.appendChild(buildSigmaReport(attestation, attrs));
            }
            return outer;
        },
        logLabel: 'SigmaGlyph',
    });

    log.debug(SEG.GLYPH, `[SigmaGlyph] Created ${glyph.id}`);
    return element;
}

// ─── Spawn helpers ───────────────────────────────────────────

/** Spawn a sigma attestation on the canvas */
export function spawnSigmaGlyph(attestation: Attestation, mouseX?: number, mouseY?: number): void {
    const contentLayer = document.querySelector('.canvas-content-layer') as HTMLElement;
    if (!contentLayer) {
        log.warn(SEG.GLYPH, '[SigmaGlyph] Cannot spawn: no canvas-content-layer found');
        return;
    }

    const glyphId = `sigma-${crypto.randomUUID()}`;
    const glyph: Glyph = {
        id: glyphId,
        title: 'Sigma',
        symbol: Sigma,
        x: mouseX !== undefined ? Math.round(mouseX - contentLayer.getBoundingClientRect().left + 20) : Math.round(window.innerWidth / 2 - 190),
        y: mouseY !== undefined ? Math.round(mouseY - contentLayer.getBoundingClientRect().top - 20) : Math.round(window.innerHeight / 2 - 150),
        content: JSON.stringify(attestation),
        renderContent: () => document.createElement('div'),
    };

    const entry = getGlyphTypeBySymbol(Sigma);
    if (!entry) {
        log.error(SEG.GLYPH, '[SigmaGlyph] Sigma not found in glyph registry');
        return;
    }

    const glyphElement = entry.render(glyph) as HTMLElement;
    contentLayer.appendChild(glyphElement);

    const rect = glyphElement.getBoundingClientRect();
    uiState.addCanvasGlyph({
        id: glyphId,
        symbol: Sigma,
        x: glyph.x!,
        y: glyph.y!,
        width: Math.round(rect.width) || 380,
        height: Math.round(rect.height) || 300,
        content: JSON.stringify(attestation),
    });

    log.debug(SEG.GLYPH, `[SigmaGlyph] Spawned ${glyphId} at (${glyph.x}, ${glyph.y})`);
}

/** Spawn a sigma attestation as a window via glyphRun */
export function spawnSigmaAsWindow(attestation: Attestation): void {
    const glyphId = `sigma-${attestation.id || crypto.randomUUID()}`;

    const existing = document.querySelector(`[data-glyph-id="${glyphId}"]`) as HTMLElement | null;
    if (existing) {
        if (isInWindowState(existing)) {
            existing.style.zIndex = '1001';
            setTimeout(() => { existing.style.zIndex = '1000'; }, 2000);
        }
        return;
    }
    if (glyphRun.has(glyphId)) {
        glyphRun.openGlyph(glyphId);
        return;
    }

    const attrs = parseDistillAttrs(attestation);
    const total = attrs?._total || attrs?._count || 0;
    const title = `${Sigma} ${formatNum(total)} · ${extractPredicate(attestation)}`;

    glyphRun.add({
        id: glyphId,
        title,
        symbol: Sigma,
        initialWidth: '380px',
        initialHeight: '400px',
        onClose: () => {
            glyphRun.remove(glyphId);
            log.debug(SEG.GLYPH, `[SigmaGlyph] Closed window ${glyphId}`);
        },
        renderContent: () => {
            const outer = document.createElement('div');
            const wrapper = document.createElement('div');
            wrapper.className = 'glyph-content';
            outer.appendChild(wrapper);
            if (attestation && attrs) {
                wrapper.appendChild(buildSigmaReport(attestation, attrs));
            }
            return outer;
        },
    });

    glyphRun.openGlyph(glyphId);
    log.debug(SEG.GLYPH, `[SigmaGlyph] Spawned ${glyphId} as window`);
}

// ─── Result line rendering (for AX/SE) ──────────────────────

/** Render a sigma attestation as a one-line summary for result lists */
export function renderSigmaResultLine(attestation: Attestation): HTMLElement {
    const attrs = parseDistillAttrs(attestation);
    const total = attrs?._total || attrs?._count || 0;
    const predicate = extractPredicate(attestation);

    const item = document.createElement('div');
    item.className = 'ax-glyph-result-item has-tooltip';
    item.style.padding = '8px';
    item.style.marginBottom = '4px';
    item.style.backgroundColor = 'rgba(80, 60, 30, 0.35)';
    item.style.borderRadius = '2px';
    item.style.cursor = 'pointer';

    item.dataset.attestation = JSON.stringify(attestation);
    item.addEventListener('dblclick', (e) => {
        e.stopPropagation();
        spawnSigmaGlyph(attestation, e.clientX, e.clientY);
    });

    const text = document.createElement('div');
    text.style.fontSize = '11px';
    text.style.fontFamily = 'monospace';
    text.style.wordBreak = 'break-word';
    text.style.overflowWrap = 'break-word';

    const sigmaSpan = document.createElement('span');
    sigmaSpan.style.color = AMBER;
    sigmaSpan.style.fontWeight = 'bold';
    sigmaSpan.textContent = `${Sigma} `;

    const countSpan = document.createElement('span');
    countSpan.style.color = AMBER_VALUE;
    countSpan.textContent = `${formatNum(total)} obs`;

    const sep1 = document.createElement('span');
    sep1.style.color = AMBER_DIM;
    sep1.textContent = ' · ';

    const predSpan = document.createElement('span');
    predSpan.style.color = AMBER_VALUE;
    predSpan.textContent = predicate;

    text.append(sigmaSpan, countSpan, sep1, predSpan);

    // Time range
    if (attrs?._first_seen && attrs?._last_seen) {
        const sep2 = document.createElement('span');
        sep2.style.color = AMBER_DIM;
        sep2.textContent = ' · ';

        const timeSpan = document.createElement('span');
        timeSpan.style.color = AMBER_DIM;
        timeSpan.textContent = `${formatDate(attrs._first_seen)} – ${formatDate(attrs._last_seen)}`;

        text.append(sep2, timeSpan);
    }

    // Subjects count
    if (attrs?._subjects_count) {
        const sep3 = document.createElement('span');
        sep3.style.color = AMBER_DIM;
        sep3.textContent = ' · ';

        const subSpan = document.createElement('span');
        subSpan.style.color = AMBER_DIM;
        subSpan.textContent = `${attrs._subjects_count} subjects`;

        text.append(sep3, subSpan);
    }

    item.dataset.tooltip = attestation.id || 'sigma';
    item.appendChild(text);

    return item;
}
