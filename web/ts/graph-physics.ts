/**
 * Graph Physics Configuration
 *
 * Handles D3 force simulation physics parameters for different link types.
 *
 * TODO(issue #7 - Phase 2): Replace hardcoded git logic with metadata lookup.
 * After Phase 2, these functions should query relationship type metadata from
 * GraphData.meta.relationship_types instead of hardcoding domain knowledge.
 */

import { GRAPH_PHYSICS } from './config.ts';
import type { D3Link } from '../types/d3-graph';

/**
 * Calculate link distance for D3 force simulation based on link type.
 *
 * TODO(issue #7): This hardcodes git domain knowledge - should query type metadata.
 *
 * Current behavior:
 * - is_child_of (git commits) → shorter distance for tight lineage
 * - points_to (git branches) → medium distance for branch pointers
 * - all others → default distance
 *
 * Phase 2 behavior:
 * - Query GraphData.meta.relationship_types for link_distance
 * - Fall back to default if not specified
 *
 * @param link - D3 link with type property
 * @returns Distance value for force simulation
 */
export function getLinkDistance(link: D3Link): number {
    if (link.type === 'is_child_of') return GRAPH_PHYSICS.GIT_CHILD_LINK_DISTANCE;
    if (link.type === 'points_to') return GRAPH_PHYSICS.GIT_BRANCH_LINK_DISTANCE;
    return GRAPH_PHYSICS.LINK_DISTANCE;
}

/**
 * Calculate link strength for D3 force simulation based on link type.
 *
 * TODO(issue #7): This hardcodes git domain knowledge - should query type metadata.
 *
 * Current behavior:
 * - is_child_of (git commits) → weaker strength for flexible lineage
 * - points_to (git branches) → weaker strength for branch pointers
 * - all others → default strength
 *
 * Phase 2 behavior:
 * - Query GraphData.meta.relationship_types for link_strength
 * - Fall back to default if not specified
 *
 * @param link - D3 link with type property
 * @returns Strength value for force simulation
 */
export function getLinkStrength(link: D3Link): number {
    if (link.type === 'is_child_of') return GRAPH_PHYSICS.GIT_CHILD_LINK_STRENGTH;
    if (link.type === 'points_to') return GRAPH_PHYSICS.GIT_BRANCH_LINK_STRENGTH;
    return GRAPH_PHYSICS.DEFAULT_LINK_STRENGTH;
}
