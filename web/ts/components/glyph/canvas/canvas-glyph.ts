/**
 * Canvas Glyph - Fractal container with spatial grid layout
 *
 * The canvas is a glyph that morphs to full-screen and contains other glyphs
 * arranged on a spatial grid. Right-click spawns new glyphs.
 *
 * Selection & Interaction:
 * - Click a glyph to select it (green outline, action bar appears at top)
 * - Shift+click to add/remove glyphs from selection (multi-select)
 * - Click canvas background to deselect
 * - Drag selected glyph(s) - all selected glyphs move together maintaining relative positions
 * - Action bar provides delete and unmeld (for melded compositions)
 *
 * Keyboard Shortcuts:
 * - ESC: deselect all glyphs
 * - DELETE or BACKSPACE: remove selected glyphs
 * - Shortcuts scoped to focused canvas (click to focus)
 *
 * This demonstrates the fractal principle: all glyphs are containers.
 */

import type { Glyph } from '../glyph';
import { Pulse, IX, AX } from '@generated/sym.js';
import { log, SEG } from '../../../logger';
import { getGlyphTypeBySymbol } from '../glyph-registry';
import { uiState } from '../../../state/ui';
import { buildCanvasWorkspace } from './canvas-workspace-builder';

// Re-export selection queries so existing consumers don't break
export { isGlyphSelected, getSelectedGlyphIds, getSelectedGlyphElements } from './canvas-workspace-builder';

/**
 * Factory function to create a Canvas glyph
 */
export function createCanvasGlyph(): Glyph {
    // Load persisted glyphs from uiState
    const allSavedGlyphs = uiState.getCanvasGlyphs();

    // Filter out error glyphs (ephemeral - should never be persisted)
    const errorGlyphs = allSavedGlyphs.filter(g => g.symbol === 'error');
    if (errorGlyphs.length > 0) {
        log.warn(SEG.GLYPH, `[Canvas] Removing ${errorGlyphs.length} persisted error glyphs (should be ephemeral)`, {
            ids: errorGlyphs.map(g => g.id)
        });
        errorGlyphs.forEach(g => uiState.removeCanvasGlyph(g.id));
    }

    const savedGlyphs = allSavedGlyphs.filter(g => g.symbol !== 'error');
    const resultCount = savedGlyphs.filter(g => g.symbol === 'result').length;
    log.debug(SEG.GLYPH, `[Canvas] Restoring ${savedGlyphs.length} glyphs from state (${resultCount} result glyphs)`, {
        symbols: savedGlyphs.map(g => g.symbol),
        resultGlyphs: savedGlyphs.filter(g => g.symbol === 'result').map(g => ({
            id: g.id,
            hasContent: !!g.content
        }))
    });

    const glyphs: Glyph[] = savedGlyphs.map(saved => {
        if (saved.symbol === 'result') {
            log.debug(SEG.GLYPH, `[Canvas] Restoring result glyph ${saved.id}`, {
                hasContent: !!saved.content,
                x: saved.x,
                y: saved.y
            });
        }

        const entry = saved.symbol ? getGlyphTypeBySymbol(saved.symbol) : undefined;
        return {
            id: saved.id,
            title: entry?.title ?? 'Glyph',
            symbol: saved.symbol,
            x: saved.x,
            y: saved.y,
            width: saved.width,
            height: saved.height,
            content: saved.content,
            renderContent: () => document.createElement('div'),
        };
    });

    return {
        id: 'canvas-workspace',
        title: 'Canvas',
        manifestationType: 'fullscreen', // Full-viewport, no chrome
        layoutStrategy: 'grid',
        children: glyphs,
        onSpawnMenu: () => [Pulse, IX, AX], // TODO: Remove Pulse when IX wired up

        renderContent: () => buildCanvasWorkspace('canvas-workspace', glyphs)
    };
}
