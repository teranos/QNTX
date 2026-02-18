/**
 * Attestation Glyph (+) — view a single attestation's full structure on canvas
 *
 * Opened via double-click on attestation result items in AX or SE glyphs.
 * Title bar IS the triple (subjects is predicates of contexts), draggable.
 * Content area shows metadata and attributes.
 * Read-only — no editing, no drag-from-results (v1).
 */

import type { Glyph } from './glyph';
import type { Attestation } from '../../generated/proto/plugin/grpc/protocol/atsstore';
import { AS } from '@generated/sym.js';
import { log, SEG } from '../../logger';
import { canvasPlaced } from './manifestations/canvas-placed';
import { uiState } from '../../state/ui';
import { getGlyphTypeBySymbol } from './glyph-registry';

// Muted azure — desaturated toward gray, dark title bar
const AZURE = '#adbcc1';
const AZURE_DARK = '#10161d';
const AZURE_KEYWORD = '#919599';
const AZURE_VALUE = '#d7dee3';

/**
 * Create an Attestation glyph using canvasPlaced().
 * Title bar is the triple itself — entire bar is draggable.
 */
export function createAttestationGlyph(glyph: Glyph): HTMLElement {
    // Parse attestation early — needed for the title bar triple
    let attestation: Attestation | null = null;
    try {
        if (glyph.content) {
            attestation = JSON.parse(glyph.content);
        }
    } catch (err) {
        log.warn(SEG.GLYPH, `[AsGlyph] Failed to parse attestation content for ${glyph.id}:`, err);
    }

    // Title bar: + symbol + triple text — whole bar is the drag handle
    const titleBar = document.createElement('div');
    titleBar.className = 'canvas-glyph-title-bar';
    titleBar.style.padding = '3px 8px';
    titleBar.style.backgroundColor = AZURE_DARK;
    titleBar.style.display = 'flex';
    titleBar.style.alignItems = 'baseline';
    titleBar.style.gap = '6px';

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
        tripleText.style.overflow = 'hidden';
        tripleText.style.textOverflow = 'ellipsis';

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

    const { element } = canvasPlaced({
        glyph,
        className: 'canvas-attestation-glyph',
        defaults: { x: 200, y: 200, width: 420, height: 300 },
        resizable: true,
        logLabel: 'AsGlyph',
    });
    element.style.minWidth = '280px';
    element.style.minHeight = '120px';

    element.appendChild(titleBar);

    // Content container: metadata + attributes
    const content = document.createElement('div');
    content.style.flex = '1';
    content.style.overflow = 'auto';
    content.style.padding = '4px 8px';
    content.style.backgroundColor = 'rgba(25, 25, 30, 0.95)';
    content.style.borderTop = '1px solid var(--border-color)';
    content.style.fontSize = '12px';
    content.style.fontFamily = 'monospace';

    if (!attestation) {
        const empty = document.createElement('div');
        empty.style.color = 'var(--text-secondary)';
        empty.style.textAlign = 'center';
        empty.style.padding = '20px';
        empty.textContent = 'No attestation data';
        content.appendChild(empty);
        element.appendChild(content);
        return element;
    }

    // Metadata section
    const metaSection = document.createElement('div');
    metaSection.style.fontSize = '11px';
    metaSection.style.color = 'var(--text-secondary)';
    metaSection.style.marginBottom = '4px';
    metaSection.style.paddingBottom = '4px';
    metaSection.style.borderBottom = '1px solid var(--border-color)';
    metaSection.style.lineHeight = '1.6';

    const metaLines: string[] = [];
    if (attestation.actors && attestation.actors.length > 0) {
        metaLines.push(`actors: ${attestation.actors.join(', ')}`);
    }
    if (attestation.source) {
        metaLines.push(`source: ${attestation.source}`);
    }
    if (attestation.timestamp) {
        metaLines.push(`timestamp: ${formatTimestamp(attestation.timestamp)}`);
    }
    if (attestation.created_at) {
        metaLines.push(`created: ${formatTimestamp(attestation.created_at)}`);
    }
    if (attestation.id) {
        metaLines.push(`id: ${attestation.id}`);
    }
    metaSection.textContent = metaLines.join('\n');
    metaSection.style.whiteSpace = 'pre-wrap';
    content.appendChild(metaSection);

    // Attributes section
    if (attestation.attributes) {
        try {
            const attrs = typeof attestation.attributes === 'string'
                ? JSON.parse(attestation.attributes)
                : attestation.attributes;

            if (typeof attrs === 'object' && attrs !== null && Object.keys(attrs).length > 0) {
                for (const [key, value] of Object.entries(attrs)) {
                    const attrRow = document.createElement('div');
                    attrRow.style.marginBottom = '4px';

                    const keyLabel = document.createElement('div');
                    keyLabel.style.fontSize = '10px';
                    keyLabel.style.color = 'var(--text-secondary)';
                    keyLabel.style.marginBottom = '2px';
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
            }
        } catch {
            // Ignore parse errors for attributes
        }
    }

    element.appendChild(content);

    log.debug(SEG.GLYPH, `[AsGlyph] Created attestation glyph ${glyph.id}`);

    return element;
}

/**
 * Spawn an attestation glyph on the canvas from an attestation object.
 * Called by double-click handlers in AX and SE glyphs.
 */
export function spawnAttestationGlyph(attestation: Attestation, mouseX?: number, mouseY?: number): void {
    const contentLayer = document.querySelector('.canvas-content-layer') as HTMLElement;
    if (!contentLayer) {
        log.warn(SEG.GLYPH, '[AsGlyph] Cannot spawn: no canvas-content-layer found');
        return;
    }

    const glyphId = `as-${crypto.randomUUID()}`;

    // Position near the double-click location, offset so glyph doesn't cover the source
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

    // Persist
    const rect = glyphElement.getBoundingClientRect();
    uiState.addCanvasGlyph({
        id: glyphId,
        symbol: AS,
        x,
        y,
        width: Math.round(rect.width) || 420,
        height: Math.round(rect.height) || 300,
        content: JSON.stringify(attestation),
    });

    log.debug(SEG.GLYPH, `[AsGlyph] Spawned attestation glyph ${glyphId} at (${x}, ${y})`);
}

function formatTimestamp(unix: number): string {
    if (!unix) return 'N/A';
    try {
        return new Date(unix * 1000).toLocaleString();
    } catch {
        return String(unix);
    }
}
