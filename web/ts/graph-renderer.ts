// D3.js graph visualization

import { state, GRAPH_PHYSICS, GRAPH_STYLES } from './config.ts';
import { saveSession } from './state-manager.ts';
import { hiddenNodeTypes, hideIsolated, revealRelatedActive, initLegendaToggles } from './legenda.ts';
import type { GraphData, Node, Link, Transform } from '../types/core';
import type {
    D3Node,
    D3Link,
    ForceSimulation,
    SVGSelection,
    GroupSelection,
    ZoomBehavior,
    NodeSelection,
    LinkSelection,
    DragEvent,
    ZoomEvent
} from '../types/d3-graph';

// Import D3 from vendor bundle
declare const d3: any;

let simulation: ForceSimulation | null = null;
let svg: SVGSelection | null = null;
let g: GroupSelection | null = null;
let zoom: ZoomBehavior | null = null;

// Individual node visibility state (by node ID)
const hiddenNodes = new Set<string>();

// Tiles are the only rendering mode
const useTiles = true;

// Virtue #2: Performance - Cache DOM references to avoid repeated queries
interface DOMCache {
    graphContainer: HTMLElement | null;
    isolatedToggle: HTMLElement | null;
    legenda: HTMLElement | null;
    get(key: keyof DOMCache, selector: string): HTMLElement | null;
    clear(): void;
}

const domCache: DOMCache = {
    graphContainer: null,
    isolatedToggle: null,
    legenda: null,
    get: function(key: keyof DOMCache, selector: string): HTMLElement | null {
        if (!this[key]) {
            const element = document.getElementById(selector) || document.querySelector(selector) as HTMLElement | null;
            (this as any)[key] = element;
        }
        return this[key] as HTMLElement | null;
    },
    clear: function(): void {
        this.graphContainer = null;
        this.isolatedToggle = null;
        this.legenda = null;
    }
};

// Helper: Normalize node type for comparison (DRY)
function normalizeNodeType(type: string | null | undefined): string {
    return (type || '').trim().toLowerCase();
}

// Virtue #6: Modularity - Extract node filtering logic
function isConnectedToRevealedType(
    nodeId: string,
    links: Link[],
    nodes: Node[],
    revealedTypes: Set<string>
): boolean {
    if (revealedTypes.size === 0) return false;

    for (const link of links) {
        const sourceId = (typeof link.source === 'object' && link.source !== null)
            ? (link.source as Node).id
            : link.source;
        const targetId = (typeof link.target === 'object' && link.target !== null)
            ? (link.target as Node).id
            : link.target;

        // If this node is connected to another node...
        if (sourceId === nodeId || targetId === nodeId) {
            const connectedNodeId = sourceId === nodeId ? targetId : sourceId;
            const connectedNode = nodes.find(n => n.id === connectedNodeId);

            // ...and that connected node has a revealed type
            if (connectedNode && revealedTypes.has(normalizeNodeType(connectedNode.type))) {
                return true;
            }
        }
    }
    return false;
}

// Phase 2: Backend controls visibility - frontend just filters based on backend's decision
// Backend sets node.visible and link.hidden based on client preferences
export function filterVisibleNodes(nodes: Node[]): Node[] {
    return nodes.filter(node => node.visible !== false);
}

// Update graph with new data
export function updateGraph(data: GraphData): void {
    // Save graph data to state
    state.currentGraphData = data;

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
    const defaultColor = nodeColors['entity'] || '#95a5a6'; // Gray fallback

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
        isolatedToggle.style.display = isolatedNodeCount > 0 ? 'flex' : 'none';
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
            htmlItem.style.display = presentNodeTypes.has(nodeType) ? 'flex' : 'none';
        }
    });

    // Show/hide entire legenda container if there are no nodes
    const legenda = domCache.get('legenda', '.legenda');
    if (legenda) {
        legenda.style.display = data.nodes.length > 0 ? 'block' : 'none';
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
    if (simulation) {
        simulation.stop();
    }

    // Clear existing graph
    d3.select("#graph").selectAll("*").remove();

    // Create SVG
    svg = d3.select("#graph")
        .attr("width", width)
        .attr("height", height)
        // Virtue #4: Accessibility - Add ARIA labels for screen readers
        .attr("role", "img")
        .attr("aria-label", `Graph visualization with ${visibleNodes.length} nodes and ${visibleLinks.length} connections`);

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

    // Create zoom behavior
    zoom = d3.zoom()
        // Virtue #5: Prudence - Use named constants instead of magic numbers
        .scaleExtent([GRAPH_PHYSICS.ZOOM_MIN, GRAPH_PHYSICS.ZOOM_MAX])
        .on("zoom", function(event: ZoomEvent) {
            if (g) {
                g.attr("transform", event.transform.toString());
            }
        })
        .on("end", function() {
            // Save transform state after zoom/pan
            saveCurrentSession();
        });

    if (svg && zoom) {
        svg.call(zoom);
    }

    // Create container group
    g = svg!.append("g");

    // Convert to D3 nodes and links
    const d3Nodes = visibleNodes as D3Node[];
    const d3Links = visibleLinks as D3Link[];

    // Create force simulation with filtered data
    simulation = d3.forceSimulation(d3Nodes)
        .force("link", d3.forceLink(d3Links)
            .id((d: D3Node) => d.id)
            .distance((d: D3Link) => {
                // Shorter distance for git parent-child relationships to create tight commit chains
                if (d.type === 'is_child_of') return 50;
                // Shorter distance for branch pointers
                if (d.type === 'points_to') return 60;
                // Default distance for other relationships
                return GRAPH_PHYSICS.LINK_DISTANCE;
            })
            .strength((d: D3Link) => {
                // Weaker strength for more flexible, elastic links
                // Git relationships still get some priority but not rigid
                if (d.type === 'is_child_of') return 0.3;
                if (d.type === 'points_to') return 0.2;
                // Very flexible default - links can stretch/compress freely
                return 0.1;
            }))
        .force("charge", d3.forceManyBody()
            .strength(-2000)  // Strong repulsion for tiles to spread them out
            .distanceMax(GRAPH_PHYSICS.CHARGE_MAX_DISTANCE))
        .force("center", d3.forceCenter(width / 2, height / 2).strength(0.05))  // Weak centering - just prevent drift
        .force("collision", d3.forceCollide()
            .radius(120)  // Larger buffer around tiles to prevent overlap
            .strength(1));

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
        .call(drag(simulation!));

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
    const tooltip = d3.select("#tooltip");
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
        if (state.currentGraphData) {
            renderGraph(state.currentGraphData);
        }
    });

    // Update positions on tick
    simulation!.on("tick", () => {
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

// Drag behavior
function drag(simulation: ForceSimulation): any {
    function dragstarted(event: DragEvent, d: D3Node): void {
        if (!event.active) simulation.alphaTarget(0.3).restart();
        d.fx = d.x;
        d.fy = d.y;
    }

    function dragged(event: DragEvent, d: D3Node): void {
        d.fx = event.x;
        d.fy = event.y;
    }

    function dragended(event: DragEvent, d: D3Node): void {
        if (!event.active) simulation.alphaTarget(0);
        d.fx = null;
        d.fy = null;
    }

    return d3.drag()
        .on("start", dragstarted)
        .on("drag", dragged)
        .on("end", dragended);
}

export function resetZoom(): void {
    if (!svg || !zoom) return;
    svg.transition()
        .duration(GRAPH_PHYSICS.ANIMATION_DURATION)
        .call(zoom.transform, d3.zoomIdentity);
}

export function centerGraph(): void {
    if (!g || !g.node()) return;

    const bounds = (g.node() as any).getBBox();
    if (bounds.width === 0 || bounds.height === 0) return;

    const container = document.getElementById('graph-container');
    if (!container) return;

    const fullWidth = container.clientWidth;
    const fullHeight = container.clientHeight;
    const width = bounds.width;
    const height = bounds.height;
    const midX = bounds.x + width / 2;
    const midY = bounds.y + height / 2;
    const scale = GRAPH_PHYSICS.CENTER_SCALE / Math.max(width / fullWidth, height / fullHeight);
    const translate = [fullWidth / 2 - scale * midX, fullHeight / 2 - scale * midY];

    if (!svg || !zoom) return;
    svg.transition()
        .duration(GRAPH_PHYSICS.ANIMATION_DURATION)
        .call(zoom.transform, d3.zoomIdentity
            .translate(translate[0], translate[1])
            .scale(scale));
}

// Handle window resize
export function initGraphResize(): void {
    window.addEventListener('resize', function() {
        if (simulation) {
            const container = document.getElementById('graph-container');
            if (!container) return;

            const width = container.clientWidth;
            const height = container.clientHeight;
            simulation.force("center", d3.forceCenter(width / 2, height / 2));
            simulation.alpha(0.3).restart();
        }
    });
}

// Get current transform state
export function getTransform(): Transform | null {
    if (!svg || !svg.node()) return null;
    const transform = d3.zoomTransform(svg.node());
    return {
        x: transform.x,
        y: transform.y,
        k: transform.k
    };
}

// Set transform state
export function setTransform(transform: Transform): void {
    if (!svg || !zoom || !transform) return;
    svg.transition()
        .duration(0) // Instant, no animation
        .call(zoom.transform, d3.zoomIdentity
            .translate(transform.x, transform.y)
            .scale(transform.k));
}

// Virtue #7: Cleanliness - Export cleanup function for when graph is destroyed
export function cleanupGraph(): void {
    if (simulation) {
        simulation.stop();
        simulation = null;
    }
    domCache.clear();
}

// Save current session to localStorage
function saveCurrentSession(): void {
    const transform = getTransform();
    if (transform) {
        state.currentTransform = transform;
    }

    saveSession({
        query: state.currentQuery,
        verbosity: state.currentVerbosity
        // NOTE: Not saving graphData - D3 object references don't serialize properly
        // Query will be re-run on page load instead
    });
}