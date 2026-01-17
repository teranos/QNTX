// Link rendering for graph visualization
// Creates connection lines and labels between nodes

import type { D3Link, D3Node } from '../../types/d3-graph';

// Import D3 from vendor bundle
declare const d3: any;

/**
 * Create link lines connecting nodes
 * Line thickness reflects link weight
 *
 * @param g - D3 selection of the graph container group
 * @param links - Array of link data
 * @returns D3 selection of link lines
 */
export function createLinks(g: any, links: D3Link[]): any {
    return g.append("g")
        .selectAll("line")
        .data(links)
        .join("line")
        .attr("class", (d: D3Link) => "link " + d.type)
        .attr("stroke-width", (d: D3Link) => Math.sqrt((d.weight || 1) * 2));
}

/**
 * Create link labels showing relationship type
 * Labels positioned at midpoint of each link
 *
 * @param g - D3 selection of the graph container group
 * @param links - Array of link data
 * @returns D3 selection of link labels
 */
export function createLinkLabels(g: any, links: D3Link[]): any {
    return g.append("g")
        .selectAll("text")
        .data(links)
        .join("text")
        .attr("class", "link-label")
        .attr("font-size", 9)
        .attr("fill", "#8892b0")
        .attr("text-anchor", "middle")
        .attr("pointer-events", "none")
        .text((d: D3Link) => d.label || d.type);
}

/**
 * Update link positions on simulation tick
 * Links connect source and target node positions
 *
 * @param linkSelection - D3 selection of link lines
 */
export function updateLinkPositions(linkSelection: any): void {
    linkSelection
        .attr("x1", (d: D3Link) => (d.source as D3Node).x!)
        .attr("y1", (d: D3Link) => (d.source as D3Node).y!)
        .attr("x2", (d: D3Link) => (d.target as D3Node).x!)
        .attr("y2", (d: D3Link) => (d.target as D3Node).y!);
}

/**
 * Update link label positions on simulation tick
 * Labels positioned at midpoint between source and target
 *
 * @param linkLabelSelection - D3 selection of link labels
 */
export function updateLinkLabelPositions(linkLabelSelection: any): void {
    linkLabelSelection
        .attr("x", (d: D3Link) => ((d.source as D3Node).x! + (d.target as D3Node).x!) / 2)
        .attr("y", (d: D3Link) => ((d.source as D3Node).y! + (d.target as D3Node).y!) / 2);
}
