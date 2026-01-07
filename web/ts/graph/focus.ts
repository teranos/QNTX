// Tile focus functionality
// Handles focusing viewport on a tile and expanding it to fill most of the view

import { GRAPH_PHYSICS, appState } from '../config.ts';
import { getSimulation, getSvg, getG, getZoom, getDomCache, getFocusedNodeId, getPreFocusTransform, setFocusedNodeId, setPreFocusTransform, setIsFocusAnimating } from './state.ts';
import { getTransform } from './transform.ts';
import { AX, BY, AS, CommandDescriptions } from '../../../types/generated/typescript/sym.ts';
import { calculateFocusedTileDimensions, DEFAULT_TILE_WIDTH, DEFAULT_TILE_HEIGHT } from './focus/dimensions.ts';
import type { D3Node } from '../../types/d3-graph';
import type { Node } from '../../types/core';

// Import D3 from vendor bundle
declare const d3: any;

// Header configuration
const HEADER_HEIGHT = 24;
const HEADER_PADDING = 6;
const HEADER_SYMBOL_SIZE = 16;
const HEADER_SYMBOL_SPACING = 6;

// Footer configuration
const FOOTER_HEIGHT = 20;
const FOOTER_PADDING = 6;

// Header symbols with their actions
interface HeaderSymbol {
    symbol: string;
    command: string;
    description: string;
    action: (node: D3Node) => void;
}

const HEADER_SYMBOLS: HeaderSymbol[] = [
    {
        symbol: AX,
        command: 'ax',
        description: CommandDescriptions['ax'],
        action: (node: D3Node) => {
            console.log(`[⋈ ax] Expand context for: ${node.label}`);
            // TODO: Implement expand related nodes
        }
    },
    {
        symbol: BY,
        command: 'by',
        description: CommandDescriptions['by'],
        action: (node: D3Node) => {
            console.log(`[⌬ by] Show provenance for: ${node.label}`);
            // TODO: Implement show provenance
        }
    },
    {
        symbol: AS,
        command: 'as',
        description: CommandDescriptions['as'],
        action: (node: D3Node) => {
            console.log(`[+ as] Add attestation to: ${node.label}`);
            // TODO: Implement add attestation
        }
    }
];

/**
 * Adjust simulation physics for focus mode
 * - Pin the focused tile in place with a position force
 * - Reduce charge strength to bring other tiles closer
 * - Reduce collision radius to allow tighter packing
 */
function adjustPhysicsForFocus(focusedNode: D3Node): void {
    const simulation = getSimulation();
    if (!simulation) return;

    const container = getDomCache().get('graphContainer', '#graph-container');
    if (!container) return;

    const viewportWidth = container.clientWidth;
    const viewportHeight = container.clientHeight;

    // Add a position force to pin the focused tile at the center
    simulation.force('focus-position', d3.forceX(viewportWidth / 2)
        .x((d: D3Node) => d.id === focusedNode.id ? viewportWidth / 2 : d.x!)
        .strength((d: D3Node) => d.id === focusedNode.id ? GRAPH_PHYSICS.FOCUS_POSITION_STRENGTH : 0));

    simulation.force('focus-position-y', d3.forceY(viewportHeight / 2)
        .y((d: D3Node) => d.id === focusedNode.id ? viewportHeight / 2 : d.y!)
        .strength((d: D3Node) => d.id === focusedNode.id ? GRAPH_PHYSICS.FOCUS_POSITION_STRENGTH : 0));

    // Reduce charge strength to bring other tiles closer
    simulation.force('charge', d3.forceManyBody()
        .strength(GRAPH_PHYSICS.FOCUS_CHARGE_STRENGTH)
        .distanceMax(GRAPH_PHYSICS.CHARGE_MAX_DISTANCE));

    // Reduce collision radius to allow tighter packing
    simulation.force('collision', d3.forceCollide()
        .radius(GRAPH_PHYSICS.FOCUS_COLLISION_RADIUS)
        .strength(1));

    // Reheat the simulation to apply changes
    simulation.alpha(0.3).restart();
}

/**
 * Restore normal simulation physics after unfocus
 */
function restoreNormalPhysics(): void {
    const simulation = getSimulation();
    if (!simulation) return;

    // Remove focus-specific position forces
    simulation.force('focus-position', null);
    simulation.force('focus-position-y', null);

    // Restore normal charge strength
    simulation.force('charge', d3.forceManyBody()
        .strength(GRAPH_PHYSICS.TILE_CHARGE_STRENGTH)
        .distanceMax(GRAPH_PHYSICS.CHARGE_MAX_DISTANCE));

    // Restore normal collision radius
    simulation.force('collision', d3.forceCollide()
        .radius(GRAPH_PHYSICS.COLLISION_RADIUS)
        .strength(1));

    // Gently reheat the simulation
    simulation.alpha(0.2).restart();
}

/**
 * Show or hide UI elements based on focus state
 * Slides elements out of view during focus zoom, slides back on unfocus
 * Also fades in a darker background to indicate focus mode
 */
function setFocusUIVisibility(visible: boolean): void {
    const duration = GRAPH_PHYSICS.ANIMATION_DURATION;
    const transition = `transform ${duration}ms ease, opacity ${duration}ms ease`;

    // Darken the graph background when focused using a CSS overlay
    // This ensures the overlay is clipped to the graph-container bounds
    const container = document.getElementById('graph-container');
    if (container) {
        let overlay = container.querySelector('.focus-overlay') as HTMLElement;

        if (visible) {
            // Remove overlay when unfocusing
            if (overlay) {
                overlay.style.opacity = '0';
                setTimeout(() => overlay?.remove(), duration);
            }
        } else {
            // Create overlay when focusing
            if (!overlay) {
                overlay = document.createElement('div');
                overlay.className = 'focus-overlay';
                overlay.style.cssText = `
                    position: absolute;
                    top: 0;
                    left: 0;
                    right: 0;
                    bottom: 0;
                    background: rgba(0, 0, 0, 0.4);
                    pointer-events: auto;
                    cursor: pointer;
                    opacity: 0;
                    transition: opacity ${duration}ms ease;
                    z-index: 1;
                `;
                // Click overlay to unfocus
                overlay.addEventListener('click', (event) => {
                    event.preventDefault();
                    event.stopPropagation();
                    unfocus();
                });
                container.appendChild(overlay);
            }
            // Trigger opacity transition
            requestAnimationFrame(() => {
                if (overlay) overlay.style.opacity = '1';
            });
        }
    }

    // Helper to slide element left
    const slideLeft = (el: HTMLElement | null) => {
        if (!el) return;
        el.style.setProperty('transition', transition, 'important');
        if (visible) {
            el.style.setProperty('transform', 'translateX(0)', 'important');
            el.style.setProperty('opacity', '1', 'important');
            el.style.setProperty('pointer-events', 'auto', 'important');
        } else {
            el.style.setProperty('transform', 'translateX(-100%)', 'important');
            el.style.setProperty('opacity', '0', 'important');
            el.style.setProperty('pointer-events', 'none', 'important');
        }
    };

    const domCache = getDomCache();

    // Left side elements (slide left)
    slideLeft(domCache.get('legenda', '.legenda'));
    slideLeft(document.getElementById('left-panel'));
    // TODO: When #controls is renamed to #legenda-container, update this selector
    slideLeft(document.getElementById('controls')); // Contains legenda

    // Expand graph-container to full width when focused
    // TODO: When #graph-container is renamed to #graph-viewer, update this selector
    const graphContainer = domCache.get('graphContainer', '#graph-container');
    if (graphContainer) {
        graphContainer.style.setProperty('transition', transition, 'important');
        if (visible) {
            // Restore normal flex layout
            graphContainer.style.removeProperty('position');
            graphContainer.style.removeProperty('left');
            graphContainer.style.removeProperty('right');
            graphContainer.style.removeProperty('width');
        } else {
            // Expand to fill entire viewport
            graphContainer.style.setProperty('position', 'fixed', 'important');
            graphContainer.style.setProperty('left', '0', 'important');
            graphContainer.style.setProperty('right', '0', 'important');
            graphContainer.style.setProperty('width', '100%', 'important');
        }
    }

    // Virtue #9: Responsive Intent - Adapt to device context, not just size
    // System drawer slides based on position (top on mobile, bottom on desktop)
    const systemDrawer = document.getElementById('system-drawer');
    if (systemDrawer) {
        const computedStyle = window.getComputedStyle(systemDrawer);
        const isAtTop = computedStyle.top !== 'auto' && computedStyle.bottom === 'auto';

        systemDrawer.style.transition = transition;
        if (visible) {
            systemDrawer.style.transform = 'translateY(0)';
            systemDrawer.style.opacity = '1';
            systemDrawer.style.pointerEvents = 'auto';
        } else {
            // Slide up if at top (mobile), slide down if at bottom (desktop)
            systemDrawer.style.transform = isAtTop ? 'translateY(-120%)' : 'translateY(120%)';
            systemDrawer.style.opacity = '0.5';
            systemDrawer.style.pointerEvents = 'none';
        }
    }

    // Symbol palette (slides based on position)
    // - Mobile (bottom): slides down
    // - Tablet portrait (fixed left): slides left
    // - Desktop (in left panel): slides left with parent
    const symbolPalette = document.getElementById('symbolPalette');
    if (symbolPalette) {
        const computedStyle = window.getComputedStyle(symbolPalette);
        const isFixed = computedStyle.position === 'fixed';
        const isAtLeft = isFixed && computedStyle.left === '0px';

        symbolPalette.style.transition = transition;
        if (visible) {
            symbolPalette.style.transform = isAtLeft ? 'translateX(0) translateY(-50%)' : 'translateY(0)';
            symbolPalette.style.opacity = '1';
            symbolPalette.style.pointerEvents = 'auto';
        } else {
            if (isAtLeft) {
                // Tablet portrait - fixed left column, slide left
                symbolPalette.style.transform = 'translateX(-120%) translateY(-50%)';
            } else {
                // Mobile/desktop - slide down (mobile at bottom) or handled by parent
                symbolPalette.style.transform = 'translateY(120%)';
            }
            symbolPalette.style.opacity = '0.5';
            symbolPalette.style.pointerEvents = 'none';
        }
    }
}

/**
 * Create the header bar with symbols above the focused tile
 */
function createFocusHeader(nodeGroup: any, node: D3Node, dimensions: { width: number; height: number; scale: number }): void {
    // Remove any existing header
    nodeGroup.select('.focus-header').remove();

    // Calculate header position (above the tile)
    const headerY = -dimensions.height / 2 - HEADER_HEIGHT - HEADER_PADDING;
    const headerWidth = HEADER_SYMBOLS.length * (HEADER_SYMBOL_SIZE + HEADER_SYMBOL_SPACING) + HEADER_PADDING;

    // Create header group
    const header = nodeGroup.append('g')
        .attr('class', 'focus-header')
        .attr('transform', `translate(0, ${headerY})`)
        .style('opacity', 0);

    // Header background - match symbol palette styling
    header.append('rect')
        .attr('class', 'focus-header-bg')
        .attr('x', -headerWidth / 2)
        .attr('y', 0)
        .attr('width', headerWidth)
        .attr('height', HEADER_HEIGHT)
        .attr('rx', 3)
        .attr('ry', 3)
        .attr('fill', '#f8f8f8')
        .attr('stroke', '#ddd')
        .attr('stroke-width', 1);

    // Add symbol buttons
    const startX = -headerWidth / 2 + HEADER_PADDING + HEADER_SYMBOL_SIZE / 2;

    HEADER_SYMBOLS.forEach((symbolDef, index) => {
        const buttonX = startX + index * (HEADER_SYMBOL_SIZE + HEADER_SYMBOL_SPACING);

        const button = header.append('g')
            .attr('class', 'focus-header-button')
            .attr('transform', `translate(${buttonX}, ${HEADER_HEIGHT / 2})`)
            .style('cursor', 'pointer');

        // Button background (hover target)
        button.append('circle')
            .attr('r', HEADER_SYMBOL_SIZE / 2)
            .attr('fill', 'transparent')
            .attr('stroke', 'transparent');

        // Symbol text - match symbol palette color
        button.append('text')
            .attr('text-anchor', 'middle')
            .attr('dominant-baseline', 'central')
            .attr('font-size', '14px')
            .attr('fill', '#000')
            .text(symbolDef.symbol);

        // Hover effects - match symbol palette
        button.on('mouseenter', function(this: SVGGElement) {
            d3.select(this).select('circle')
                .attr('fill', '#e8e8e8');
            d3.select(this).select('text')
                .attr('fill', '#000');
        });

        button.on('mouseleave', function(this: SVGGElement) {
            d3.select(this).select('circle')
                .attr('fill', 'transparent');
            d3.select(this).select('text')
                .attr('fill', '#000');
        });

        // Click handler
        button.on('click', (event: MouseEvent) => {
            event.stopPropagation();
            symbolDef.action(node);
        });

        // Tooltip on hover
        button.append('title')
            .text(`${symbolDef.symbol} ${symbolDef.command}: ${symbolDef.description}`);
    });

    // Animate header in
    header.transition()
        .duration(GRAPH_PHYSICS.ANIMATION_DURATION)
        .style('opacity', 1);
}

/**
 * Remove the focus header from a node
 */
function removeFocusHeader(nodeGroup: any): void {
    nodeGroup.select('.focus-header')
        .transition()
        .duration(GRAPH_PHYSICS.ANIMATION_DURATION)
        .style('opacity', 0)
        .remove();
}

/**
 * Count connections for a node from the current graph data
 */
function countNodeConnections(nodeId: string): { incoming: number; outgoing: number; total: number } {
    const graphData = appState.currentGraphData;
    if (!graphData || !graphData.links) {
        return { incoming: 0, outgoing: 0, total: 0 };
    }

    let incoming = 0;
    let outgoing = 0;

    graphData.links.forEach(link => {
        const sourceId = (typeof link.source === 'object' && link.source !== null)
            ? (link.source as Node).id
            : link.source as string;
        const targetId = (typeof link.target === 'object' && link.target !== null)
            ? (link.target as Node).id
            : link.target as string;

        if (sourceId === nodeId) outgoing++;
        if (targetId === nodeId) incoming++;
    });

    return { incoming, outgoing, total: incoming + outgoing };
}

/**
 * Create the footer bar with contextual information below the focused tile
 */
function createFocusFooter(nodeGroup: any, node: D3Node, dimensions: { width: number; height: number; scale: number }): void {
    // Remove any existing footer
    nodeGroup.select('.focus-footer').remove();

    // Calculate footer position (below the tile)
    const footerY = dimensions.height / 2 + FOOTER_PADDING;
    const footerWidth = dimensions.width * 0.9;

    // Get connection counts
    const connections = countNodeConnections(node.id);

    // Build contextual info items
    const infoItems: string[] = [];
    infoItems.push(node.type);
    if (connections.total > 0) {
        infoItems.push(`${connections.total} connection${connections.total !== 1 ? 's' : ''}`);
    }
    // Add first metadata value if available
    if (node.metadata) {
        const keys = Object.keys(node.metadata).filter(k => k !== 'original_id');
        if (keys.length > 0) {
            const firstKey = keys[0];
            const value = String(node.metadata[firstKey]).substring(0, 20);
            infoItems.push(`${firstKey}: ${value}`);
        }
    }

    // Create footer group
    const footer = nodeGroup.append('g')
        .attr('class', 'focus-footer')
        .attr('transform', `translate(0, ${footerY})`)
        .style('opacity', 0);

    // Footer background - subtle light style
    footer.append('rect')
        .attr('class', 'focus-footer-bg')
        .attr('x', -footerWidth / 2)
        .attr('y', 0)
        .attr('width', footerWidth)
        .attr('height', FOOTER_HEIGHT)
        .attr('rx', 3)
        .attr('ry', 3)
        .attr('fill', '#f8f8f8')
        .attr('stroke', '#ddd')
        .attr('stroke-width', 1);

    // Footer text - contextual info
    footer.append('text')
        .attr('class', 'focus-footer-text')
        .attr('text-anchor', 'middle')
        .attr('dominant-baseline', 'central')
        .attr('y', FOOTER_HEIGHT / 2)
        .attr('font-size', '10px')
        .attr('fill', '#666')
        .text(infoItems.join(' · '));

    // Animate footer in
    footer.transition()
        .duration(GRAPH_PHYSICS.ANIMATION_DURATION)
        .style('opacity', 1);
}

/**
 * Remove the focus footer from a node
 */
function removeFocusFooter(nodeGroup: any): void {
    nodeGroup.select('.focus-footer')
        .transition()
        .duration(GRAPH_PHYSICS.ANIMATION_DURATION)
        .style('opacity', 0)
        .remove();
}

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
        return;
    }

    const domCache = getDomCache();
    const container = domCache.get('graphContainer', '#graph-container');
    if (!container) return;

    const previouslyFocusedId = getFocusedNodeId();
    const isTransition = previouslyFocusedId !== null && previouslyFocusedId !== node.id;

    // If transitioning between tiles, restore the previous tile to normal size
    if (isTransition) {
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

    // Calculate transform to center the node at canonical zoom
    const targetX = viewportWidth / 2 - node.x * canonicalZoom;
    const targetY = viewportHeight / 2 - node.y * canonicalZoom;

    // Set flag to prevent unfocus detection during programmatic zoom animation
    setIsFocusAnimating(true);

    // Animate to canonical zoom and center on the tile
    svg.transition()
        .duration(GRAPH_PHYSICS.ANIMATION_DURATION)
        .call(zoom.transform, d3.zoomIdentity
            .translate(targetX, targetY)
            .scale(canonicalZoom))  // Reset to canonical zoom level
        .on("end", () => {
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

    // Expand the rect
    nodeGroup.select('rect')
        .transition()
        .duration(GRAPH_PHYSICS.ANIMATION_DURATION)
        .attr('width', focusedDimensions.width)
        .attr('height', focusedDimensions.height)
        .attr('x', -focusedDimensions.width / 2)
        .attr('y', -focusedDimensions.height / 2);

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

    // Adjust physics to make other tiles move closer
    adjustPhysicsForFocus(node);
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
        .attr('y', -DEFAULT_TILE_HEIGHT / 2);

    nodeGroup.select('text')
        .transition()
        .duration(GRAPH_PHYSICS.ANIMATION_DURATION)
        .attr('transform', 'scale(1)');

    // Restore the previous transform if available
    const preFocusTransform = getPreFocusTransform();
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
