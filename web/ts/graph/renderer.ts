// D3.js graph visualization
// Main rendering orchestration - delegates to specialized modules

import { appState, GRAPH_PHYSICS, GRAPH_STYLES } from '../config.ts';
import { uiState } from '../ui-state.ts';
import { hiddenNodeTypes, initLegendaToggles } from '../legenda.ts';
import { getLinkDistance, getLinkStrength } from './physics.ts';
import {
    getSimulation, getG, getHiddenNodes, getDomCache,
    setSimulation, setSvg, setG, setZoom, clearState
} from './state.ts';
import { normalizeNodeType, filterVisibleNodes } from './utils.ts';
import { createDragBehavior } from './interactions.ts';
import { getTransform, centerGraph } from './transform.ts';
import { focusOnTile, unfocus, isFocused, initFocusKeyboardSupport, cleanupFocusKeyboardSupport } from './focus.ts';
import type { GraphData, Node } from '../../types/core';
import type {
    D3Node,
    D3Link,
    ZoomEvent
} from '../../types/d3-graph';

// Import D3 from vendor bundle
declare const d3: any;

// Re-export for public API
export { filterVisibleNodes } from './utils.ts';
export { initGraphResize } from './interactions.ts';
export { getTransform, setTransform, centerGraph, resetZoom } from './transform.ts';

// Update graph with new data
export function updateGraph(data: GraphData): void {
    // Save graph data to state
    appState.currentGraphData = data;

    // Rebuild legend with node types from backend and re-attach event listeners
    initLegendaToggles(renderGraph, data);

    renderGraph(data);

    // Save session after graph renders
    saveCurrentSession();
}

function renderGraph(data: GraphData): void {
    // Virtue #1: Error Handling - Guard against invalid data
    if (!data || !Array.isArray(data.nodes) || !Array.isArray(data.links)) {
        console.error('Invalid graph data:', data);
        return;
    }

    // TODO(issue #10): Attestation-based in-tile documentation
    // Add documentation sections to tiles showing attestation-based docs
    // Support expandable/collapsible sections with markdown rendering
    // Query documentation attestations when tiles are selected

    const domCache = getDomCache();
    const container = domCache.get('graphContainer', '#graph-container');
    if (!container) {
        console.error('Graph container not found');
        return;
    }

    const width = container.clientWidth;
    const height = container.clientHeight;

    // Build color map from backend node_types metadata (single source of truth)
    const nodeColors: Record<string, string> = {};
    if (data.meta?.node_types) {
        data.meta.node_types.forEach(typeInfo => {
            nodeColors[typeInfo.type] = typeInfo.color;
        });
    }

    // Detect isolated nodes (nodes with no connections)
    const nodeHasLinks = new Set<string>();
    data.links.forEach(link => {
        const sourceId = (typeof link.source === 'object' && link.source !== null)
            ? (link.source as Node).id
            : link.source as string;
        const targetId = (typeof link.target === 'object' && link.target !== null)
            ? (link.target as Node).id
            : link.target as string;
        nodeHasLinks.add(sourceId);
        nodeHasLinks.add(targetId);
    });

    // Count isolated nodes (considering type visibility)
    const isolatedNodeCount = data.nodes.filter(node =>
        !hiddenNodeTypes.has(normalizeNodeType(node.type)) && !nodeHasLinks.has(node.id)
    ).length;

    // Show/hide isolated node toggle based on whether isolated nodes exist
    const isolatedToggle = domCache.get('isolatedToggle', 'isolated-toggle');
    if (isolatedToggle) {
        if (isolatedNodeCount > 0) {
            isolatedToggle.classList.remove('u-hidden');
            isolatedToggle.classList.add('u-flex');
        } else {
            isolatedToggle.classList.remove('u-flex');
            isolatedToggle.classList.add('u-hidden');
        }
    }

    // Detect which node types are present in the data
    const presentNodeTypes = new Set(data.nodes.map(node => normalizeNodeType(node.type)));

    // Show/hide legenda items based on node type presence (use cached query)
    const legendaItems = document.querySelectorAll('.legenda-item');
    legendaItems.forEach((item: Element) => {
        const htmlItem = item as HTMLElement;
        const typeNameSpan = item.querySelector('.legenda-type-name');
        if (typeNameSpan) {
            const nodeType = normalizeNodeType(typeNameSpan.textContent);
            if (presentNodeTypes.has(nodeType)) {
                htmlItem.classList.remove('u-hidden');
                htmlItem.classList.add('u-flex');
            } else {
                htmlItem.classList.remove('u-flex');
                htmlItem.classList.add('u-hidden');
            }
        }
    });

    // Show/hide entire legenda container if there are no nodes
    const legenda = domCache.get('legenda', '.legenda');
    if (legenda) {
        if (data.nodes.length > 0) {
            legenda.classList.remove('u-hidden');
            legenda.classList.add('u-block');
        } else {
            legenda.classList.remove('u-block');
            legenda.classList.add('u-hidden');
        }
    }

    // Phase 2: Filter based on backend-controlled visibility
    // Backend sets node.visible and link.hidden based on client preferences
    const visibleNodes = filterVisibleNodes(data.nodes);
    const visibleLinks = data.links.filter(link => !link.hidden);

    // Update stats with visible counts
    const nodeCountEl = document.getElementById('node-count');
    const linkCountEl = document.getElementById('link-count');
    if (nodeCountEl) nodeCountEl.textContent = String(visibleNodes.length);
    if (linkCountEl) linkCountEl.textContent = String(visibleLinks.length);

    // Virtue #3: Memory Management - Stop old simulation before creating new one
    // Avoid Sin #3: Memory Leaks - Always cleanup before recreation
    const oldSimulation = getSimulation();
    if (oldSimulation) {
        oldSimulation.stop();
    }

    // Clear existing graph
    d3.select("#graph").selectAll("*").remove();

    // Create SVG
    const svg = d3.select("#graph")
        .attr("width", width)
        .attr("height", height)
        // Virtue #4: Accessibility - Add ARIA labels for screen readers
        .attr("role", "img")
        .attr("aria-label", `Graph visualization with ${visibleNodes.length} nodes and ${visibleLinks.length} connections`);

    if (!svg) {
        console.error('[Graph] Failed to create SVG element');
        return;
    }
    setSvg(svg);

    // Add pattern definition for problematic/unknown types
    const defs = svg.append("defs");
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

    // Track zoom state for unfocus detection
    let zoomStartTransform: { x: number; y: number; k: number } | null = null;

    // Create zoom behavior
    const zoom = d3.zoom()
        // Virtue #5: Prudence - Use named constants instead of magic numbers
        .scaleExtent([GRAPH_PHYSICS.ZOOM_MIN, GRAPH_PHYSICS.ZOOM_MAX])
        .on("start", function(event: ZoomEvent) {
            // Capture starting transform for pan detection
            zoomStartTransform = {
                x: event.transform.x,
                y: event.transform.y,
                k: event.transform.k
            };
        })
        .on("zoom", function(event: ZoomEvent) {
            const g = getG();
            if (g) {
                g.attr("transform", event.transform.toString());
            }

            // Detect unfocus triggers while focused
            if (isFocused() && zoomStartTransform) {
                const currentScale = event.transform.k;
                const startScale = zoomStartTransform.k;

                // Unfocus if user zooms out significantly
                if (currentScale < startScale * 0.85) {
                    unfocus();
                    return;
                }

                // Unfocus if user pans significantly (more than 50px in either direction)
                const panDeltaX = Math.abs(event.transform.x - zoomStartTransform.x);
                const panDeltaY = Math.abs(event.transform.y - zoomStartTransform.y);
                if (panDeltaX > 50 || panDeltaY > 50) {
                    unfocus();
                    return;
                }
            }
        })
        .on("end", function() {
            // Save transform state after zoom/pan
            saveCurrentSession();
            zoomStartTransform = null;
        });

    setZoom(zoom);
    svg.call(zoom);

    // Click on empty SVG space unfocuses
    svg.on("click", function(event: MouseEvent) {
        // Only unfocus if clicking directly on the SVG (not on a node)
        if (event.target === svg.node() && isFocused()) {
            unfocus();
        }
    });

    // Initialize keyboard support for focus mode (Escape to unfocus)
    initFocusKeyboardSupport();

    // Create container group
    const g = svg.append("g");
    setG(g);

    // Convert to D3 nodes and links
    const d3Nodes = visibleNodes as D3Node[];
    const d3Links = visibleLinks as D3Link[];

    // Create force simulation with filtered data
    // Phase 2: Physics metadata from backend type system (issue #7)
    const relationshipMeta = data.meta?.relationship_types;
    const simulation = d3.forceSimulation(d3Nodes)
        .force("link", d3.forceLink(d3Links)
            .id((d: D3Node) => d.id)
            .distance((link: D3Link) => getLinkDistance(link, relationshipMeta))
            .strength((link: D3Link) => getLinkStrength(link, relationshipMeta)))
        .force("charge", d3.forceManyBody()
            .strength(GRAPH_PHYSICS.TILE_CHARGE_STRENGTH)
            .distanceMax(GRAPH_PHYSICS.CHARGE_MAX_DISTANCE))
        .force("center", d3.forceCenter(width / 2, height / 2).strength(GRAPH_PHYSICS.CENTER_STRENGTH))
        .force("collision", d3.forceCollide()
            .radius(GRAPH_PHYSICS.COLLISION_RADIUS)
            .strength(1));

    setSimulation(simulation);

    // Create links
    const link = g.append("g")
        .selectAll("line")
        .data(d3Links)
        .join("line")
        .attr("class", (d: D3Link) => "link " + d.type)
        .attr("stroke-width", (d: D3Link) => Math.sqrt((d.weight || 1) * 2));

    // Create link labels
    const linkLabel = g.append("g")
        .selectAll("text")
        .data(d3Links)
        .join("text")
        .attr("class", "link-label")
        .attr("font-size", 9)
        .attr("fill", "#8892b0")
        .attr("text-anchor", "middle")
        .attr("pointer-events", "none")
        .text((d: D3Link) => d.label || d.type);

    // Create nodes
    const node = g.append("g")
        .selectAll("g")
        .data(d3Nodes)
        .join("g")
        .attr("class", "node")
        .call(createDragBehavior(simulation));

    // Render tiles (rectangles with inline data)
    const tileWidth = 180;
    const tileHeight = 80;
    node.append("rect")
        .attr("width", tileWidth)
        .attr("height", tileHeight)
        .attr("x", -tileWidth / 2)
        .attr("y", -tileHeight / 2)
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

    // Multi-line text with metadata
    node.each(function(this: any, d: D3Node) {
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

    // Tooltip
    const hiddenNodes = getHiddenNodes();
    const tooltip = d3.select("#tooltip");

    // Click handler for tile focus
    node.on("click", function(event: MouseEvent, d: D3Node) {
        event.stopPropagation(); // Prevent SVG click handler from firing
        focusOnTile(d);
    });

    node.on("mouseover", function(event: MouseEvent, d: D3Node) {
        let content = '<strong>' + d.label + '</strong><br/>';
        content += '<div class="meta-item"><span class="meta-label">Type:</span> ' + d.type + '</div>';

        if (d.metadata) {
            Object.entries(d.metadata).forEach(([key, value]) => {
                if (key !== 'original_id') {
                    content += '<div class="meta-item"><span class="meta-label">' + key + ':</span> ' + value + '</div>';
                }
            });
        }

        tooltip.html(content)
            .style("left", (event.pageX + 15) + "px")
            .style("top", (event.pageY - 15) + "px")
            .style("opacity", 1);
    })
    .on("mouseout", function() {
        tooltip.style("opacity", 0);
    })
    .on("contextmenu", function(event: MouseEvent, d: D3Node) {
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
            renderGraph(appState.currentGraphData);
        }
    });

    // Update positions on tick
    simulation.on("tick", () => {
        link
            .attr("x1", (d: D3Link) => (d.source as D3Node).x!)
            .attr("y1", (d: D3Link) => (d.source as D3Node).y!)
            .attr("x2", (d: D3Link) => (d.target as D3Node).x!)
            .attr("y2", (d: D3Link) => (d.target as D3Node).y!);

        linkLabel
            .attr("x", (d: D3Link) => ((d.source as D3Node).x! + (d.target as D3Node).x!) / 2)
            .attr("y", (d: D3Link) => ((d.source as D3Node).y! + (d.target as D3Node).y!) / 2);

        node
            .attr("transform", (d: D3Node) => "translate(" + d.x + "," + d.y + ")");
    });

    // Auto-center if there are nodes
    if (data.nodes.length > 0) {
        setTimeout(centerGraph, 500);
    }
}

// Virtue #7: Cleanliness - Export cleanup function for when graph is destroyed
export function cleanupGraph(): void {
    cleanupFocusKeyboardSupport();
    clearState();
}

// Save current session to localStorage
function saveCurrentSession(): void {
    const transform = getTransform();
    if (transform) {
        appState.currentTransform = transform;
    }

    // NOTE: Not saving graphData - D3 object references don't serialize properly
    // Query will be re-run on page load instead
    uiState.setGraphSession({
        query: appState.currentQuery,
        verbosity: appState.currentVerbosity,
    });
}
