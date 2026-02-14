/**
 * Doc Glyph — inline document viewer on canvas (PDF via <embed>)
 *
 * Created by dragging a file onto the canvas workspace.
 * Content field stores JSON: { fileId, filename, ext }
 * The file is served from /api/files/{fileId}{ext}.
 */

import type { Glyph } from './glyph';
import { log, SEG } from '../../logger';
import { uiState } from '../../state/ui';
import { canvasPlaced } from './manifestations/canvas-placed';
import { fileUrl } from '../../api/files';

/** Metadata stored in the glyph content field */
export interface DocGlyphContent {
    fileId: string;
    filename: string;
    ext: string;
}

/**
 * Create a doc glyph element
 */
export async function createDocGlyph(glyph: Glyph): Promise<HTMLElement> {
    const element = document.createElement('div');

    // Parse stored content
    const existing = uiState.getCanvasGlyphs().find(g => g.id === glyph.id);
    let meta: DocGlyphContent | null = null;
    if (existing?.content) {
        try {
            meta = JSON.parse(existing.content);
        } catch {
            log.error(SEG.GLYPH, `[Doc Glyph] Failed to parse content JSON for ${glyph.id}`);
        }
    }

    const filename = meta?.filename ?? 'Document';

    canvasPlaced({
        element,
        glyph,
        className: 'canvas-doc-glyph',
        defaults: { x: 200, y: 200, width: 400, height: 500 },
        titleBar: { label: filename },
        resizable: { minWidth: 200, minHeight: 200 },
        logLabel: 'DocGlyph',
    });

    // Embed container fills remaining space below title bar
    const embedContainer = document.createElement('div');
    embedContainer.className = 'doc-embed-container';

    if (meta) {
        const url = fileUrl(meta.fileId, meta.ext);
        const embed = document.createElement('embed');
        embed.src = url;
        embed.type = 'application/pdf';
        embed.className = 'doc-embed';
        embedContainer.appendChild(embed);
    } else {
        const placeholder = document.createElement('div');
        placeholder.className = 'doc-placeholder';
        placeholder.textContent = 'No document loaded';
        embedContainer.appendChild(placeholder);
    }

    element.appendChild(embedContainer);

    log.debug(SEG.GLYPH, `[Doc Glyph] Created ${glyph.id} for ${filename}`);
    return element;
}
