// Dimension calculations for focus mode

import { GRAPH_PHYSICS } from '../../config.ts';
import { getDomCache } from '../state.ts';
import { log, SEG } from '../../logger.ts';

// Default tile dimensions (must match renderer.ts)
export const DEFAULT_TILE_WIDTH = 180;
export const DEFAULT_TILE_HEIGHT = 80;

/**
 * Calculate dimensions for a focused tile based on viewport size at canonical zoom (1.0)
 * Focus mode always resets to zoom 1.0 for a consistent viewing experience
 */
export function calculateFocusedTileDimensions(): { width: number; height: number; scale: number } {
    // TODO: When #graph-container is renamed to #graph-viewer, update this selector
    // Use graph-container dimensions (the actual graph viewing area)
    const domCache = getDomCache();
    const container = domCache.get('graphContainer', '#graph-container');
    if (!container) {
        log.warn(SEG.GRAPH, '[dimensions] container not found, using defaults', {
            defaults: { width: DEFAULT_TILE_WIDTH, height: DEFAULT_TILE_HEIGHT, scale: 1 }
        });
        return { width: DEFAULT_TILE_WIDTH, height: DEFAULT_TILE_HEIGHT, scale: 1 };
    }

    const viewportWidth = container.clientWidth;
    const viewportHeight = container.clientHeight;
    const padding = GRAPH_PHYSICS.FOCUS_TILE_PADDING;

    // Canonical zoom level for focus mode
    const canonicalZoom = 1.0;

    // Target SCREEN size (what we want to see on screen)
    const targetScreenWidth = (viewportWidth * GRAPH_PHYSICS.FOCUS_VIEWPORT_RATIO) - (padding * 2);
    const targetScreenHeight = (viewportHeight * GRAPH_PHYSICS.FOCUS_VIEWPORT_RATIO) - (padding * 2);

    // At canonical zoom (1.0), screen size = SVG size
    const targetWidth = targetScreenWidth / canonicalZoom;
    const targetHeight = targetScreenHeight / canonicalZoom;

    // Calculate scale factors for reference
    const scaleX = targetWidth / DEFAULT_TILE_WIDTH;
    const scaleY = targetHeight / DEFAULT_TILE_HEIGHT;

    return {
        width: targetWidth,   // SVG width at canonical zoom
        height: targetHeight, // SVG height at canonical zoom
        scale: Math.max(scaleX, scaleY)
    };
}
