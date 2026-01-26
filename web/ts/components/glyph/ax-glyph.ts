/**
 * Ax Glyph - Ax query editor on canvas grid
 *
 * Text editor for writing ax queries like "is git", "has certification", etc.
 * Similar to ATS editor or prompt editor - just the query text itself.
 *
 * ARCHITECTURE:
 * Canvas ax glyphs are lightweight references/thumbnails that show the query text.
 * They serve as visual markers on the spatial canvas to show active ax queries.
 *
 * TODO: Future enhancements
 * - Add result count badge (e.g., "42 attestations")
 * - Show mini type distribution (tiny bar chart or colored dots for node types)
 * - Click handler to spawn full ax manifestation (attestation explorer window)
 * - Integration with graph explorer (may reuse existing graph component)
 * - Persist query results to avoid re-execution on reload
 * - Support query templates/snippets
 */

import type { Glyph } from './glyph';
import { AX } from '@generated/sym.js';
import { log, SEG } from '../../logger';
import { GRID_SIZE } from './grid-constants';

/**
 * Factory function to create an Ax query editor glyph
 *
 * @param id Optional glyph ID
 * @param initialQuery Optional initial query text
 */
export function createAxGlyph(id?: string, initialQuery: string = ''): Glyph {
    const glyphId = id || `ax-${crypto.randomUUID()}`;
    let currentQuery = initialQuery;

    return {
        id: glyphId,
        title: 'Ax Query',
        symbol: AX,
        manifestationType: 'ax', // Ax manifestation - inline grid editor
        renderContent: () => {
            const container = document.createElement('div');
            container.className = 'ax-query-editor';
            container.style.width = `${GRID_SIZE}px`;
            container.style.height = `${GRID_SIZE}px`;
            container.style.display = 'flex';
            container.style.flexDirection = 'column';
            container.style.overflow = 'hidden';
            container.style.backgroundColor = 'var(--bg-primary)';
            container.style.border = '1px solid var(--border-primary)';
            container.style.borderRadius = '4px';

            // Text editor for the ax query
            const editor = document.createElement('textarea');
            editor.className = 'ax-query-textarea';
            editor.value = currentQuery;
            editor.placeholder = 'Enter ax query (e.g., is git, has certification)';
            editor.style.flex = '1';
            editor.style.width = '100%';
            editor.style.padding = '8px';
            editor.style.fontSize = '13px';
            editor.style.fontFamily = 'monospace';
            editor.style.border = 'none';
            editor.style.outline = 'none';
            editor.style.resize = 'none';
            editor.style.backgroundColor = 'var(--bg-primary)';
            editor.style.color = 'var(--text-primary)';
            editor.style.overflow = 'auto';

            // Update current query on change
            editor.addEventListener('input', () => {
                currentQuery = editor.value;
                log.debug(SEG.UI, `[Ax Glyph ${glyphId}] Query updated: ${currentQuery}`);
            });

            container.appendChild(editor);

            return container;
        }
    };
}
