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
 * Domain-agnostic: queries relationship type metadata from backend.
 * All domain-specific physics come from attestation type system.
 *
 * @param link - D3 link with type property
 * @param metadata - Relationship type metadata from backend (optional)
 * @returns Distance value for force simulation (metadata or default)
 */
export function getLinkDistance(link: D3Link, metadata?: RelationshipTypeInfo[]): number {
    // Query metadata from backend type system
    if (metadata) {
        const typeInfo = metadata.find(info => info.type === link.type);
        if (typeInfo?.link_distance !== undefined && typeInfo.link_distance !== null) {
            return typeInfo.link_distance;
        }
    }

    // Default for unknown types
    return GRAPH_PHYSICS.LINK_DISTANCE;
}

/**
 * Calculate link strength for D3 force simulation based on link type.
 *
 * Domain-agnostic: queries relationship type metadata from backend.
 * All domain-specific physics come from attestation type system.
 *
 * @param link - D3 link with type property
 * @param metadata - Relationship type metadata from backend (optional)
 * @returns Strength value for force simulation (metadata or default)
 */
export function getLinkStrength(link: D3Link, metadata?: RelationshipTypeInfo[]): number {
    // Query metadata from backend type system
    if (metadata) {
        const typeInfo = metadata.find(info => info.type === link.type);
        if (typeInfo?.link_strength !== undefined && typeInfo.link_strength !== null) {
            return typeInfo.link_strength;
        }
    }

    // Default for unknown types
    return GRAPH_PHYSICS.DEFAULT_LINK_STRENGTH;
}
