/**
 * Generic spawn-on-canvas helper for attestation-family glyphs.
 *
 * Eliminates the repeated contentLayer lookup → position calc → registry lookup →
 * render → appendChild → uiState.addCanvasGlyph pattern across attestation,
 * triplet, and sigma glyphs.
 */

import type { Glyph } from '@qntx/glyphs';
import { log, SEG } from '../../logger';
import { uiState } from '../../state/ui';
import { getGlyphTypeBySymbol } from './glyph-registry';

export interface SpawnOpts {
    /** Glyph symbol (used for registry lookup and uiState tracking) */
    symbol: string;
    /** ID prefix (e.g. 'as', 'triplet', 'sigma') */
    prefix: string;
    /** Title for the glyph */
    title: string;
    /** JSON-serializable content */
    content: string;
    /** Fallback width if element has no measured width */
    fallbackWidth?: number;
    /** Fallback height if element has no measured height */
    fallbackHeight?: number;
    /** Mouse position for placement near cursor */
    mouseX?: number;
    mouseY?: number;
}

/**
 * Spawn a glyph on the canvas. Returns the glyph element, or null if
 * the canvas content layer or registry entry is missing.
 */
export function spawnOnCanvas(opts: SpawnOpts): HTMLElement | null {
    const contentLayer = document.querySelector('.canvas-content-layer') as HTMLElement;
    if (!contentLayer) {
        log.warn(SEG.GLYPH, `[${opts.prefix}] Cannot spawn: no canvas-content-layer found`);
        return null;
    }

    const glyphId = `${opts.prefix}-${crypto.randomUUID()}`;
    const layerRect = contentLayer.getBoundingClientRect();
    const fallbackW = opts.fallbackWidth || 420;
    const fallbackH = opts.fallbackHeight || 200;

    const x = opts.mouseX !== undefined
        ? Math.round(opts.mouseX - layerRect.left + 20)
        : Math.round(window.innerWidth / 2 - fallbackW / 2);
    const y = opts.mouseY !== undefined
        ? Math.round(opts.mouseY - layerRect.top - 20)
        : Math.round(window.innerHeight / 2 - fallbackH / 2);

    const glyph: Glyph = {
        id: glyphId,
        title: opts.title,
        symbol: opts.symbol,
        x,
        y,
        content: opts.content,
        renderContent: () => document.createElement('div'),
    };

    const entry = getGlyphTypeBySymbol(opts.symbol);
    if (!entry) {
        log.error(SEG.GLYPH, `[${opts.prefix}] Symbol ${opts.symbol} not found in glyph registry`);
        return null;
    }

    const glyphElement = entry.render(glyph) as HTMLElement;
    contentLayer.appendChild(glyphElement);

    const rect = glyphElement.getBoundingClientRect();
    uiState.addCanvasGlyph({
        id: glyphId,
        symbol: opts.symbol,
        x,
        y,
        width: Math.round(rect.width) || fallbackW,
        height: Math.round(rect.height) || fallbackH,
        content: opts.content,
    });

    log.debug(SEG.GLYPH, `[${opts.prefix}] Spawned ${glyphId} at (${x}, ${y})`);
    return glyphElement;
}
