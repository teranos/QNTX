/**
 * IX Glyph - Ingest data from external sources
 *
 * Provides a form for entering URLs, file paths, or any ix-compatible input.
 * Executes ingestion and creates attestations from the imported data.
 */

import type { Glyph } from './glyph';
import { IX } from '@generated/sym.js';
import { renderIxManifestation } from './manifestations/ix';

/**
 * Create an IX glyph with IX manifestation
 */
export function createIxGlyph(gridX: number, gridY: number): Glyph {
    return {
        id: `ix-${crypto.randomUUID()}`,
        title: 'Ingest',
        symbol: IX,
        manifestationType: 'ix',
        gridX,
        gridY,
        renderContent: () => renderIxManifestation()
    };
}

