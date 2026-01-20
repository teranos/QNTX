// D3.js graph visualization
// Main rendering orchestration - delegates to specialized modules

import { appState, GRAPH_PHYSICS } from '../config.ts';
import { uiState } from '../ui-state.ts';
import { hiddenNodeTypes, initTypeAttestations } from '../components/type-attestations.ts';
import { getLinkDistance, getLinkStrength } from './physics.ts';
import {
    getSimulation, getDomCache,
    setSimulation, setSvg, setG, setZoom, clearState
} from './state.ts';
import { normalizeNodeType, filterVisibleNodes } from './utils.ts';
import { createDragBehavior } from './interactions.ts';
import { getTransform, centerGraph } from './transform.ts';
import { initFocusKeyboardSupport, cleanupFocusKeyboardSupport } from './focus.ts';
import { createZoomBehavior, setupSVGClickHandler } from './zoom.ts';
import { createLinks, createLinkLabels, updateLinkPositions, updateLinkLabelPositions } from './links.ts';
import { createDiagnosticPattern, createTileRect, createTileText, updateTilePositions } from './tile/rendering.ts';
import { setupTileClickHandler, setupTileHoverHandlers, setupTileContextMenu } from './tile/interactions.ts';
import type { GraphData, Node } from '../../types/core';
import type {
    D3Node,
    D3Link
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
    initTypeAttestations(renderGraph, data);

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
            nodeColors[typeInfo.type] = typeInfo.color ?? '#666666';
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
    createDiagnosticPattern(defs);

    // Create zoom behavior with unfocus detection
    const zoom = createZoomBehavior(saveCurrentSession);
    setZoom(zoom);
    svg.call(zoom);

    // Setup click handler on empty SVG space to unfocus
    setupSVGClickHandler(svg);

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

    // Create links and link labels
    const link = createLinks(g, d3Links);
    const linkLabel = createLinkLabels(g, d3Links);

    // Create tile nodes
    const node = g.append("g")
        .selectAll("g")
        .data(d3Nodes)
        .join("g")
        .attr("class", "node")
        .call(createDragBehavior(simulation));

    // Render tiles (rectangles and text)
    createTileRect(node, nodeColors);
    createTileText(node);

    // Setup tile interaction handlers
    setupTileClickHandler(node);
    setupTileHoverHandlers(node);
    setupTileContextMenu(node, renderGraph);

    // Update positions on tick
    simulation.on("tick", () => {
        updateLinkPositions(link);
        updateLinkLabelPositions(linkLabel);
        updateTilePositions(node);
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
