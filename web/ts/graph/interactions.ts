// Graph interaction handlers
// Drag behavior and resize handling for D3.js graph

import { getSimulation } from './state.ts';
import type { D3Node, ForceSimulation, DragEvent } from '../../types/d3-graph';

// Import D3 from vendor bundle
declare const d3: any;

// Create drag behavior for nodes
export function createDragBehavior(simulation: ForceSimulation): any {
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

// Handle window resize
export function initGraphResize(): void {
    window.addEventListener('resize', function() {
        const simulation = getSimulation();
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
