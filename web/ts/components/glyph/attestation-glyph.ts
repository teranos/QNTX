/**
 * Attestation Glyph (+) — view a single attestation on canvas
 *
 * Opened via double-click on attestation result items in AX or SE glyphs.
 * Title bar IS the triple (subjects is predicates of contexts).
 * Attributes shown below title bar only when present.
 * No attributes → compact title-bar-only glyph.
 * Metadata (actors, source, timestamps, id) hidden by default —
 * revealed via hover pill at bottom center of title bar.
 */

import type { Glyph } from './glyph';
import type { Attestation } from '../../generated/proto/plugin/grpc/protocol/atsstore';
import { AS } from '@generated/sym.js';
import { log, SEG } from '../../logger';
import { canvasPlaced } from './manifestations/canvas-placed';
import { morphCanvasPlacedToWindow, placeWindowOnCanvas } from './manifestations/canvas-window';
import { isInWindowState } from './dataset';
import { preventDrag } from './glyph-interaction';
import { uiState } from '../../state/ui';
import { getGlyphTypeBySymbol } from './glyph-registry';
import { glyphRun } from './run';

// Muted azure — desaturated toward gray
const AZURE = '#adbcc1';
const AZURE_KEYWORD = '#919599';
const AZURE_VALUE = '#d7dee3';

/**
 * Parse attributes from attestation, returns non-empty object or null.
 */
function parseAttributes(attestation: Attestation): Record<string, unknown> | null {
    if (!attestation.attributes) return null;
    try {
        const attrs = typeof attestation.attributes === 'string'
            ? JSON.parse(attestation.attributes)
            : attestation.attributes;
        if (typeof attrs === 'object' && attrs !== null && Object.keys(attrs).length > 0) {
            return attrs;
        }
    } catch { /* ignore */ }
    return null;
}

/**
 * Build metadata lines from attestation fields.
 */
function buildMetaLines(attestation: Attestation): string[] {
    const lines: string[] = [];
    if (attestation.actors && attestation.actors.length > 0) {
        lines.push(`actors: ${attestation.actors.join(', ')}`);
    }
    if (attestation.source) {
        lines.push(`source: ${attestation.source}`);
    }
    if (attestation.timestamp) {
        lines.push(`timestamp: ${formatTimestamp(attestation.timestamp)}`);
    }
    if (attestation.created_at) {
        lines.push(`created: ${formatTimestamp(attestation.created_at)}`);
    }
    if (attestation.signer_did) {
        // Cyan color for signer (between green and purple)
        lines.push(`<span style="color: #00d4aa">signer: ${attestation.signer_did}</span>`);
    }
    if (attestation.signature && attestation.signature.length > 0) {
        lines.push(`signature: ${attestation.signature.length} bytes`);
    }
    if (attestation.id) {
        lines.push(`id: ${attestation.id}`);
    }
    return lines;
}

/**
 * Create an Attestation glyph.
 * Title bar = triple. Attributes below if present. Metadata behind hover pill.
 */
export function createAttestationGlyph(glyph: Glyph): HTMLElement {
    let attestation: Attestation | null = null;
    try {
        if (glyph.content) {
            attestation = JSON.parse(glyph.content);
        }
    } catch (err) {
        log.warn(SEG.GLYPH, `[AsGlyph] Failed to parse attestation content for ${glyph.id}:`, err);
    }

    const attrs = attestation ? parseAttributes(attestation) : null;

    // Title bar: + symbol + triple + expand button + metadata pill
    const titleBar = document.createElement('div');
    titleBar.className = 'glyph-title-bar glyph-title-bar--auto';
    titleBar.style.position = 'relative';

    const symbol = document.createElement('span');
    symbol.textContent = AS;
    symbol.style.fontWeight = 'bold';
    symbol.style.flexShrink = '0';
    symbol.style.color = AZURE;
    titleBar.appendChild(symbol);

    if (attestation) {
        const subjects = attestation.subjects?.join(', ') || 'N/A';
        const predicates = attestation.predicates?.join(', ') || 'N/A';
        const contexts = attestation.contexts?.join(', ') || 'N/A';

        const tripleText = document.createElement('span');
        tripleText.style.fontSize = '12px';
        tripleText.style.fontFamily = 'monospace';
        tripleText.style.lineHeight = '1.4';
        tripleText.style.wordBreak = 'break-word';

        const subjectSpan = document.createElement('span');
        subjectSpan.style.color = AZURE_VALUE;
        subjectSpan.textContent = subjects;

        const isSpan = document.createElement('span');
        isSpan.style.color = AZURE_KEYWORD;
        isSpan.textContent = ' is ';

        const predSpan = document.createElement('span');
        predSpan.style.color = AZURE_VALUE;
        predSpan.textContent = predicates;

        const ofSpan = document.createElement('span');
        ofSpan.style.color = AZURE_KEYWORD;
        ofSpan.textContent = ' of ';

        const ctxSpan = document.createElement('span');
        ctxSpan.style.color = AZURE_VALUE;
        ctxSpan.textContent = contexts;

        tripleText.append(subjectSpan, isSpan, predSpan, ofSpan, ctxSpan);
        titleBar.appendChild(tripleText);
    }

    // Expand/place button
    const expandBtn = document.createElement('button');
    expandBtn.className = 'titlebar-btn';
    expandBtn.textContent = '\u2B06'; // ⬆
    expandBtn.title = 'Expand to window';
    expandBtn.style.flexShrink = '0';
    expandBtn.style.marginLeft = 'auto';
    preventDrag(expandBtn);
    titleBar.appendChild(expandBtn);

    // Metadata pill — appears on hover at bottom center of title bar
    if (attestation) {
        const metaLines = buildMetaLines(attestation);
        if (metaLines.length > 0) {
            const pill = document.createElement('div');
            pill.className = 'as-meta-pill';

            const metaPopover = document.createElement('div');
            metaPopover.className = 'as-meta-popover';
            metaPopover.innerHTML = metaLines.join('<br>');

            pill.appendChild(metaPopover);
            titleBar.appendChild(pill);
        }
    }

    // Compact when no attributes, expanded when attributes present
    const hasContent = !!attrs;

    const { element } = canvasPlaced({
        glyph,
        className: 'canvas-attestation-glyph',
        defaults: { x: 200, y: 200, width: 420, height: hasContent ? 200 : 28 },
        resizable: hasContent,
        useMinHeight: true,
        logLabel: 'AsGlyph',
    });
    element.style.minWidth = '200px';

    element.appendChild(titleBar);

    // Attributes content — only when there are attributes to show
    if (attestation && attrs) {
        const content = document.createElement('div');
        content.className = 'glyph-content-area';
        content.style.padding = '4px 8px';
        content.style.backgroundColor = 'rgba(25, 25, 30, 0.95)';
        content.style.borderTop = '1px solid var(--border)';
        content.style.fontSize = '12px';
        content.style.fontFamily = 'monospace';

        for (const [key, value] of Object.entries(attrs)) {
            const attrRow = document.createElement('div');
            attrRow.style.marginBottom = '4px';

            const keyLabel = document.createElement('div');
            keyLabel.style.fontSize = '10px';
            keyLabel.style.color = 'var(--text-secondary)';
            keyLabel.style.marginBottom = '1px';
            keyLabel.textContent = key;
            attrRow.appendChild(keyLabel);

            const valueEl = document.createElement('div');
            valueEl.style.fontSize = '12px';

            if (typeof value === 'string') {
                valueEl.style.color = AZURE_VALUE;
                valueEl.style.whiteSpace = 'pre-wrap';
                valueEl.style.wordBreak = 'break-word';
                valueEl.style.lineHeight = '1.5';
                valueEl.textContent = value;
            } else {
                valueEl.style.color = 'var(--text-secondary)';
                valueEl.style.whiteSpace = 'pre-wrap';
                valueEl.style.wordBreak = 'break-word';
                valueEl.textContent = JSON.stringify(value, null, 2);
            }

            attrRow.appendChild(valueEl);
            content.appendChild(attrRow);
        }

        element.appendChild(content);
    }

    // Morph wiring: canvas ↔ window ↔ tray
    const title = attestation
        ? `${attestation.subjects?.join(', ') || '?'} is ${attestation.predicates?.join(', ') || '?'}`
        : 'Attestation';

    expandBtn.addEventListener('click', () => {
        if (isInWindowState(element)) {
            placeWindowOnCanvas(element, {
                onRestoreComplete: () => {
                    expandBtn.textContent = '\u2B06'; // ⬆
                    expandBtn.title = 'Expand to window';
                    log.debug(SEG.GLYPH, `[AsGlyph] Placed on canvas ${glyph.id}`);
                },
            });
            return;
        }

        const canvas = element.closest('.canvas-workspace') as HTMLElement | null;
        const canvasId = (canvas?.closest('[data-canvas-id]') as HTMLElement | null)?.dataset?.canvasId ?? 'canvas-workspace';

        morphCanvasPlacedToWindow(element, {
            title,
            canvasId,
            onClose: () => {
                element.remove();
                uiState.removeCanvasGlyph(glyph.id);
                log.debug(SEG.GLYPH, `[AsGlyph] Closed from window ${glyph.id}`);
            },
            onMinimize: (el: HTMLElement) => {
                glyphRun.adopt(el, {
                    id: glyph.id,
                    title,
                    symbol: AS,
                    renderContent: () => buildAttestationContent(attestation, attrs),
                    onClose: () => {
                        log.debug(SEG.GLYPH, `[AsGlyph] Closed from tray ${glyph.id}`);
                    },
                });
                log.debug(SEG.GLYPH, `[AsGlyph] Minimized to tray ${glyph.id}`);
            },
            onRestoreComplete: () => {
                log.debug(SEG.GLYPH, `[AsGlyph] Restored to canvas ${glyph.id}`);
            },
        });

        expandBtn.textContent = '\u2B07'; // ⬇
        expandBtn.title = 'Place on canvas';
    });

    log.debug(SEG.GLYPH, `[AsGlyph] Created attestation glyph ${glyph.id} (attrs: ${hasContent})`);

    return element;
}

/**
 * Spawn an attestation glyph on the canvas from an attestation object.
 */
export function spawnAttestationGlyph(attestation: Attestation, mouseX?: number, mouseY?: number): void {
    const contentLayer = document.querySelector('.canvas-content-layer') as HTMLElement;
    if (!contentLayer) {
        log.warn(SEG.GLYPH, '[AsGlyph] Cannot spawn: no canvas-content-layer found');
        return;
    }

    const glyphId = `as-${crypto.randomUUID()}`;
    const attrs = parseAttributes(attestation);

    const layerRect = contentLayer.getBoundingClientRect();
    const x = mouseX !== undefined ? Math.round(mouseX - layerRect.left + 20) : Math.round(window.innerWidth / 2 - 210);
    const y = mouseY !== undefined ? Math.round(mouseY - layerRect.top - 20) : Math.round(window.innerHeight / 2 - 150);

    const glyph: Glyph = {
        id: glyphId,
        title: 'Attestation',
        symbol: AS,
        x,
        y,
        content: JSON.stringify(attestation),
        renderContent: () => document.createElement('div'),
    };

    const entry = getGlyphTypeBySymbol(AS);
    if (!entry) {
        log.error(SEG.GLYPH, '[AsGlyph] AS not found in glyph registry');
        return;
    }

    const glyphElement = entry.render(glyph) as HTMLElement;
    contentLayer.appendChild(glyphElement);

    const rect = glyphElement.getBoundingClientRect();
    uiState.addCanvasGlyph({
        id: glyphId,
        symbol: AS,
        x,
        y,
        width: Math.round(rect.width) || 420,
        height: Math.round(rect.height) || (attrs ? 200 : 28),
        content: JSON.stringify(attestation),
    });

    log.debug(SEG.GLYPH, `[AsGlyph] Spawned attestation glyph ${glyphId} at (${x}, ${y})`);
}

/**
 * Build attestation content for tray restoration.
 */
function buildAttestationContent(
    attestation: Attestation | null,
    attrs: Record<string, unknown> | null,
): HTMLElement {
    const wrapper = document.createElement('div');
    wrapper.className = 'glyph-content';

    if (attestation) {
        // Triple
        const triple = document.createElement('div');
        triple.style.padding = '8px';
        triple.style.fontSize = '12px';
        triple.style.fontFamily = 'monospace';
        triple.style.color = AZURE_VALUE;
        triple.style.wordBreak = 'break-word';
        const s = attestation.subjects?.join(', ') || 'N/A';
        const p = attestation.predicates?.join(', ') || 'N/A';
        const c = attestation.contexts?.join(', ') || 'N/A';
        triple.textContent = `${s} is ${p} of ${c}`;
        wrapper.appendChild(triple);

        // Metadata
        const metaLines = buildMetaLines(attestation);
        if (metaLines.length > 0) {
            const meta = document.createElement('div');
            meta.style.padding = '4px 8px';
            meta.style.fontSize = '11px';
            meta.style.color = 'var(--text-secondary)';
            meta.innerHTML = metaLines.join('<br>');
            wrapper.appendChild(meta);
        }

        // Attributes
        if (attrs) {
            const attrDiv = document.createElement('div');
            attrDiv.style.padding = '4px 8px';
            attrDiv.style.borderTop = '1px solid var(--border)';
            attrDiv.style.fontSize = '12px';
            attrDiv.style.fontFamily = 'monospace';
            for (const [key, value] of Object.entries(attrs)) {
                const row = document.createElement('div');
                row.style.marginBottom = '4px';
                const keyEl = document.createElement('div');
                keyEl.style.fontSize = '10px';
                keyEl.style.color = 'var(--text-secondary)';
                keyEl.textContent = key;
                const valEl = document.createElement('div');
                valEl.style.color = AZURE_VALUE;
                valEl.style.whiteSpace = 'pre-wrap';
                valEl.style.wordBreak = 'break-word';
                valEl.textContent = typeof value === 'string' ? value : JSON.stringify(value, null, 2);
                row.appendChild(keyEl);
                row.appendChild(valEl);
                attrDiv.appendChild(row);
            }
            wrapper.appendChild(attrDiv);
        }
    }

    return wrapper;
}

function formatTimestamp(unix: number): string {
    if (!unix) return 'N/A';
    try {
        return new Date(unix * 1000).toLocaleString();
    } catch {
        return String(unix);
    }
}
