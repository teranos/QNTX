/**
 * Graph Physics Configuration
 *
 * Handles D3 force simulation physics parameters for different link types.
 * Physics metadata comes from backend via GraphData.meta.relationship_types.
 * Domain types (git, music, etc.) define their own physics via the type system.
 */

import { GRAPH_PHYSICS } from './config.ts';
import type { D3Link } from '../types/d3-graph';
import type { RelationshipTypeInfo } from '../types/core';

/**
 * Calculate link distance for D3 force simulation based on link type.
 *
 * Queries relationship type metadata from backend for physics configuration.
 * Domain types define their own physics values via the attestation type system.
 *
 * Fallback strategy:
 * 1. Use metadata.link_distance if available (backend type system)
 * 2. Fall back to hardcoded git constants (backward compatibility)
 * 3. Fall back to default distance (unknown types)
 *
 * @param link - D3 link with type property
 * @param metadata - Relationship type metadata from backend (optional)
 * @returns Distance value for force simulation
 */
export function getLinkDistance(link: D3Link, metadata?: RelationshipTypeInfo[]): number {
    // Phase 2: Query metadata from backend type system
    if (metadata) {
        const typeInfo = metadata.find(info => info.type === link.type);
        if (typeInfo?.link_distance !== undefined && typeInfo.link_distance !== null) {
            return typeInfo.link_distance;
        }
    }

    // Backward compatibility: Hardcoded git values
    // TODO(issue #7 - Phase 3): Remove after all domains use type system
    if (link.type === 'is_child_of') return GRAPH_PHYSICS.GIT_CHILD_LINK_DISTANCE;
    if (link.type === 'points_to') return GRAPH_PHYSICS.GIT_BRANCH_LINK_DISTANCE;

    // Default for unknown types
    return GRAPH_PHYSICS.LINK_DISTANCE;
}

/**
 * Calculate link strength for D3 force simulation based on link type.
 *
 * Queries relationship type metadata from backend for physics configuration.
 * Domain types define their own physics values via the attestation type system.
 *
 * Fallback strategy:
 * 1. Use metadata.link_strength if available (backend type system)
 * 2. Fall back to hardcoded git constants (backward compatibility)
 * 3. Fall back to default strength (unknown types)
 *
 * @param link - D3 link with type property
 * @param metadata - Relationship type metadata from backend (optional)
 * @returns Strength value for force simulation
 */
export function getLinkStrength(link: D3Link, metadata?: RelationshipTypeInfo[]): number {
    // Phase 2: Query metadata from backend type system
    if (metadata) {
        const typeInfo = metadata.find(info => info.type === link.type);
        if (typeInfo?.link_strength !== undefined && typeInfo.link_strength !== null) {
            return typeInfo.link_strength;
        }
    }

    // Backward compatibility: Hardcoded git values
    // TODO(issue #7 - Phase 3): Remove after all domains use type system
    if (link.type === 'is_child_of') return GRAPH_PHYSICS.GIT_CHILD_LINK_STRENGTH;
    if (link.type === 'points_to') return GRAPH_PHYSICS.GIT_BRANCH_LINK_STRENGTH;

    // Default for unknown types
    return GRAPH_PHYSICS.DEFAULT_LINK_STRENGTH;
}
