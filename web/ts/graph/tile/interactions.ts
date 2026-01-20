// Tile interactions - User interaction handlers for tiles
// Click to focus, hover for tooltip, context menu to hide

import { appState } from '../../state/app.ts';
import { getHiddenNodes } from '../state.ts';
import { focusOnTile } from '../focus.ts';
import type { D3Node } from '../../../types/d3-graph';
import type { GraphData } from '../../../types/core';

// Import D3 from vendor bundle
declare const d3: any;

/**
 * Setup click handler on tiles to trigger focus mode
 *
 * @param nodeSelection - D3 selection of node groups
 */
export function setupTileClickHandler(nodeSelection: any): void {
    nodeSelection.on("click", function(event: MouseEvent, d: D3Node) {
        event.stopPropagation(); // Prevent SVG click handler from firing
        focusOnTile(d);
    });
}

/**
 * Build tooltip content HTML from node data
 *
 * @param d - Node data
 * @returns HTML string for tooltip content
 */
function buildTooltipContent(d: D3Node): string {
    let content = '<strong>' + d.label + '</strong><br/>';
    content += '<div class="meta-item"><span class="meta-label">Type:</span> ' + d.type + '</div>';

    if (d.metadata) {
        Object.entries(d.metadata).forEach(([key, value]) => {
            if (key !== 'original_id') {
                content += '<div class="meta-item"><span class="meta-label">' + key + ':</span> ' + value + '</div>';
            }
        });
    }

    return content;
}

/**
 * Setup hover handlers on tiles to show tooltip with node details
 *
 * @param nodeSelection - D3 selection of node groups
 */
export function setupTileHoverHandlers(nodeSelection: any): void {
    const tooltip = d3.select("#tooltip");

    nodeSelection.on("mouseover", function(event: MouseEvent, d: D3Node) {
        const content = buildTooltipContent(d);
        tooltip.html(content)
            .style("left", (event.pageX + 15) + "px")
            .style("top", (event.pageY - 15) + "px")
            .style("opacity", 1);
    })
    .on("mouseout", function() {
        tooltip.style("opacity", 0);
    });
}

/**
 * Setup context menu handler on tiles to toggle visibility
 * Right-click a tile to hide it, right-click again to show it
 *
 * @param nodeSelection - D3 selection of node groups
 * @param rerenderCallback - Function to call to re-render the graph after visibility change
 */
export function setupTileContextMenu(nodeSelection: any, rerenderCallback: (data: GraphData) => void): void {
    const hiddenNodes = getHiddenNodes();

    nodeSelection.on("contextmenu", function(event: MouseEvent, d: D3Node) {
        // Prevent default context menu
        event.preventDefault();

        // Toggle node visibility
        if (hiddenNodes.has(d.id)) {
            hiddenNodes.delete(d.id);
        } else {
            hiddenNodes.add(d.id);
        }

        // Re-render graph with updated visibility
        if (appState.currentGraphData) {
            rerenderCallback(appState.currentGraphData);
        }
    });
}
