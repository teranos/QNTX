// Tile focus functionality
// Handles focusing viewport on a tile and expanding it to fill most of the view

import { GRAPH_PHYSICS } from '../config.ts';
import { getSimulation, getSvg, getG, getZoom, getDomCache, getFocusedNodeId, getPreFocusTransform, setFocusedNodeId, setPreFocusTransform, setIsFocusAnimating } from './state.ts';
import { getTransform } from './transform.ts';
import { calculateFocusedTileDimensions, DEFAULT_TILE_WIDTH, DEFAULT_TILE_HEIGHT } from './focus/dimensions.ts';
import { setFocusUIVisibility, setUnfocusCallback } from './focus/ui.ts';
import { adjustPhysicsForFocus, restoreNormalPhysics } from './focus/physics.ts';
import { createFocusHeader, removeFocusHeader, createFocusFooter, removeFocusFooter } from './tile/controls.ts';
import type { D3Node } from '../../types/d3-graph';

// Import D3 from vendor bundle
declare const d3: any;

// Register unfocus callback for UI module
setUnfocusCallback(unfocus);

/**
 * Focus on a specific tile
 * - Centers the viewport on the tile
 * - Zooms in
 * - Expands the tile to fill most of the viewport
 * - Supports smooth tile-to-tile transitions when already focused
 */
export function focusOnTile(node: D3Node): void {
    const svg = getSvg();
    const g = getG();
    const zoom = getZoom();

    if (!svg || !g || !zoom || node.x === undefined || node.y === undefined) {
        console.warn('[focus] early return - missing requirements', {
            svg: !!svg,
            g: !!g,
            zoom: !!zoom,
            nodeX: node.x,
            nodeY: node.y
        });
        return;
    }

    const domCache = getDomCache();
    const container = domCache.get('graphContainer', '#graph-container');
    if (!container) {
        console.warn('[focus] container not found', { selector: '#graph-container' });
        return;
    }

    const previouslyFocusedId = getFocusedNodeId();
    const isTransition = previouslyFocusedId !== null && previouslyFocusedId !== node.id;

    console.log('[focus] focusOnTile', {
        nodeId: node.id,
        nodeLabel: node.label,
        nodePos: { x: node.x, y: node.y },
        previouslyFocusedId,
        isTransition,
        viewportSize: { width: container.clientWidth, height: container.clientHeight }
    });

    // If transitioning between tiles, restore the previous tile to normal size
    if (isTransition) {
        console.log('[focus] tile-transition', {
            from: previouslyFocusedId,
            to: node.id,
            toLabel: node.label
        });

        const prevNodeGroup = g.selectAll<SVGGElement, D3Node>('.node')
            .filter((d) => d.id === previouslyFocusedId);

        // Remove header and footer from previous tile
        removeFocusHeader(prevNodeGroup);
        removeFocusFooter(prevNodeGroup);

        // Restore previous tile to normal size
        prevNodeGroup.select('rect')
            .transition()
            .duration(GRAPH_PHYSICS.ANIMATION_DURATION)
            .attr('width', DEFAULT_TILE_WIDTH)
            .attr('height', DEFAULT_TILE_HEIGHT)
            .attr('x', -DEFAULT_TILE_WIDTH / 2)
            .attr('y', -DEFAULT_TILE_HEIGHT / 2);

        prevNodeGroup.select('text')
            .transition()
            .duration(GRAPH_PHYSICS.ANIMATION_DURATION)
            .attr('transform', 'scale(1)');
    }

    // Save current transform before focusing (only if not already focused)
    if (!previouslyFocusedId) {
        const currentTransform = getTransform();
        setPreFocusTransform(currentTransform);
    }

    // Set the focused node
    setFocusedNodeId(node.id);

    const viewportWidth = container.clientWidth;
    const viewportHeight = container.clientHeight;

    // Reset to canonical zoom level (1.0) for consistent focus experience
    const canonicalZoom = 1.0;

    // Calculate the focused tile dimensions (at canonical zoom)
    const focusedDimensions = calculateFocusedTileDimensions();

    console.log('[focus] dimensions', {
        focusedDimensions,
        canonicalZoom,
        viewport: { width: viewportWidth, height: viewportHeight }
    });

    // Adjust physics first to pin the tile at its current position
    // This prevents the tile from drifting during focus
    adjustPhysicsForFocus(node);

    // Wait a tick for physics to apply pinning forces
    // This ensures node.x and node.y are at their final positions before we calculate pan
    const simulation = getSimulation();
    if (simulation) {
        simulation.tick();
    }

    // Calculate transform to center the node at canonical zoom
    // Use the node's current position (which is now pinned by physics)
    const targetX = viewportWidth / 2 - node.x * canonicalZoom;
    const targetY = viewportHeight / 2 - node.y * canonicalZoom;

    console.log('[focus] pan-target', {
        nodePos: { x: node.x, y: node.y },
        targetTransform: { x: targetX, y: targetY, k: canonicalZoom },
        calculation: `viewport_center (${viewportWidth/2}, ${viewportHeight/2}) - node_pos * zoom`
    });

    // Set flag to prevent unfocus detection during programmatic zoom animation
    setIsFocusAnimating(true);

    console.log('[focus] animation-start', {
        focusedNodeId: node.id,
        duration: GRAPH_PHYSICS.ANIMATION_DURATION
    });

    // Always animate to canonical zoom and center on the tile (even during transitions)
    svg.transition()
        .duration(GRAPH_PHYSICS.ANIMATION_DURATION)
        .call(zoom.transform, d3.zoomIdentity
            .translate(targetX, targetY)
            .scale(canonicalZoom))  // Reset to canonical zoom level
        .on("end", () => {
            console.log('[focus] animation-end', {
                focusedNodeId: node.id
            });
            // Clear animation flag once transition completes
            setIsFocusAnimating(false);
        });

    // Hide UI elements when focused (only if not already in focus mode)
    if (!isTransition) {
        setFocusUIVisibility(false);
    }

    // Animate the focused tile to expand
    const nodeGroup = g.selectAll<SVGGElement, D3Node>('.node')
        .filter((d) => d.id === node.id);

    // Expand the rect and remove stroke for cleaner focused appearance
    nodeGroup.select('rect')
        .transition()
        .duration(GRAPH_PHYSICS.ANIMATION_DURATION)
        .attr('width', focusedDimensions.width)
        .attr('height', focusedDimensions.height)
        .attr('x', -focusedDimensions.width / 2)
        .attr('y', -focusedDimensions.height / 2)
        .attr('stroke-width', 0);

    // Don't scale text - tile expansion handles the size increase
    // Text remains at normal size for readability
    nodeGroup.select('text')
        .transition()
        .duration(GRAPH_PHYSICS.ANIMATION_DURATION)
        .attr('transform', 'scale(1)');

    // Create the header bar with symbols
    createFocusHeader(nodeGroup, node, focusedDimensions);

    // Create the footer bar with contextual info
    createFocusFooter(nodeGroup, node, focusedDimensions);
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

    const preFocusTransform = getPreFocusTransform();
    console.log('[focus] unfocus', {
        focusedId,
        hadPreFocusTransform: preFocusTransform !== null
    });

    // Show UI elements again
    setFocusUIVisibility(true);

    // Restore normal physics (remove position forces, restore repulsion)
    restoreNormalPhysics();

    // Restore the focused tile to normal size
    const nodeGroup = g.selectAll<SVGGElement, D3Node>('.node')
        .filter((d) => d.id === focusedId);

    // Remove the header and footer
    removeFocusHeader(nodeGroup);
    removeFocusFooter(nodeGroup);

    nodeGroup.select('rect')
        .transition()
        .duration(GRAPH_PHYSICS.ANIMATION_DURATION)
        .attr('width', DEFAULT_TILE_WIDTH)
        .attr('height', DEFAULT_TILE_HEIGHT)
        .attr('x', -DEFAULT_TILE_WIDTH / 2)
        .attr('y', -DEFAULT_TILE_HEIGHT / 2)
        .attr('stroke-width', 2);

    nodeGroup.select('text')
        .transition()
        .duration(GRAPH_PHYSICS.ANIMATION_DURATION)
        .attr('transform', 'scale(1)');

    // Restore the previous transform if available
    if (preFocusTransform) {
        // Set flag to prevent unfocus detection during programmatic zoom animation
        setIsFocusAnimating(true);

        svg.transition()
            .duration(GRAPH_PHYSICS.ANIMATION_DURATION)
            .call(zoom.transform, d3.zoomIdentity
                .translate(preFocusTransform.x, preFocusTransform.y)
                .scale(preFocusTransform.k))
            .on("end", () => {
                // Clear animation flag once zoom transition completes
                setIsFocusAnimating(false);
            });
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
export function handleUnfocusTrigger(_event: any): void {
    if (!isFocused()) return;

    // Determine if this is an unfocus-worthy interaction
    // Zoom out (scale decreased) or pan (translate changed significantly)
    const currentTransform = getTransform();

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

/**
 * Handle keyboard events for focus mode
 */
function handleFocusKeydown(event: KeyboardEvent): void {
    if (event.key === 'Escape' && isFocused()) {
        event.preventDefault();
        unfocus();
    }
}

/**
 * Handle window resize while in focus mode
 * Recomputes tile dimensions and re-centers the focused tile
 */
function handleFocusResize(): void {
    const focusedId = getFocusedNodeId();
    if (!focusedId) return;

    const g = getG();
    if (!g) return;

    // Find the focused node data
    const nodeData = g.selectAll<SVGGElement, D3Node>('.node')
        .data()
        .find((d) => d.id === focusedId);

    if (!nodeData) return;

    // Recalculate dimensions based on new viewport size
    const focusedDimensions = calculateFocusedTileDimensions();

    // Re-focus the tile with new dimensions (transition between focus states)
    focusOnTile(nodeData);
}

/**
 * Initialize focus mode keyboard listeners and resize handler
 * Should be called once when the graph is initialized
 */
export function initFocusKeyboardSupport(): void {
    document.addEventListener('keydown', handleFocusKeydown);
    window.addEventListener('resize', handleFocusResize);
}

/**
 * Clean up focus mode keyboard listeners and resize handler
 */
export function cleanupFocusKeyboardSupport(): void {
    document.removeEventListener('keydown', handleFocusKeydown);
    window.removeEventListener('resize', handleFocusResize);
}
