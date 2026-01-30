// Graph transform operations
// Zoom, pan, and centering for D3.js graph
// Virtue #11: Animation Purpose - All transitions communicate navigation state, never decorative

import { GRAPH_PHYSICS } from '../config.ts';
import { getSvg, getG, getZoom } from './state.ts';
import type { Transform } from '../../types/core';

// Import D3 from vendor bundle
declare const d3: any;

export function resetZoom(): void {
    const svg = getSvg();
    const zoom = getZoom();
    if (!svg || !zoom) return;
    svg.transition()
        .duration(GRAPH_PHYSICS.ANIMATION_DURATION)
        .call(zoom.transform, d3.zoomIdentity);
}

export function centerGraph(): void {
    const g = getG();
    if (!g || !g.node()) return;

    const bounds = (g.node() as any).getBBox();
    if (bounds.width === 0 || bounds.height === 0) return;

    const container = document.getElementById('graph-viewer');
    if (!container) return;

    const fullWidth = container.clientWidth;
    const fullHeight = container.clientHeight;
    const width = bounds.width;
    const height = bounds.height;
    const midX = bounds.x + width / 2;
    const midY = bounds.y + height / 2;
    const scale = GRAPH_PHYSICS.CENTER_SCALE / Math.max(width / fullWidth, height / fullHeight);
    const translate = [fullWidth / 2 - scale * midX, fullHeight / 2 - scale * midY];

    const svg = getSvg();
    const zoom = getZoom();
    if (!svg || !zoom) return;
    svg.transition()
        .duration(GRAPH_PHYSICS.ANIMATION_DURATION)
        .call(zoom.transform, d3.zoomIdentity
            .translate(translate[0], translate[1])
            .scale(scale));
}

// Get current transform state
export function getTransform(): Transform | null {
    const svg = getSvg();
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
    const svg = getSvg();
    const zoom = getZoom();
    if (!svg || !zoom || !transform) return;
    svg.transition()
        .duration(0) // Instant, no animation
        .call(zoom.transform, d3.zoomIdentity
            .translate(transform.x, transform.y)
            .scale(transform.k));
}
