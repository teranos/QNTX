/**
 * IX Glyph - Lightweight reference on canvas
 *
 * A visual marker on the canvas showing an IX operation.
 * Clicking it opens the full IX manifestation (editor/previewer).
 * The manifestation can minimize back to the tray, but the canvas glyph persists.
 */

import type { Glyph } from './glyph';
import { IX } from '@generated/sym.js';

/**
 * Create a lightweight IX glyph for canvas
 * This is just a reference/thumbnail - clicking it opens the full IX manifestation
 */
export function createIxGlyph(gridX: number, gridY: number): Glyph {
    return {
        id: `ix-${crypto.randomUUID()}`,
        title: 'Ingest',
        symbol: IX,
        manifestationType: 'ix',
        gridX,
        gridY,
        renderContent: () => {
            // Placeholder - actual content rendered by morphToIx
            const placeholder = document.createElement('div');
            placeholder.textContent = 'IX Manifestation';
            return placeholder;
        }
    };
}

