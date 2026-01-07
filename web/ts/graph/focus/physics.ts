// Physics adjustments for focus mode

import { GRAPH_PHYSICS } from '../../config.ts';
import { getSimulation, getDomCache } from '../state.ts';
import type { D3Node } from '../../../types/d3-graph';

// Import D3 from vendor bundle
declare const d3: any;

/**
 * Adjust simulation physics for focus mode
 * - Pin the focused tile in place with a position force
 * - Reduce charge strength to bring other tiles closer
 * - Reduce collision radius to allow tighter packing
 */
export function adjustPhysicsForFocus(focusedNode: D3Node): void {
    const simulation = getSimulation();
    if (!simulation) return;

    const container = getDomCache().get('graphContainer', '#graph-container');
    if (!container) return;

    const viewportWidth = container.clientWidth;
    const viewportHeight = container.clientHeight;

    console.log('[physics] adjusting for focus', {
        focusedNodeId: focusedNode.id,
        positionStrength: GRAPH_PHYSICS.FOCUS_POSITION_STRENGTH,
        chargeStrength: GRAPH_PHYSICS.FOCUS_CHARGE_STRENGTH,
        collisionRadius: GRAPH_PHYSICS.FOCUS_COLLISION_RADIUS,
        simulationAlpha: 0.3
    });

    // Add a position force to pin the focused tile at its current position
    // This prevents physics from moving the focused tile while other tiles can still move
    simulation.force('focus-position', d3.forceX()
        .x((d: D3Node) => d.x!)  // Pin each node to its current x
        .strength((d: D3Node) => d.id === focusedNode.id ? GRAPH_PHYSICS.FOCUS_POSITION_STRENGTH : 0));

    simulation.force('focus-position-y', d3.forceY()
        .y((d: D3Node) => d.y!)  // Pin each node to its current y
        .strength((d: D3Node) => d.id === focusedNode.id ? GRAPH_PHYSICS.FOCUS_POSITION_STRENGTH : 0));

    // Reduce charge strength to bring other tiles closer
    simulation.force('charge', d3.forceManyBody()
        .strength(GRAPH_PHYSICS.FOCUS_CHARGE_STRENGTH)
        .distanceMax(GRAPH_PHYSICS.CHARGE_MAX_DISTANCE));

    // Reduce collision radius to allow tighter packing
    simulation.force('collision', d3.forceCollide()
        .radius(GRAPH_PHYSICS.FOCUS_COLLISION_RADIUS)
        .strength(1));

    // Reheat the simulation to apply changes
    simulation.alpha(0.3).restart();
}

/**
 * Restore normal simulation physics after unfocus
 */
export function restoreNormalPhysics(): void {
    const simulation = getSimulation();
    if (!simulation) return;

    console.log('[physics] restoring normal physics', {
        chargeStrength: GRAPH_PHYSICS.TILE_CHARGE_STRENGTH,
        collisionRadius: GRAPH_PHYSICS.COLLISION_RADIUS,
        simulationAlpha: 0.2
    });

    // Remove focus-specific position forces
    simulation.force('focus-position', null);
    simulation.force('focus-position-y', null);

    // Restore normal charge strength
    simulation.force('charge', d3.forceManyBody()
        .strength(GRAPH_PHYSICS.TILE_CHARGE_STRENGTH)
        .distanceMax(GRAPH_PHYSICS.CHARGE_MAX_DISTANCE));

    // Restore normal collision radius
    simulation.force('collision', d3.forceCollide()
        .radius(GRAPH_PHYSICS.COLLISION_RADIUS)
        .strength(1));

    // Gently reheat the simulation
    simulation.alpha(0.2).restart();
}
