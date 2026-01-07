// Tile focus functionality
// Handles focusing viewport on a tile and expanding it to fill most of the view

import { GRAPH_PHYSICS } from '../config.ts';
import { getSvg, getG, getZoom, getDomCache, getFocusedNodeId, getPreFocusTransform, setFocusedNodeId, setPreFocusTransform } from './state.ts';
import { getTransform } from './transform.ts';
import type { D3Node } from '../../types/d3-graph';

// Import D3 from vendor bundle
declare const d3: any;

// Default tile dimensions (must match renderer.ts)
const DEFAULT_TILE_WIDTH = 180;
const DEFAULT_TILE_HEIGHT = 80;

/**
 * Calculate focused tile dimensions based on viewport size
 * The tile should take up FOCUS_VIEWPORT_RATIO of the viewport
 */
function calculateFocusedTileDimensions(): { width: number; height: number; scale: number } {
    const domCache = getDomCache();
    const container = domCache.get('graphContainer', '#graph-container');
    if (!container) {
        return { width: DEFAULT_TILE_WIDTH, height: DEFAULT_TILE_HEIGHT, scale: 1 };
    }

    const viewportWidth = container.clientWidth;
    const viewportHeight = container.clientHeight;
    const padding = GRAPH_PHYSICS.FOCUS_TILE_PADDING;

    // Target size is viewport * ratio - padding
    const targetWidth = (viewportWidth * GRAPH_PHYSICS.FOCUS_VIEWPORT_RATIO) - (padding * 2);
    const targetHeight = (viewportHeight * GRAPH_PHYSICS.FOCUS_VIEWPORT_RATIO) - (padding * 2);

    // Calculate scale factors to fit tile in target area while maintaining aspect ratio
    const scaleX = targetWidth / DEFAULT_TILE_WIDTH;
    const scaleY = targetHeight / DEFAULT_TILE_HEIGHT;

    // Use the smaller scale to fit within bounds
    const scale = Math.min(scaleX, scaleY);

    return {
        width: DEFAULT_TILE_WIDTH * scale,
        height: DEFAULT_TILE_HEIGHT * scale,
        scale
    };
}

/**
 * Focus on a specific tile
 * - Centers the viewport on the tile
 * - Zooms in
 * - Expands the tile to fill most of the viewport
 */
export function focusOnTile(node: D3Node): void {
    const svg = getSvg();
    const g = getG();
    const zoom = getZoom();

    if (!svg || !g || !zoom || node.x === undefined || node.y === undefined) {
        return;
    }

    const domCache = getDomCache();
    const container = domCache.get('graphContainer', '#graph-container');
    if (!container) return;

    // Save current transform before focusing (only if not already focused)
    if (!getFocusedNodeId()) {
        const currentTransform = getTransform();
        setPreFocusTransform(currentTransform);
    }

    // Set the focused node
    setFocusedNodeId(node.id);

    const viewportWidth = container.clientWidth;
    const viewportHeight = container.clientHeight;

    // Calculate the focused tile dimensions
    const focusedDimensions = calculateFocusedTileDimensions();

    // Calculate zoom level to make the tile the right size
    // We want the tile to appear as focusedDimensions.width/height on screen
    const zoomScale = focusedDimensions.scale;

    // Calculate transform to center the node
    const targetX = viewportWidth / 2 - node.x * zoomScale;
    const targetY = viewportHeight / 2 - node.y * zoomScale;

    // Animate viewport to center on the tile
    svg.transition()
        .duration(GRAPH_PHYSICS.ANIMATION_DURATION)
        .call(zoom.transform, d3.zoomIdentity
            .translate(targetX, targetY)
            .scale(zoomScale));

    // Animate the focused tile to expand
    const nodeGroup = g.selectAll('.node')
        .filter((d: D3Node) => d.id === node.id);

    // Expand the rect
    nodeGroup.select('rect')
        .transition()
        .duration(GRAPH_PHYSICS.ANIMATION_DURATION)
        .attr('width', focusedDimensions.width)
        .attr('height', focusedDimensions.height)
        .attr('x', -focusedDimensions.width / 2)
        .attr('y', -focusedDimensions.height / 2);

    // Scale the text appropriately
    nodeGroup.select('text')
        .transition()
        .duration(GRAPH_PHYSICS.ANIMATION_DURATION)
        .attr('transform', `scale(${focusedDimensions.scale})`);
}

/**
 * Unfocus the current tile and restore normal view
 */
export function unfocus(): void {
    const focusedId = getFocusedNodeId();
    if (!focusedId) return;

    const svg = getSvg();
    const g = getG();
    const zoom = getZoom();

    if (!svg || !g || !zoom) return;

    // Restore the focused tile to normal size
    const nodeGroup = g.selectAll('.node')
        .filter((d: D3Node) => d.id === focusedId);

    nodeGroup.select('rect')
        .transition()
        .duration(GRAPH_PHYSICS.ANIMATION_DURATION)
        .attr('width', DEFAULT_TILE_WIDTH)
        .attr('height', DEFAULT_TILE_HEIGHT)
        .attr('x', -DEFAULT_TILE_WIDTH / 2)
        .attr('y', -DEFAULT_TILE_HEIGHT / 2);

    nodeGroup.select('text')
        .transition()
        .duration(GRAPH_PHYSICS.ANIMATION_DURATION)
        .attr('transform', 'scale(1)');

    // Restore the previous transform if available
    const preFocusTransform = getPreFocusTransform();
    if (preFocusTransform) {
        svg.transition()
            .duration(GRAPH_PHYSICS.ANIMATION_DURATION)
            .call(zoom.transform, d3.zoomIdentity
                .translate(preFocusTransform.x, preFocusTransform.y)
                .scale(preFocusTransform.k));
    }

    // Clear focus state
    setFocusedNodeId(null);
    setPreFocusTransform(null);
}

/**
 * Check if a tile is currently focused
 */
export function isFocused(): boolean {
    return getFocusedNodeId() !== null;
}

/**
 * Get the currently focused node ID
 */
export function getFocusedId(): string | null {
    return getFocusedNodeId();
}

/**
 * Handle user interaction that should trigger unfocus
 * Called when user zooms out, pans, or clicks empty space
 */
export function handleUnfocusTrigger(event: any): void {
    if (!isFocused()) return;

    // Determine if this is an unfocus-worthy interaction
    // Zoom out (scale decreased) or pan (translate changed significantly)
    const currentTransform = getTransform();
    const preFocus = getPreFocusTransform();

    if (!currentTransform) return;

    // If user is zooming out (scale is now smaller than what we set for focus)
    // or significantly panning, trigger unfocus
    const focusedDimensions = calculateFocusedTileDimensions();
    const focusScale = focusedDimensions.scale;

    // User zoomed out from the focus level
    if (currentTransform.k < focusScale * 0.9) {
        unfocus();
    }
}
