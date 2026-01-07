// Tile rendering - Visual representation of nodes in the knowledge graph
// Tiles are the primary user interface for interacting with attestations

import { GRAPH_STYLES } from '../../config.ts';
import { DEFAULT_TILE_WIDTH, DEFAULT_TILE_HEIGHT } from './constants.ts';
import type { D3Node } from '../../../types/d3-graph';

// Import D3 from vendor bundle
declare const d3: any;

/**
 * Create tile rectangle with color coding and diagnostic patterns
 *
 * @param nodeSelection - D3 selection of node groups
 * @param nodeColors - Map of node types to colors
 * @returns D3 selection of tile rectangles
 */
export function createTileRect(nodeSelection: any, nodeColors: Record<string, string>): any {
    return nodeSelection.append("rect")
        .attr("width", DEFAULT_TILE_WIDTH)
        .attr("height", DEFAULT_TILE_HEIGHT)
        .attr("x", -DEFAULT_TILE_WIDTH / 2)
        .attr("y", -DEFAULT_TILE_HEIGHT / 2)
        .attr("fill", (d: D3Node) => {
            // If type is in nodeColors (includes backend "untyped"), use that color
            if (nodeColors[d.type]) {
                return nodeColors[d.type];
            }
            // Problematic type: backend sent a type we don't have color for
            // This indicates backend/frontend mismatch - use striped pattern for diagnostic visibility
            return "url(#diagonal-stripe-pattern)";
        })
        .attr("stroke", GRAPH_STYLES.NODE_STROKE_COLOR)
        .attr("stroke-width", 2)
        .attr("aria-label", (d: D3Node) => `${d.type}: ${d.label}`);
}

/**
 * Create multi-line text content for tiles
 * Shows label (bold), type (subdued), and first metadata value
 *
 * @param nodeSelection - D3 selection of node groups
 */
export function createTileText(nodeSelection: any): void {
    nodeSelection.each(function(this: any, d: D3Node) {
        const textGroup = d3.select(this).append("text")
            .attr("text-anchor", "middle")
            .attr("fill", "#e0e0e0");

        // Line 1: Label (bold)
        textGroup.append("tspan")
            .attr("x", 0)
            .attr("dy", "-1em")
            .attr("font-size", "13px")
            .attr("font-weight", "bold")
            .text(d.label);

        // Line 2: Type (subdued)
        textGroup.append("tspan")
            .attr("x", 0)
            .attr("dy", "1.2em")
            .attr("font-size", "10px")
            .attr("fill", "#888")
            .text(d.type);

        // Line 3: First metadata value if available
        if (d.metadata && Object.keys(d.metadata).length > 0) {
            const firstKey = Object.keys(d.metadata)[0];
            const firstValue = d.metadata[firstKey];
            if (firstValue && firstKey !== 'original_id') {
                textGroup.append("tspan")
                    .attr("x", 0)
                    .attr("dy", "1.2em")
                    .attr("font-size", "9px")
                    .attr("fill", "#666")
                    .text(`${firstKey}: ${String(firstValue).substring(0, 20)}`);
            }
        }
    });
}

/**
 * Create SVG pattern definition for problematic/unknown node types
 * Diagonal stripe pattern provides diagnostic visibility for backend/frontend mismatches
 *
 * @param defs - D3 selection of SVG defs element
 */
export function createDiagnosticPattern(defs: any): void {
    const pattern = defs.append("pattern")
        .attr("id", "diagonal-stripe-pattern")
        .attr("patternUnits", "userSpaceOnUse")
        .attr("width", 8)
        .attr("height", 8)
        .attr("patternTransform", "rotate(45)");

    // Light gray background
    pattern.append("rect")
        .attr("width", 8)
        .attr("height", 8)
        .attr("fill", "#b0b0b0");

    // Dark gray stripes
    pattern.append("line")
        .attr("x1", 0)
        .attr("y1", 0)
        .attr("x2", 0)
        .attr("y2", 8)
        .attr("stroke", "#808080")
        .attr("stroke-width", 3);
}

/**
 * Update tile positions on simulation tick
 *
 * @param nodeSelection - D3 selection of node groups
 */
export function updateTilePositions(nodeSelection: any): void {
    nodeSelection.attr("transform", (d: D3Node) => `translate(${d.x},${d.y})`);
}
