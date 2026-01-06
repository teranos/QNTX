// Graph utility functions
// Helper functions for graph data processing

import type { Node } from '../../types/core';

// Normalize node type for comparison (DRY)
export function normalizeNodeType(type: string | null | undefined): string {
    return (type || '').trim().toLowerCase();
}

// Phase 2: Backend controls visibility - frontend just filters based on backend's decision
// Backend sets node.visible and link.hidden based on client preferences
export function filterVisibleNodes(nodes: Node[]): Node[] {
    return nodes.filter(node => node.visible !== false);
}
