// Tile controls (header and footer bars) for focused tiles
// Header provides command buttons (ax, by, as)
// Footer displays contextual information (type, connections, metadata)

import { GRAPH_PHYSICS, appState } from '../../config.ts';
import { AX, BY, AS, CommandDescriptions } from '../../../../types/generated/typescript/sym.ts';
import type { D3Node } from '../../../types/d3-graph';
import type { Node } from '../../../types/core';

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
 * Create the header bar with command symbols above the focused tile
 * Header provides interactive buttons for tile operations (expand, provenance, add)
 */
export function createFocusHeader(nodeGroup: any, node: D3Node, dimensions: { width: number; height: number; scale: number }): void {
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
export function removeFocusHeader(nodeGroup: any): void {
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
 * Footer displays tile type, connection count, and metadata preview
 */
export function createFocusFooter(nodeGroup: any, node: D3Node, dimensions: { width: number; height: number; scale: number }): void {
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
export function removeFocusFooter(nodeGroup: any): void {
    nodeGroup.select('.focus-footer')
        .transition()
        .duration(GRAPH_PHYSICS.ANIMATION_DURATION)
        .style('opacity', 0)
        .remove();
}
