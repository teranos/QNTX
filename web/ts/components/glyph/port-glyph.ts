/**
 * Port Glyph — directional data flow indicator inside nested canvases.
 *
 * Port glyphs mark where data enters and exits a nested canvas:
 * - in/out use → (right arrow) — horizontal data flow
 * - top/bottom use ↓ (down arrow) — vertical data flow
 *
 * Each port has a titlebar labeling its role. The arrow symbol shows
 * flow direction, the titlebar disambiguates which face it represents.
 *
 * Ports are auto-created when a nested canvas is spawned — one per face.
 */

import type { Glyph } from './glyph';
import { canvasPlaced } from './manifestations/canvas-placed';

export type PortDirection = 'in' | 'out' | 'top' | 'bottom';

/** Arrow symbol for the given port direction */
function portSymbol(direction: PortDirection): string {
    return direction === 'in' || direction === 'out' ? '→' : '↓';
}

/** CSS class suffix for port glyphs */
const PORT_CLASS = 'canvas-port-glyph';

/** Color accents per port direction */
const PORT_COLORS: Record<PortDirection, { bar: string; label: string }> = {
    in:     { bar: '#2d5a3d', label: '#7aca7a' },
    out:    { bar: '#5a2d2d', label: '#ca7a7a' },
    top:    { bar: '#2d3d5a', label: '#7a9aca' },
    bottom: { bar: '#5a4a2d', label: '#caaa7a' },
};

export function createPortGlyph(glyph: Glyph, direction: PortDirection): HTMLElement {
    const symbol = portSymbol(direction);

    const { element } = canvasPlaced({
        glyph,
        className: PORT_CLASS,
        defaults: { x: glyph.x ?? 0, y: glyph.y ?? 0, width: 80, height: 48 },
        titleBar: { label: `${symbol} ${direction}` },
        resizable: false,
        logLabel: 'PortGlyph',
    });

    // Port-specific title bar color
    const colors = PORT_COLORS[direction];
    const titleBar = element.querySelector('.canvas-glyph-title-bar') as HTMLElement;
    if (titleBar) {
        titleBar.style.backgroundColor = colors.bar;
        const labelSpan = titleBar.querySelector('span:first-child') as HTMLElement;
        if (labelSpan) {
            labelSpan.style.color = colors.label;
            labelSpan.style.fontWeight = 'bold';
        }
    }

    // Store direction as data attribute for meld system
    element.dataset.portDirection = direction;

    return element;
}
