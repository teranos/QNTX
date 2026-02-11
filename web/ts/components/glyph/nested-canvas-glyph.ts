/**
 * Nested Canvas Glyph — subcircuit container with four port faces.
 *
 * A nested canvas is a rectangle on the parent surface with four meldable
 * faces (left, right, top, bottom). Each face has a port glyph inside:
 *
 *              ┌──────[↓ top]──────┐
 *              │                    │
 *   [→ in] ───│                    │─── [→ out]
 *              │                    │
 *              └──────[↓ bottom]───┘
 *
 * All four ports are auto-created when the canvas is spawned.
 * Port glyphs use arrows showing flow direction with titlebars
 * disambiguating the face: → for horizontal (in/out), ↓ for vertical (top/bottom).
 */

import type { Glyph } from './glyph';
import { log, SEG } from '../../logger';
import { canvasPlaced } from './manifestations/canvas-placed';
import { createPortGlyph, type PortDirection } from './port-glyph';

const CANVAS_WIDTH = 480;
const CANVAS_HEIGHT = 320;
const PORT_WIDTH = 80;
const PORT_HEIGHT = 48;
const PORT_MARGIN = 8;

/** Port layout: position each port glyph relative to the canvas content area */
function portPosition(direction: PortDirection, cw: number, ch: number): { x: number; y: number } {
    switch (direction) {
        case 'in':
            return { x: PORT_MARGIN, y: Math.round((ch - PORT_HEIGHT) / 2) };
        case 'out':
            return { x: cw - PORT_WIDTH - PORT_MARGIN, y: Math.round((ch - PORT_HEIGHT) / 2) };
        case 'top':
            return { x: Math.round((cw - PORT_WIDTH) / 2), y: PORT_MARGIN };
        case 'bottom':
            return { x: Math.round((cw - PORT_WIDTH) / 2), y: ch - PORT_HEIGHT - PORT_MARGIN };
    }
}

export async function createNestedCanvasGlyph(glyph: Glyph): Promise<HTMLElement> {
    const { element } = canvasPlaced({
        glyph,
        className: 'canvas-nested-glyph',
        defaults: {
            x: glyph.x ?? 200,
            y: glyph.y ?? 200,
            width: glyph.width ?? CANVAS_WIDTH,
            height: glyph.height ?? CANVAS_HEIGHT,
        },
        titleBar: { label: '⧉ canvas' },
        resizable: true,
        logLabel: 'NestedCanvas',
    });
    element.style.minWidth = '240px';
    element.style.minHeight = '180px';
    element.style.zIndex = '1';

    // Canvas title bar: distinctive color
    const titleBar = element.querySelector('.canvas-glyph-title-bar') as HTMLElement;
    if (titleBar) {
        titleBar.style.backgroundColor = '#3a2d5a';
        const labelSpan = titleBar.querySelector('span:first-child') as HTMLElement;
        if (labelSpan) {
            labelSpan.style.color = '#c0a0e8';
            labelSpan.style.fontWeight = 'bold';
        }
    }

    // Content area for port glyphs
    const content = document.createElement('div');
    content.className = 'nested-canvas-content';
    content.style.flex = '1';
    content.style.position = 'relative';
    content.style.overflow = 'hidden';
    content.style.backgroundColor = 'var(--bg-dark-hover)';
    element.appendChild(content);

    // Subtle inner grid pattern (matches parent canvas aesthetic)
    const gridOverlay = document.createElement('div');
    gridOverlay.className = 'canvas-grid-overlay';
    gridOverlay.style.position = 'absolute';
    gridOverlay.style.inset = '0';
    gridOverlay.style.pointerEvents = 'none';
    gridOverlay.style.opacity = '0.3';
    content.appendChild(gridOverlay);

    // Auto-create the four port glyphs
    const contentWidth = glyph.width ?? CANVAS_WIDTH;
    const contentHeight = (glyph.height ?? CANVAS_HEIGHT) - 32; // subtract title bar
    const directions: PortDirection[] = ['in', 'out', 'top', 'bottom'];

    for (const dir of directions) {
        const pos = portPosition(dir, contentWidth, contentHeight);
        const portGlyph: Glyph = {
            id: `port-${dir}-${glyph.id}`,
            title: `Port ${dir}`,
            symbol: `port-${dir}`,
            x: pos.x,
            y: pos.y,
            renderContent: () => document.createElement('div'),
        };

        const portElement = createPortGlyph(portGlyph, dir);

        // Position port within the content area (absolute positioning)
        portElement.style.position = 'absolute';
        portElement.style.left = `${pos.x}px`;
        portElement.style.top = `${pos.y}px`;

        content.appendChild(portElement);
    }

    log.debug(SEG.GLYPH, `[NestedCanvas] Created canvas ${glyph.id} with 4 port glyphs`);

    return element;
}
