// Tile focus functionality
// Handles focusing viewport on a tile and expanding it to fill most of the view

import { GRAPH_PHYSICS, appState } from '../config.ts';
import { getSvg, getG, getZoom, getDomCache, getFocusedNodeId, getPreFocusTransform, setFocusedNodeId, setPreFocusTransform } from './state.ts';
import { getTransform } from './transform.ts';
import { AX, BY, AS, CommandDescriptions } from '../../../types/generated/typescript/sym.ts';
import type { D3Node } from '../../types/d3-graph';
import type { Node } from '../../types/core';

// Import D3 from vendor bundle
declare const d3: any;

// Default tile dimensions (must match renderer.ts)
const DEFAULT_TILE_WIDTH = 180;
const DEFAULT_TILE_HEIGHT = 80;

// Header configuration
const HEADER_HEIGHT = 32;
const HEADER_PADDING = 8;
const HEADER_SYMBOL_SIZE = 24;
const HEADER_SYMBOL_SPACING = 8;

// Footer configuration
const FOOTER_HEIGHT = 24;
const FOOTER_PADDING = 8;

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
 * Show or hide UI elements based on focus state
 * Slides elements out of view during focus zoom, slides back on unfocus
 */
function setFocusUIVisibility(visible: boolean): void {
    const duration = GRAPH_PHYSICS.ANIMATION_DURATION;
    const transition = `transform ${duration}ms ease, opacity ${duration}ms ease`;

    // Helper to slide element left
    const slideLeft = (el: HTMLElement | null) => {
        if (!el) return;
        el.style.transition = transition;
        if (visible) {
            el.style.transform = 'translateX(0)';
            el.style.opacity = '1';
            el.style.pointerEvents = 'auto';
        } else {
            el.style.transform = 'translateX(-120%)';
            el.style.opacity = '0.5';
            el.style.pointerEvents = 'none';
        }
    };

    const domCache = getDomCache();

    // Left side elements (slide left)
    slideLeft(domCache.get('legenda', '.legenda'));
    slideLeft(document.getElementById('left-panel'));

    // System drawer (slides up on mobile/top, down on desktop/bottom)
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

    // Header background
    header.append('rect')
        .attr('class', 'focus-header-bg')
        .attr('x', -headerWidth / 2)
        .attr('y', 0)
        .attr('width', headerWidth)
        .attr('height', HEADER_HEIGHT)
        .attr('rx', 6)
        .attr('ry', 6)
        .attr('fill', 'rgba(0, 0, 0, 0.8)')
        .attr('stroke', 'rgba(255, 255, 255, 0.2)')
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

        // Symbol text
        button.append('text')
            .attr('text-anchor', 'middle')
            .attr('dominant-baseline', 'central')
            .attr('font-size', '16px')
            .attr('fill', '#e0e0e0')
            .text(symbolDef.symbol);

        // Hover effects
        button.on('mouseenter', function() {
            d3.select(this).select('circle')
                .attr('fill', 'rgba(255, 255, 255, 0.15)');
            d3.select(this).select('text')
                .attr('fill', '#ffffff');
        });

        button.on('mouseleave', function() {
            d3.select(this).select('circle')
                .attr('fill', 'transparent');
            d3.select(this).select('text')
                .attr('fill', '#e0e0e0');
        });

        // Click handler
        button.on('click', function(event: MouseEvent) {
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

    // Footer background
    footer.append('rect')
        .attr('class', 'focus-footer-bg')
        .attr('x', -footerWidth / 2)
        .attr('y', 0)
        .attr('width', footerWidth)
        .attr('height', FOOTER_HEIGHT)
        .attr('rx', 4)
        .attr('ry', 4)
        .attr('fill', 'rgba(0, 0, 0, 0.6)')
        .attr('stroke', 'rgba(255, 255, 255, 0.1)')
        .attr('stroke-width', 1);

    // Footer text - contextual info
    footer.append('text')
        .attr('class', 'focus-footer-text')
        .attr('text-anchor', 'middle')
        .attr('dominant-baseline', 'central')
        .attr('y', FOOTER_HEIGHT / 2)
        .attr('font-size', '11px')
        .attr('fill', '#a0a0a0')
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

    // Hide UI elements when focused
    setFocusUIVisibility(false);

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

    // Show UI elements again
    setFocusUIVisibility(true);

    // Restore the focused tile to normal size
    const nodeGroup = g.selectAll('.node')
        .filter((d: D3Node) => d.id === focusedId);

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
