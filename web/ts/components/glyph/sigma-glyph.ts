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
import { wireExpandToWindow, isInWindowState, glyphRun, canvasPlaced, preventDrag } from '@qntx/glyphs';
import type { Attestation } from '../../generated/proto/plugin/grpc/protocol/atsstore';
import { Sigma, Watcher } from '@generated/sym.js';
import { getWatchersByPredicate, eyeStyle } from '../../watcher-predicates';
import { log, SEG } from '../../logger';
import { spawnOnCanvasDragging } from './spawn-on-canvas';
import { el } from '../../html-utils';

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
    _version?: string | { count: number; values: string[] };
    _rust_version?: string | { count: number; values: string[] };
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

/** Build watcher eye suffix for a predicate (checks both raw and stripped forms) */
function watcherEyes(predicate: string): string {
    const map = getWatchersByPredicate();
    const info = map.get(predicate) || map.get('distill:' + predicate);
    if (!info) return '';
    return ' ' + Watcher.repeat(info.names.length);
}

/** Create a styled watcher eye span element — spice-blue, dilation-aware */
function watcherEyeSpan(predicate: string): HTMLSpanElement | null {
    const map = getWatchersByPredicate();
    const info = map.get(predicate) || map.get('distill:' + predicate);
    if (!info) return null;
    const s = eyeStyle(info);
    const span = el('span', {
        text: Watcher.repeat(info.names.length),
        style: { color: s.color, textShadow: s.shadow, cursor: 'default', marginLeft: '3px' },
    });
    span.title = info.names.join(', ');
    return span;
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

/** Check if a value is a string aggregate {frequencies, count} or legacy {values, count} */
function isStringAggregate(v: unknown): v is { frequencies?: Record<string, number>; values?: string[]; unplaced?: string[]; count: number } {
    if (typeof v !== 'object' || v === null) return false;
    const o = v as Record<string, unknown>;
    return typeof o.count === 'number' && (typeof o.frequencies === 'object' || Array.isArray(o.values));
}

/** Keys that are structural metadata, not domain data */
const META_KEYS = new Set([
    '_distill', '_count', '_total', '_first_seen', '_last_seen',
    '_subjects_count', '_subjects_sample', '_version', '_rust_version',
    '_histogram', '_actors_count', '_actors_sample',
]);

// ─── Rendering ───────────────────────────────────────────────

/** Build the sigma report content (used in both canvas and window) */
function buildSigmaReport(attestation: Attestation, attrs: DistillAttrs): HTMLElement {
    const container = el('div', {
        style: { padding: '8px 12px', fontFamily: 'monospace', fontSize: '12px' },
    });

    // ── Header: big number + predicate ──
    const header = el('div', { style: { marginBottom: '8px' } });

    const total = attrs._total || attrs._count || 0;
    const bigNumber = el('div', {
        text: `${Sigma} ${formatNum(total)}`,
        style: { fontSize: '24px', fontWeight: 'bold', color: AMBER, lineHeight: '1.2' },
    });
    header.appendChild(bigNumber);

    const batchNote = attrs._count && attrs._count !== total ? ` (batch of ${attrs._count})` : '';
    const subtitle = el('div', {
        text: `observations${batchNote}`,
        style: { fontSize: '11px', color: AMBER_DIM },
    });
    header.appendChild(subtitle);

    const pred = extractPredicate(attestation);
    const predLine = el('div', {
        text: pred,
        style: { fontSize: '13px', color: AMBER_VALUE, marginTop: '2px' },
    });
    const eye = watcherEyeSpan(pred);
    if (eye) predLine.appendChild(eye);
    header.appendChild(predLine);

    container.appendChild(header);

    // ── Time range ──
    if (attrs._first_seen && attrs._last_seen) {
        const startLabel = el('span', { text: formatDate(attrs._first_seen), style: { color: AMBER_VALUE } });
        const line = el('span', { style: { flex: '1', height: '1px', backgroundColor: AMBER_DIM } });
        const endLabel = el('span', { text: formatDate(attrs._last_seen), style: { color: AMBER_VALUE } });
        const timeRow = el('div', {
            style: {
                marginBottom: '10px', display: 'flex', alignItems: 'center',
                gap: '6px', fontSize: '11px', color: AMBER_DIM,
            },
        }, [startLabel, line, endLabel]);
        container.appendChild(timeRow);
    }

    // ── Histogram ──
    const histogram = (attrs as Record<string, unknown>)['_histogram'] as Record<string, number> | undefined;
    if (histogram && typeof histogram === 'object') {
        const histSection = el('div', { style: { marginBottom: '10px' } });

        const entries = Object.entries(histogram).sort((a, b) => a[0].localeCompare(b[0]));
        if (entries.length > 0) {
            const maxVal = Math.max(...entries.map(e => e[1]));
            const barContainer = el('div', {
                style: { display: 'flex', alignItems: 'flex-end', gap: '1px', height: '60px' },
            });

            for (const [key, val] of entries) {
                const pct = maxVal > 0 ? (val / maxVal) * 100 : 0;
                const bar = el('div', {
                    style: {
                        height: `${pct}%`, minHeight: '1px',
                        backgroundColor: AMBER_BAR, borderRadius: '1px 1px 0 0',
                    },
                });
                bar.title = `${key}: ${formatNum(val)}`;
                barContainer.appendChild(el('div', {
                    style: {
                        flex: '1', height: '100%', display: 'flex',
                        flexDirection: 'column', justifyContent: 'flex-end',
                    },
                }, [bar]));
            }

            histSection.appendChild(barContainer);

            // X-axis labels: first and last key
            const firstKey = el('span', { text: entries[0][0] });
            const lastKey = el('span', { text: entries[entries.length - 1][0] });
            histSection.appendChild(el('div', {
                style: {
                    display: 'flex', justifyContent: 'space-between',
                    fontSize: '9px', color: AMBER_DIM, marginTop: '2px',
                },
            }, [firstKey, lastKey]));

            // Summary line
            const histTotal = entries.reduce((s, e) => s + e[1], 0);
            histSection.appendChild(el('div', {
                text: `${entries.length} buckets · ${formatNum(histTotal)} placed`,
                style: { fontSize: '9px', color: AMBER_DIM, marginTop: '2px' },
            }));
        }

        container.appendChild(histSection);
    }

    // ── Domain attributes (non-meta) ──
    for (const [key, value] of Object.entries(attrs)) {
        if (META_KEYS.has(key)) continue;

        const section = el('div', { style: { marginBottom: '8px' } });

        const label = el('div', {
            text: key,
            style: { fontSize: '10px', color: AMBER_DIM, marginBottom: '3px' },
        });
        section.appendChild(label);

        if (isStringAggregate(value)) {
            if (value.frequencies) {
                // Pie chart + legend for frequency data
                const entries = Object.entries(value.frequencies)
                    .sort((a, b) => b[1] - a[1]);
                const freqTotal = entries.reduce((s, e) => s + e[1], 0);

                const row = el('div', {
                    style: { display: 'flex', alignItems: 'center', gap: '10px' },
                });

                // SVG pie chart
                const size = 48;
                const r = 20;
                const cx = size / 2;
                const cy = size / 2;
                const svg = document.createElementNS('http://www.w3.org/2000/svg', 'svg');
                svg.setAttribute('width', String(size));
                svg.setAttribute('height', String(size));
                svg.style.flexShrink = '0';

                const pieColors = ['#d4a574', '#c49a6c', '#a07850', '#8a7560', '#6e5a40', '#e8d0b4', '#b8956a', '#927048'];
                let startAngle = -Math.PI / 2;
                const slices = entries.slice(0, 8);
                const sliceTotal = slices.reduce((s, e) => s + e[1], 0);
                const otherCount = freqTotal - sliceTotal;

                for (let i = 0; i < slices.length; i++) {
                    const [, freq] = slices[i];
                    const sliceAngle = (freq / freqTotal) * Math.PI * 2;
                    const endAngle = startAngle + sliceAngle;
                    const largeArc = sliceAngle > Math.PI ? 1 : 0;
                    const x1 = cx + r * Math.cos(startAngle);
                    const y1 = cy + r * Math.sin(startAngle);
                    const x2 = cx + r * Math.cos(endAngle);
                    const y2 = cy + r * Math.sin(endAngle);

                    if (slices.length === 1 && otherCount === 0) {
                        // Full circle
                        const circle = document.createElementNS('http://www.w3.org/2000/svg', 'circle');
                        circle.setAttribute('cx', String(cx));
                        circle.setAttribute('cy', String(cy));
                        circle.setAttribute('r', String(r));
                        circle.setAttribute('fill', pieColors[0]);
                        svg.appendChild(circle);
                    } else {
                        const path = document.createElementNS('http://www.w3.org/2000/svg', 'path');
                        path.setAttribute('d', `M${cx},${cy} L${x1},${y1} A${r},${r} 0 ${largeArc},1 ${x2},${y2} Z`);
                        path.setAttribute('fill', pieColors[i % pieColors.length]);
                        svg.appendChild(path);
                    }
                    startAngle = endAngle;
                }

                if (otherCount > 0) {
                    const sliceAngle = (otherCount / freqTotal) * Math.PI * 2;
                    const endAngle = startAngle + sliceAngle;
                    const largeArc = sliceAngle > Math.PI ? 1 : 0;
                    const x1 = cx + r * Math.cos(startAngle);
                    const y1 = cy + r * Math.sin(startAngle);
                    const x2 = cx + r * Math.cos(endAngle);
                    const y2 = cy + r * Math.sin(endAngle);
                    const path = document.createElementNS('http://www.w3.org/2000/svg', 'path');
                    path.setAttribute('d', `M${cx},${cy} L${x1},${y1} A${r},${r} 0 ${largeArc},1 ${x2},${y2} Z`);
                    path.setAttribute('fill', '#4a3d30');
                    svg.appendChild(path);
                }

                row.appendChild(svg);

                // Legend
                const legend = el('div', {
                    style: { display: 'flex', flexWrap: 'wrap', gap: '3px 8px', fontSize: '10px' },
                });

                for (let i = 0; i < slices.length; i++) {
                    const [v, freq] = slices[i];
                    const pct = freqTotal > 0 ? Math.round((freq / freqTotal) * 100) : 0;
                    legend.appendChild(el('span', {
                        text: `${v} ${pct}%`,
                        style: { color: pieColors[i % pieColors.length] },
                    }));
                }
                if (otherCount > 0) {
                    const remaining = entries.length - slices.length;
                    legend.appendChild(el('span', {
                        text: `+${remaining} more ${Math.round((otherCount / freqTotal) * 100)}%`,
                        style: { color: '#4a3d30' },
                    }));
                }
                if (value.unplaced && value.unplaced.length > 0) {
                    legend.appendChild(el('span', {
                        text: `+${value.unplaced.length} unplaced`,
                        style: { color: AMBER_DIM },
                    }));
                }

                row.appendChild(legend);
                section.appendChild(row);
            } else if (value.values) {
                // Legacy format: values without frequencies — inline tags
                const tagRow = el('div', {
                    style: { display: 'flex', flexWrap: 'wrap', gap: '4px', alignItems: 'center' },
                });

                for (const v of value.values.slice(0, 20)) {
                    tagRow.appendChild(el('span', {
                        text: String(v),
                        style: {
                            fontSize: '11px', color: AMBER_VALUE, padding: '1px 5px',
                            backgroundColor: AMBER_BAR_BG, borderRadius: '2px',
                        },
                    }));
                }
                if (value.count > 20) {
                    tagRow.appendChild(el('span', {
                        text: `+${value.count - 20} more`,
                        style: { fontSize: '10px', color: AMBER_DIM },
                    }));
                }
                section.appendChild(tagRow);
            }
        } else if (isNumberAggregate(value)) {
            // Number aggregate — range bar
            const minLabel = el('span', {
                text: formatNum(value.min),
                style: { fontSize: '11px', color: AMBER_VALUE },
            });

            const bar = el('div', {
                style: {
                    flex: '1', height: '4px', backgroundColor: AMBER_BAR_BG,
                    borderRadius: '2px', position: 'relative',
                },
            });

            // Average marker
            if (value.count > 0) {
                const avg = value.sum / value.count;
                const range = value.max - value.min;
                if (range > 0) {
                    const pct = ((avg - value.min) / range) * 100;
                    bar.appendChild(el('div', {
                        style: {
                            position: 'absolute', left: `${pct}%`, top: '-2px',
                            width: '2px', height: '8px', backgroundColor: AMBER_BAR, borderRadius: '1px',
                        },
                    }));
                }
            }

            const maxLabel = el('span', {
                text: formatNum(value.max),
                style: { fontSize: '11px', color: AMBER_VALUE },
            });

            const avg = value.count > 0 ? Math.round(value.sum / value.count) : 0;
            const avgLabel = el('span', {
                text: `avg ${formatNum(avg)}`,
                style: { fontSize: '10px', color: AMBER_DIM, flexShrink: '0' },
            });

            const rangeRow = el('div', {
                style: { display: 'flex', alignItems: 'center', gap: '6px' },
            }, [minLabel, bar, maxLabel, avgLabel]);
            section.appendChild(rangeRow);
        } else if (typeof value === 'string' || typeof value === 'number' || typeof value === 'boolean') {
            // Scalar constant
            section.appendChild(el('div', {
                text: String(value),
                style: { fontSize: '11px', color: AMBER_VALUE },
            }));
        } else {
            // Unknown shape — JSON fallback
            section.appendChild(el('div', {
                text: JSON.stringify(value, null, 2),
                style: {
                    fontSize: '11px', color: AMBER_DIM,
                    whiteSpace: 'pre-wrap', wordBreak: 'break-word',
                },
            }));
        }

        container.appendChild(section);
    }

    // ── Subjects ──
    if (attrs._subjects_count || attrs._subjects_sample) {
        const section = el('div', { style: { marginBottom: '8px' } });

        const label = el('div', {
            text: `${attrs._subjects_count || 0} subjects`,
            style: { fontSize: '10px', color: AMBER_DIM, marginBottom: '3px' },
        });
        section.appendChild(label);

        if (attrs._subjects_sample && attrs._subjects_sample.length > 0) {
            section.appendChild(el('div', {
                text: attrs._subjects_sample.join(' · '),
                style: { fontSize: '11px', color: AMBER_VALUE, wordBreak: 'break-word' },
            }));
        }

        container.appendChild(section);
    }

    // ── Version footer ──
    if (attrs._version || attrs._rust_version) {
        const parts: string[] = [];
        if (attrs._rust_version && typeof attrs._rust_version === 'string') parts.push(attrs._rust_version);
        if (attrs._version && typeof attrs._version === 'string') parts.push(attrs._version.split(' ')[0]); // just version, not hash
        const footer = el('div', {
            text: parts.join(' · '),
            style: { fontSize: '9px', color: AMBER_DIM, marginTop: '4px', textAlign: 'right' },
        });
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
    const titleBar = el('div', {
        class: 'glyph-title-bar glyph-title-bar--auto',
        style: { position: 'relative' },
    });

    const symbolEl = el('span', {
        class: 'glyph-symbol',
        text: Sigma,
        style: { fontWeight: 'bold', flexShrink: '0', color: AMBER },
    });
    titleBar.appendChild(symbolEl);

    if (attestation && attrs) {
        const total = attrs._total || attrs._count || 0;
        const titleText = el('span', {
            text: `${formatNum(total)} obs · ${extractPredicate(attestation)}${watcherEyes(extractPredicate(attestation))}`,
            style: { fontSize: '12px', fontFamily: 'monospace', color: AMBER_VALUE },
        });
        titleBar.appendChild(titleText);
    }

    const expandBtn = el('button', {
        class: 'titlebar-btn',
        text: '\u2B06',
        style: { flexShrink: '0', marginLeft: 'auto' },
    });
    expandBtn.title = 'Expand to window';
    preventDrag(expandBtn);
    titleBar.appendChild(expandBtn);

    const { element } = canvasPlaced({
        glyph,
        className: 'canvas-sigma-glyph',
        defaults: { x: 200, y: 200, width: 520, height: 400 },
        resizable: true,
        useMinHeight: true,
        logLabel: 'SigmaGlyph',
    });
    element.style.minWidth = '200px';
    element.appendChild(titleBar);

    // Report content
    if (attestation && attrs) {
        const content = el('div', {
            class: 'glyph-content-area',
            style: {
                backgroundColor: 'rgba(25, 25, 30, 0.95)',
                borderTop: '1px solid var(--border)',
                overflow: 'auto',
            },
        });
        content.appendChild(buildSigmaReport(attestation, attrs));
        element.appendChild(content);
    }

    const title = attestation && attrs
        ? `${Sigma} ${formatNum(attrs._total || attrs._count || 0)} · ${extractPredicate(attestation)}${watcherEyes(extractPredicate(attestation))}`
        : 'Sigma';

    wireExpandToWindow({
        element,
        expandBtn,
        glyphId: glyph.id,
        title,
        symbol: Sigma,
        renderContent: () => {
            const outer = el('div');
            const wrapper = el('div', { class: 'glyph-content' });
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
    spawnOnCanvasDragging({
        symbol: Sigma,
        prefix: 'sigma',
        title: 'Sigma',
        content: JSON.stringify(attestation),
        fallbackWidth: 520,
        fallbackHeight: 400,
    }, mouseX || window.innerWidth / 2, mouseY || window.innerHeight / 2);
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
    const title = `${Sigma} ${formatNum(total)} · ${extractPredicate(attestation)}${watcherEyes(extractPredicate(attestation))}`;

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
            const outer = el('div');
            const wrapper = el('div', { class: 'glyph-content' });
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

    const item = el('div', {
        class: 'ax-glyph-result-item has-tooltip',
        style: {
            padding: '8px', marginBottom: '4px',
            backgroundColor: 'rgba(80, 60, 30, 0.35)',
            borderRadius: '2px', cursor: 'pointer',
        },
    });

    item.dataset.attestation = JSON.stringify(attestation);
    item.addEventListener('dblclick', (e) => {
        e.stopPropagation();
        spawnSigmaGlyph(attestation, e.clientX, e.clientY);
    });

    const text = el('div', {
        style: {
            fontSize: '11px', fontFamily: 'monospace',
            wordBreak: 'break-word', overflowWrap: 'break-word',
        },
    });

    const sigmaSpan = el('span', {
        text: `${Sigma} `,
        style: { color: AMBER, fontWeight: 'bold' },
    });

    const countSpan = el('span', {
        text: `${formatNum(total)} obs`,
        style: { color: AMBER_VALUE },
    });

    const sep1 = el('span', { text: ' · ', style: { color: AMBER_DIM } });

    const predSpan = el('span', { text: predicate, style: { color: AMBER_VALUE } });
    const resultEye = watcherEyeSpan(predicate);

    text.append(sigmaSpan, countSpan, sep1, predSpan);
    if (resultEye) text.appendChild(resultEye);

    // Time range
    if (attrs?._first_seen && attrs?._last_seen) {
        const sep2 = el('span', { text: ' · ', style: { color: AMBER_DIM } });
        const timeSpan = el('span', {
            text: `${formatDate(attrs._first_seen)} – ${formatDate(attrs._last_seen)}`,
            style: { color: AMBER_DIM },
        });

        text.append(sep2, timeSpan);
    }

    // Subjects count
    if (attrs?._subjects_count) {
        const sep3 = el('span', { text: ' · ', style: { color: AMBER_DIM } });
        const subSpan = el('span', {
            text: `${attrs._subjects_count} subjects`,
            style: { color: AMBER_DIM },
        });

        text.append(sep3, subSpan);
    }

    item.dataset.tooltip = attestation.id || 'sigma';
    item.appendChild(text);

    return item;
}
