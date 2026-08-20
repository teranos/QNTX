/**
 * Border specimen — a canvas-placed glyph whose border is visual identity.
 *
 * Like color, the border lives on the Glyph datum and every manifestation
 * wears it: ⬆ expands to a window that keeps the dashed border; the window's
 * − places it back unchanged. Everything about a glyph survives every
 * transition (Element Axioma).
 */

import { canvasPlaced } from '../manifestations/canvas-placed';
import { morphCanvasPlacedToWindow } from '../manifestations/canvas-window';
import { isInWindowState } from '../dataset';
import type { Glyph } from '../glyph';

const OWNED_BORDER = '2px dashed #ffd43b';

export function renderBorderSpecimen(): void {
    const root = document.getElementById('root');
    if (!root) return;

    const area = document.createElement('div');
    area.style.position = 'relative';
    area.style.height = '240px';
    area.dataset.canvasId = 'border-canvas';
    root.appendChild(area);

    const glyph: Glyph = {
        id: 'border-specimen',
        title: 'Border',
        symbol: '▣',
        // Visual identity on the datum — every manifestation wears it
        border: OWNED_BORDER,
        renderContent: () => document.createElement('div'),
    };

    const expand = document.createElement('button');
    expand.textContent = '⬆';
    expand.title = 'Expand to window';

    const { element } = canvasPlaced({
        glyph,
        className: 'canvas-border-specimen',
        defaults: { x: 16, y: 40, width: 240, height: 150 },
        titleBar: { label: 'owns its border', actions: [expand] },
        logLabel: 'BorderSpecimen',
    });

    const body = document.createElement('div');
    body.className = 'glyph-content-area';
    body.textContent = `inline border: ${OWNED_BORDER}`;
    element.appendChild(body);

    expand.addEventListener('click', () => {
        if (isInWindowState(element)) return;
        morphCanvasPlacedToWindow(element, {
            title: 'Border',
            canvasId: 'border-canvas',
            onRestoreComplete: () => {},
        });
    });

    area.appendChild(element);
}
