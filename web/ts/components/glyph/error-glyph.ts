/**
 * Error Glyph - Diagnostic panel for failed glyph rendering
 *
 * Error glyphs are ephemeral - they exist only in the DOM to show
 * diagnostic information and are never persisted to state.
 *
 * When a glyph fails to render (e.g., result glyph missing execution data),
 * an error glyph is spawned in its place with diagnostic context.
 */

import { log, SEG } from '../../logger';
import { uiState } from '../../state/ui';
import { applyCanvasGlyphLayout, makeDraggable } from './glyph-interaction';

/**
 * Error context for diagnostic display
 */
export interface ErrorContext {
    /** Type of error (e.g., "missing_data", "parse_failed") */
    type: string;
    /** Human-readable error message */
    message: string;
    /** Additional diagnostic data */
    details?: Record<string, unknown>;
}

/**
 * Create an error glyph showing diagnostic information
 *
 * Error glyphs are ephemeral - not added to uiState, exist only in DOM.
 * They provide in-canvas error feedback with dismiss functionality.
 *
 * @param failedGlyphId - ID of the glyph that failed to render
 * @param failedSymbol - Symbol of the failed glyph
 * @param position - Position where error glyph should appear
 * @param error - Error context with diagnostic info
 */
export function createErrorGlyph(
    failedGlyphId: string,
    failedSymbol: string,
    position: { x: number; y: number },
    error: ErrorContext
): HTMLElement {
    const element = document.createElement('div');
    element.className = 'canvas-error-glyph canvas-glyph';
    element.dataset.glyphId = `error-${crypto.randomUUID()}`;
    element.dataset.glyphSymbol = 'error';

    const width = 450;
    const height = 280;

    // Apply canvas layout
    applyCanvasGlyphLayout(element, { x: position.x, y: position.y, width, height });

    // Error styling
    element.style.backgroundColor = 'rgba(60, 20, 20, 0.95)';
    element.style.border = '2px solid #dd4444';
    element.style.color = '#ffcccc';
    element.style.fontFamily = 'monospace';
    element.style.fontSize = '12px';
    element.style.overflow = 'auto';

    // Header with dismiss button
    const header = document.createElement('div');
    header.style.padding = '12px';
    header.style.borderBottom = '1px solid #aa4444';
    header.style.display = 'flex';
    header.style.justifyContent = 'space-between';
    header.style.alignItems = 'center';
    header.style.backgroundColor = 'rgba(80, 20, 20, 0.8)';
    header.style.cursor = 'move';
    header.style.userSelect = 'none';

    const title = document.createElement('div');
    title.style.fontWeight = 'bold';
    title.style.fontSize = '14px';
    title.textContent = '⚠️  Glyph Rendering Error';
    header.appendChild(title);

    const dismissBtn = document.createElement('button');
    dismissBtn.textContent = '✕';
    dismissBtn.style.background = 'none';
    dismissBtn.style.border = 'none';
    dismissBtn.style.color = '#ffcccc';
    dismissBtn.style.cursor = 'pointer';
    dismissBtn.style.fontSize = '18px';
    dismissBtn.style.padding = '0';
    dismissBtn.style.width = '24px';
    dismissBtn.style.height = '24px';
    dismissBtn.title = 'Dismiss and remove broken glyph from state';
    dismissBtn.addEventListener('click', (e) => {
        e.stopPropagation();
        // Remove the broken glyph from state
        uiState.removeCanvasGlyph(failedGlyphId);
        // Remove error glyph from DOM
        element.remove();
        log.info(SEG.GLYPH, `[ErrorGlyph] Dismissed and removed broken glyph ${failedGlyphId}`);
    });
    header.appendChild(dismissBtn);

    element.appendChild(header);

    // Content area
    const content = document.createElement('div');
    content.style.padding = '16px';
    content.style.whiteSpace = 'pre-wrap';
    content.style.lineHeight = '1.6';

    const lines = [
        `Failed Glyph: ${failedSymbol}`,
        `ID: ${failedGlyphId}`,
        `Position: (${position.x}, ${position.y})`,
        '',
        `Error Type: ${error.type}`,
        `Message: ${error.message}`,
    ];

    if (error.details) {
        lines.push('', 'Details:');
        for (const [key, value] of Object.entries(error.details)) {
            lines.push(`  ${key}: ${JSON.stringify(value)}`);
        }
    }

    lines.push('', '---', 'This is a diagnostic error glyph (ephemeral).');
    lines.push('Click ✕ to dismiss and remove the broken glyph.');

    content.textContent = lines.join('\n');
    element.appendChild(content);

    // Make draggable via header
    const dummyGlyph = {
        id: element.dataset.glyphId!,
        title: 'Error',
        symbol: 'error',
        x: position.x,
        y: position.y,
        renderContent: () => element
    };
    makeDraggable(element, header, dummyGlyph, { logLabel: 'ErrorGlyph' });

    return element;
}
