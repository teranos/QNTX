// Dimension calculations for focus mode

import { GRAPH_PHYSICS } from '../../config.ts';
import { getDomCache } from '../state.ts';
import { getTransform } from '../transform.ts';

// Default tile dimensions (must match renderer.ts)
export const DEFAULT_TILE_WIDTH = 180;
export const DEFAULT_TILE_HEIGHT = 80;

/**
 * Calculate dimensions for a focused tile based on viewport size AND current zoom level
 * Returns SVG dimensions that will appear at the correct screen size regardless of zoom
 */
export function calculateFocusedTileDimensions(): { width: number; height: number; scale: number } {
    // TODO: When #graph-container is renamed to #graph-viewer, update this selector
    // Use graph-container dimensions (the actual graph viewing area)
    const domCache = getDomCache();
    const container = domCache.get('graphContainer', '#graph-container');
    if (!container) {
        return { width: DEFAULT_TILE_WIDTH, height: DEFAULT_TILE_HEIGHT, scale: 1 };
    }

    const viewportWidth = container.clientWidth;
    const viewportHeight = container.clientHeight;
    const padding = GRAPH_PHYSICS.FOCUS_TILE_PADDING;

    // Get current zoom level
    const currentTransform = getTransform();
    const currentZoom = currentTransform ? currentTransform.k : 1;

    // Target SCREEN size (what we want to see on screen)
    const targetScreenWidth = (viewportWidth * GRAPH_PHYSICS.FOCUS_VIEWPORT_RATIO) - (padding * 2);
    const targetScreenHeight = (viewportHeight * GRAPH_PHYSICS.FOCUS_VIEWPORT_RATIO) - (padding * 2);

    // Convert screen size to SVG size by dividing by current zoom
    // This ensures the tile appears at the correct size regardless of zoom level
    const targetWidth = targetScreenWidth / currentZoom;
    const targetHeight = targetScreenHeight / currentZoom;

    // Calculate scale factors for reference
    const scaleX = targetWidth / DEFAULT_TILE_WIDTH;
    const scaleY = targetHeight / DEFAULT_TILE_HEIGHT;

    return {
        width: targetWidth,   // SVG width (compensated for zoom)
        height: targetHeight, // SVG height (compensated for zoom)
        scale: Math.max(scaleX, scaleY)
    };
}
