/**
 * Generic spawn-on-canvas helper for attestation-family elements.
 *
 * Eliminates the repeated contentLayer lookup → position calc → registry lookup →
 * render → appendChild → uiState.addCanvasElement pattern across attestation,
 * triplet, and sigma elements.
 */

import type { Element } from '@teranos/elements';
import { log, SEG } from '../../logger';
import { uiState } from '../../state/ui';
import { getElementTypeBySymbol } from './element-registry';

export interface SpawnOpts {
    /** Element symbol (used for registry lookup and uiState tracking) */
    symbol: string;
    /** ID prefix (e.g. 'as', 'triplet', 'sigma') */
    prefix: string;
    /** Title for the element */
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
 * Spawn an element on the canvas. Returns the element element, or null if
 * the canvas content layer or registry entry is missing.
 */
export function spawnOnCanvas(opts: SpawnOpts): HTMLElement | null {
    const contentLayer = document.querySelector('.canvas-content-layer') as HTMLElement;
    if (!contentLayer) {
        log.warn(SEG.ELEMENT, `[${opts.prefix}] Cannot spawn: no canvas-content-layer found`);
        return null;
    }

    const elementId = `${opts.prefix}-${crypto.randomUUID()}`;
    const layerRect = contentLayer.getBoundingClientRect();
    const fallbackW = opts.fallbackWidth || 420;
    const fallbackH = opts.fallbackHeight || 200;

    const x = opts.mouseX !== undefined
        ? Math.round(opts.mouseX - layerRect.left + 20)
        : Math.round(window.innerWidth / 2 - fallbackW / 2);
    const y = opts.mouseY !== undefined
        ? Math.round(opts.mouseY - layerRect.top - 20)
        : Math.round(window.innerHeight / 2 - fallbackH / 2);

    const item: Element = {
        id: elementId,
        title: opts.title,
        symbol: opts.symbol,
        x,
        y,
        content: opts.content,
        renderContent: () => document.createElement('div'),
    };

    const entry = getElementTypeBySymbol(opts.symbol);
    if (!entry) {
        log.error(SEG.ELEMENT, `[${opts.prefix}] Symbol ${opts.symbol} not found in element registry`);
        return null;
    }

    const elementElement = entry.render(item) as HTMLElement;
    contentLayer.appendChild(elementElement);

    const rect = elementElement.getBoundingClientRect();
    uiState.addCanvasElement({
        id: elementId,
        symbol: opts.symbol,
        x,
        y,
        width: Math.round(rect.width) || fallbackW,
        height: Math.round(rect.height) || fallbackH,
        content: opts.content,
    });

    log.debug(SEG.ELEMENT, `[${opts.prefix}] Spawned ${elementId} at (${x}, ${y})`);
    return elementElement;
}

/**
 * Spawn an element attached to the cursor. The element follows the mouse
 * until the user clicks to place it down on the canvas.
 */
export function spawnOnCanvasDragging(opts: Omit<SpawnOpts, 'mouseX' | 'mouseY'>, startX: number, startY: number): void {
    const contentLayer = document.querySelector('.canvas-content-layer') as HTMLElement;
    if (!contentLayer) {
        log.warn(SEG.ELEMENT, `[${opts.prefix}] Cannot spawn: no canvas-content-layer found`);
        return;
    }

    const elementId = `${opts.prefix}-${crypto.randomUUID()}`;
    const layerRect = contentLayer.getBoundingClientRect();
    const fallbackW = opts.fallbackWidth || 420;
    const fallbackH = opts.fallbackHeight || 200;

    const item: Element = {
        id: elementId,
        title: opts.title,
        symbol: opts.symbol,
        x: Math.round(startX - layerRect.left),
        y: Math.round(startY - layerRect.top),
        content: opts.content,
        renderContent: () => document.createElement('div'),
    };

    const entry = getElementTypeBySymbol(opts.symbol);
    if (!entry) {
        log.error(SEG.ELEMENT, `[${opts.prefix}] Symbol ${opts.symbol} not found in element registry`);
        return;
    }

    const elementElement = entry.render(item) as HTMLElement;
    elementElement.style.opacity = '0.7';
    elementElement.style.pointerEvents = 'none';
    contentLayer.appendChild(elementElement);

    const onMove = (e: MouseEvent) => {
        const lx = Math.round(e.clientX - layerRect.left);
        const ly = Math.round(e.clientY - layerRect.top);
        elementElement.style.left = `${lx}px`;
        elementElement.style.top = `${ly}px`;
    };

    const onPlace = (e: MouseEvent) => {
        e.stopPropagation();
        document.removeEventListener('mousemove', onMove, true);

        elementElement.style.opacity = '1';
        elementElement.style.pointerEvents = '';

        const finalX = Math.round(e.clientX - layerRect.left);
        const finalY = Math.round(e.clientY - layerRect.top);
        elementElement.style.left = `${finalX}px`;
        elementElement.style.top = `${finalY}px`;

        const rect = elementElement.getBoundingClientRect();
        uiState.addCanvasElement({
            id: elementId,
            symbol: opts.symbol,
            x: finalX,
            y: finalY,
            width: Math.round(rect.width) || fallbackW,
            height: Math.round(rect.height) || fallbackH,
            content: opts.content,
        });

        log.debug(SEG.ELEMENT, `[${opts.prefix}] Placed ${elementId} at (${finalX}, ${finalY})`);
    };

    document.addEventListener('mousemove', onMove, true);
    // Delay attaching click to avoid the same click event that triggered the spawn
    setTimeout(() => document.addEventListener('click', onPlace, { once: true, capture: true }), 0);
}
