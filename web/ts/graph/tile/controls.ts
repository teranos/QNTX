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

/**
 * Create array field tags (skills, languages, certifications) for focused tiles
 * Tags are rendered as interactive badges that can be clicked for navigation
 */
export function createArrayFieldTags(nodeGroup: any, node: D3Node, dimensions: { width: number; height: number }): void {
    console.log('[array-tags] createArrayFieldTags called', {
        nodeId: node.id,
        nodeType: node.type,
        hasGraphData: !!appState.currentGraphData,
        hasMeta: !!appState.currentGraphData?.meta,
        nodeTypes: appState.currentGraphData?.meta?.node_types
    });

    // Remove any existing tags
    nodeGroup.select('.focus-array-tags').remove();

    // Get array fields from node type metadata
    const nodeType = appState.currentGraphData?.meta?.node_types?.find(nt => nt.type === node.type);
    console.log('[array-tags] nodeType lookup', { nodeType, searchingFor: node.type });

    if (!nodeType || !nodeType.array_fields || nodeType.array_fields.length === 0) {
        console.log('[array-tags] early return - no array fields', {
            hasNodeType: !!nodeType,
            arrayFields: nodeType?.array_fields
        });
        return; // No array fields defined for this type
    }

    // Collect all tag values from node metadata
    const tags: Array<{ field: string; value: string }> = [];
    for (const fieldName of nodeType.array_fields) {
        const fieldValue = node.metadata?.[fieldName];
        if (fieldValue && typeof fieldValue === 'string') {
            // Parse comma-separated values
            const values = fieldValue.split(',').map(v => v.trim()).filter(v => v.length > 0);
            for (const value of values) {
                tags.push({ field: fieldName, value });
            }
        }
    }

    if (tags.length === 0) {
        return; // No tag values to display
    }

    // Tag styling constants
    const TAG_HEIGHT = 20;
    const TAG_PADDING_X = 8;
    const TAG_PADDING_Y = 4;
    const TAG_SPACING = 6;
    const TAG_Y_OFFSET = -dimensions.height / 2 + HEADER_HEIGHT + HEADER_PADDING + 10;

    // Create tags container
    const tagsGroup = nodeGroup.append('g')
        .attr('class', 'focus-array-tags')
        .attr('transform', `translate(0, ${TAG_Y_OFFSET})`)
        .style('opacity', 0);

    // Render tags with dynamic positioning
    let currentX = -dimensions.width / 2 + 10; // Start from left edge with padding
    let currentY = 0;
    const maxWidth = dimensions.width - 20; // Leave padding on both sides

    tags.forEach((tag, index) => {
        // Estimate tag width (rough approximation based on text length)
        const estimatedWidth = tag.value.length * 7 + TAG_PADDING_X * 2;

        // Wrap to next line if needed
        if (currentX + estimatedWidth > dimensions.width / 2 - 10 && index > 0) {
            currentX = -dimensions.width / 2 + 10;
            currentY += TAG_HEIGHT + TAG_SPACING;
        }

        const tagGroup = tagsGroup.append('g')
            .attr('class', 'focus-tag')
            .attr('transform', `translate(${currentX}, ${currentY})`)
            .style('cursor', 'pointer');

        // Tag background
        const tagBg = tagGroup.append('rect')
            .attr('class', 'focus-tag-bg')
            .attr('height', TAG_HEIGHT)
            .attr('rx', 3)
            .attr('ry', 3)
            .attr('fill', '#3498db')
            .attr('opacity', 0.8);

        // Tag text
        const tagText = tagGroup.append('text')
            .attr('class', 'focus-tag-text')
            .attr('x', TAG_PADDING_X)
            .attr('y', TAG_HEIGHT / 2)
            .attr('font-size', '11px')
            .attr('fill', '#fff')
            .attr('dominant-baseline', 'central')
            .style('user-select', 'text')
            .style('cursor', 'text')
            .text(tag.value);

        // Measure actual text width and update background
        const textWidth = (tagText.node() as SVGTextElement).getComputedTextLength();
        const tagWidth = textWidth + TAG_PADDING_X * 2;
        tagBg.attr('width', tagWidth);

        // Click handler for future navigation
        tagGroup.on('click', (event: MouseEvent) => {
            event.stopPropagation();
            console.log(`[Tag clicked] ${tag.field}: ${tag.value}`);
            // TODO: Implement filtering/navigation
            // - Filter graph to show only nodes with this tag value
            // - Or jump to another node with the same tag
        });

        // Hover effects
        tagGroup.on('mouseenter', function(this: SVGGElement) {
            d3.select(this).select('.focus-tag-bg')
                .attr('opacity', 1.0);
        });

        tagGroup.on('mouseleave', function(this: SVGGElement) {
            d3.select(this).select('.focus-tag-bg')
                .attr('opacity', 0.8);
        });

        // Update position for next tag
        currentX += tagWidth + TAG_SPACING;
    });

    // Animate tags in
    tagsGroup.transition()
        .duration(GRAPH_PHYSICS.ANIMATION_DURATION)
        .style('opacity', 1);
}

/**
 * Remove array field tags from a node
 */
export function removeArrayFieldTags(nodeGroup: any): void {
    nodeGroup.select('.focus-array-tags')
        .transition()
        .duration(GRAPH_PHYSICS.ANIMATION_DURATION)
        .style('opacity', 0)
        .remove();
}

/**
 * Create rich string content display (notes, description) for focused tiles
 * Renders word-wrapped text content below the tile center
 */
export function createRichStringContent(nodeGroup: any, node: D3Node, dimensions: { width: number; height: number }): void {
    // Remove any existing rich content
    nodeGroup.select('.focus-rich-content').remove();

    // Get rich string fields from node type metadata
    const nodeType = appState.currentGraphData?.meta?.node_types?.find(nt => nt.type === node.type);
    if (!nodeType || !nodeType.rich_string_fields || nodeType.rich_string_fields.length === 0) {
        return; // No rich string fields defined for this type
    }

    // Collect rich content from node metadata
    let richContent = '';
    for (const fieldName of nodeType.rich_string_fields) {
        const fieldValue = node.metadata?.[fieldName];
        if (fieldValue && typeof fieldValue === 'string' && fieldValue.trim().length > 0) {
            richContent = fieldValue.trim();
            break; // Use first available rich string field
        }
    }

    if (!richContent) {
        return; // No content to display
    }

    // Rich content styling constants
    const CONTENT_PADDING = 20;
    const CONTENT_Y_OFFSET = 60; // Below tile center
    const LINE_HEIGHT = 16;
    const FONT_SIZE = 12;
    const MAX_WIDTH = dimensions.width - (CONTENT_PADDING * 2);
    const MAX_LINES = 8; // Limit to prevent overflow

    // Create rich content container
    const contentGroup = nodeGroup.append('g')
        .attr('class', 'focus-rich-content')
        .attr('transform', `translate(0, ${CONTENT_Y_OFFSET})`)
        .style('opacity', 0);

    // Word-wrap the text
    const words = richContent.split(/\s+/);
    const lines: string[] = [];
    let currentLine = '';

    // Simple word wrapping algorithm
    const tempText = contentGroup.append('text')
        .attr('font-size', `${FONT_SIZE}px`)
        .style('visibility', 'hidden');

    for (const word of words) {
        const testLine = currentLine ? `${currentLine} ${word}` : word;
        tempText.text(testLine);
        const textWidth = (tempText.node() as SVGTextElement).getComputedTextLength();

        if (textWidth > MAX_WIDTH && currentLine) {
            lines.push(currentLine);
            currentLine = word;
        } else {
            currentLine = testLine;
        }

        if (lines.length >= MAX_LINES - 1) {
            break; // Stop if we've hit the line limit
        }
    }

    if (currentLine && lines.length < MAX_LINES) {
        lines.push(currentLine);
    }

    tempText.remove();

    // Add ellipsis if truncated
    const wasTruncated = lines.length >= MAX_LINES && words.length > lines.join(' ').split(/\s+/).length;
    if (wasTruncated && lines.length > 0) {
        lines[lines.length - 1] += '...';
    }

    // Calculate content height
    const contentHeight = lines.length * LINE_HEIGHT + CONTENT_PADDING * 2;

    // Background for readability
    contentGroup.append('rect')
        .attr('x', -MAX_WIDTH / 2 - CONTENT_PADDING)
        .attr('y', -CONTENT_PADDING)
        .attr('width', MAX_WIDTH + CONTENT_PADDING * 2)
        .attr('height', contentHeight)
        .attr('rx', 4)
        .attr('fill', '#f8f8f8')
        .attr('stroke', '#ddd')
        .attr('stroke-width', 1);

    // Render each line of text
    lines.forEach((line, index) => {
        contentGroup.append('text')
            .attr('x', -MAX_WIDTH / 2)
            .attr('y', index * LINE_HEIGHT)
            .attr('font-size', `${FONT_SIZE}px`)
            .attr('fill', '#333')
            .style('user-select', 'text')
            .style('cursor', 'text')
            .text(line);
    });

    // Animate in
    contentGroup.transition()
        .duration(GRAPH_PHYSICS.ANIMATION_DURATION)
        .style('opacity', 1);
}

/**
 * Remove rich string content from a node
 */
export function removeRichStringContent(nodeGroup: any): void {
    nodeGroup.select('.focus-rich-content')
        .transition()
        .duration(GRAPH_PHYSICS.ANIMATION_DURATION)
        .style('opacity', 0)
        .remove();
}
