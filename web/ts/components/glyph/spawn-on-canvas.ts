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

/**
 * Spawn a glyph attached to the cursor. The glyph follows the mouse
 * until the user clicks to place it down on the canvas.
 */
export function spawnOnCanvasDragging(opts: Omit<SpawnOpts, 'mouseX' | 'mouseY'>, startX: number, startY: number): void {
    const contentLayer = document.querySelector('.canvas-content-layer') as HTMLElement;
    if (!contentLayer) {
        log.warn(SEG.GLYPH, `[${opts.prefix}] Cannot spawn: no canvas-content-layer found`);
        return;
    }

    const glyphId = `${opts.prefix}-${crypto.randomUUID()}`;
    const layerRect = contentLayer.getBoundingClientRect();
    const fallbackW = opts.fallbackWidth || 420;
    const fallbackH = opts.fallbackHeight || 200;

    const glyph: Glyph = {
        id: glyphId,
        title: opts.title,
        symbol: opts.symbol,
        x: Math.round(startX - layerRect.left),
        y: Math.round(startY - layerRect.top),
        content: opts.content,
        renderContent: () => document.createElement('div'),
    };

    const entry = getGlyphTypeBySymbol(opts.symbol);
    if (!entry) {
        log.error(SEG.GLYPH, `[${opts.prefix}] Symbol ${opts.symbol} not found in glyph registry`);
        return;
    }

    const glyphElement = entry.render(glyph) as HTMLElement;
    glyphElement.style.opacity = '0.7';
    glyphElement.style.pointerEvents = 'none';
    contentLayer.appendChild(glyphElement);

    const onMove = (e: MouseEvent) => {
        const lx = Math.round(e.clientX - layerRect.left);
        const ly = Math.round(e.clientY - layerRect.top);
        glyphElement.style.left = `${lx}px`;
        glyphElement.style.top = `${ly}px`;
    };

    const onPlace = (e: MouseEvent) => {
        e.stopPropagation();
        document.removeEventListener('mousemove', onMove, true);

        glyphElement.style.opacity = '1';
        glyphElement.style.pointerEvents = '';

        const finalX = Math.round(e.clientX - layerRect.left);
        const finalY = Math.round(e.clientY - layerRect.top);
        glyphElement.style.left = `${finalX}px`;
        glyphElement.style.top = `${finalY}px`;

        const rect = glyphElement.getBoundingClientRect();
        uiState.addCanvasGlyph({
            id: glyphId,
            symbol: opts.symbol,
            x: finalX,
            y: finalY,
            width: Math.round(rect.width) || fallbackW,
            height: Math.round(rect.height) || fallbackH,
            content: opts.content,
        });

        log.debug(SEG.GLYPH, `[${opts.prefix}] Placed ${glyphId} at (${finalX}, ${finalY})`);
    };

    document.addEventListener('mousemove', onMove, true);
    // Delay attaching click to avoid the same click event that triggered the spawn
    setTimeout(() => document.addEventListener('click', onPlace, { once: true, capture: true }), 0);
}
