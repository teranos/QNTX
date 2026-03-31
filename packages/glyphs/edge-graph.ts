/**
 * Edge graph — pure DAG traversal and layout for composition edges.
 *
 * All functions are pure: edges in, data out. No DOM, no QNTX deps.
 * Used by both the real meld system and standalone demos.
 */

import type { CompositionEdge } from './composition';

// ---------------------------------------------------------------------------
// Graph queries (EWALK)
// ---------------------------------------------------------------------------

/**
 * Find root nodes — glyphs with no incoming edges (graph sources).
 */
export function getRootGlyphIds(
    edges: Array<{ from: string; to: string; direction: string }>
): string[] {
    const allIds = new Set<string>();
    const toIds = new Set<string>();

    for (const edge of edges) {
        allIds.add(edge.from);
        allIds.add(edge.to);
        toIds.add(edge.to);
    }

    return [...allIds].filter(id => !toIds.has(id));
}

/**
 * Find leaf nodes — glyphs with no outgoing edges (graph sinks).
 */
export function getLeafGlyphIds(
    edges: Array<{ from: string; to: string; direction: string }>
): string[] {
    const allIds = new Set<string>();
    const fromIds = new Set<string>();

    for (const edge of edges) {
        allIds.add(edge.from);
        allIds.add(edge.to);
        fromIds.add(edge.from);
    }

    return [...allIds].filter(id => !fromIds.has(id));
}

/**
 * Check if a glyph's port is free (no existing edge occupies it).
 */
export function isPortFree(
    glyphId: string,
    direction: string,
    role: 'incoming' | 'outgoing',
    edges: Array<{ from: string; to: string; direction: string }>
): boolean {
    for (const edge of edges) {
        if (role === 'outgoing' && edge.from === glyphId && edge.direction === direction) return false;
        if (role === 'incoming' && edge.to === glyphId && edge.direction === direction) return false;
    }
    return true;
}

/**
 * Check if edge set forms a connected graph (treating edges as undirected).
 * Used to decide between partial detach and full unmeld.
 */
export function isConnectedGraph(edges: CompositionEdge[]): boolean {
    const ids = new Set<string>();
    const adjacency = new Map<string, Set<string>>();
    for (const edge of edges) {
        ids.add(edge.from);
        ids.add(edge.to);
        if (!adjacency.has(edge.from)) adjacency.set(edge.from, new Set());
        if (!adjacency.has(edge.to)) adjacency.set(edge.to, new Set());
        adjacency.get(edge.from)!.add(edge.to);
        adjacency.get(edge.to)!.add(edge.from);
    }
    if (ids.size === 0) return false;

    const start = ids.values().next().value!;
    const visited = new Set<string>([start]);
    const queue = [start];
    while (queue.length > 0) {
        const current = queue.shift()!;
        for (const neighbor of adjacency.get(current) || []) {
            if (!visited.has(neighbor)) {
                visited.add(neighbor);
                queue.push(neighbor);
            }
        }
    }
    return visited.size === ids.size;
}

// ---------------------------------------------------------------------------
// Grid layout from edges (GRDLP)
// ---------------------------------------------------------------------------

/**
 * Compute grid row/col for each glyph in an edge DAG.
 * BFS from roots: 'right' → same row, next col; 'bottom' → next row, same col.
 * Single source of truth for composition spatial layout.
 *
 * Roots are processed sequentially, deepest chains first. Lateral roots
 * (those whose targets are already positioned by a longer chain) derive
 * their position from the connection rather than starting at row 1.
 */
export function computeGridPositions(
    edges: Array<{ from: string; to: string; direction: string }>
): Map<string, { row: number; col: number }> {
    const positions = new Map<string, { row: number; col: number }>();

    if (edges.length === 0) return positions;

    const roots = getRootGlyphIds(edges);

    // Build adjacency list
    const adjacency = new Map<string, Array<{ to: string; direction: string }>>();
    for (const edge of edges) {
        if (!adjacency.has(edge.from)) adjacency.set(edge.from, []);
        adjacency.get(edge.from)!.push({ to: edge.to, direction: edge.direction });
    }

    // Sort roots: deepest chains first so they establish positions before lateral roots
    function countReachable(id: string): number {
        let count = 0;
        const visited = new Set<string>();
        const q = [id];
        while (q.length > 0) {
            const n = q.shift()!;
            for (const { to } of adjacency.get(n) || []) {
                if (!visited.has(to)) {
                    visited.add(to);
                    count++;
                    q.push(to);
                }
            }
        }
        return count;
    }
    roots.sort((a, b) => countReachable(b) - countReachable(a));

    // Process each root sequentially: place + full BFS before next root
    let nextRootCol = 1;
    for (const root of roots) {
        if (positions.has(root)) continue;

        // Lateral root: target already positioned → derive position from connection
        let derived = false;
        const rootNeighbors = adjacency.get(root);
        if (rootNeighbors) {
            for (const { to, direction } of rootNeighbors) {
                const targetPos = positions.get(to);
                if (!targetPos) continue;

                let candidate: { row: number; col: number } | null = null;
                if (direction === 'right') {
                    candidate = { row: targetPos.row, col: targetPos.col - 1 };
                } else if (direction === 'bottom') {
                    candidate = { row: targetPos.row - 1, col: targetPos.col };
                } else if (direction === 'top') {
                    candidate = { row: targetPos.row + 1, col: targetPos.col };
                }

                // Only use derived position if it doesn't collide with an existing glyph
                if (candidate) {
                    const occupied = [...positions.values()].some(
                        p => p.row === candidate!.row && p.col === candidate!.col
                    );
                    if (!occupied) {
                        positions.set(root, candidate);
                        derived = true;
                        break;
                    }
                }
            }
        }

        if (!derived) {
            positions.set(root, { row: 1, col: nextRootCol });
            nextRootCol++;
        }

        // BFS from this root
        const queue = [root];
        while (queue.length > 0) {
            const current = queue.shift()!;
            const pos = positions.get(current)!;
            const neighbors = adjacency.get(current);
            if (!neighbors) continue;

            const dirOffset = new Map<string, number>();

            for (const { to, direction } of neighbors) {
                if (positions.has(to)) continue; // first assignment wins

                const offset = dirOffset.get(direction) || 0;

                if (direction === 'right') {
                    positions.set(to, { row: pos.row, col: pos.col + 1 + offset });
                } else if (direction === 'bottom') {
                    positions.set(to, { row: pos.row + 1 + offset, col: pos.col });
                } else if (direction === 'top') {
                    positions.set(to, { row: pos.row - 1 - offset, col: pos.col });
                }

                dirOffset.set(direction, offset + 1);
                queue.push(to);
            }
        }
    }

    // Normalize: shift all positions so minimum row and col are both 1
    let minRow = Infinity, minCol = Infinity;
    for (const pos of positions.values()) {
        minRow = Math.min(minRow, pos.row);
        minCol = Math.min(minCol, pos.col);
    }
    if (minRow < 1 || minCol < 1) {
        const rowShift = 1 - minRow;
        const colShift = 1 - minCol;
        for (const [, pos] of positions) {
            pos.row += rowShift;
            pos.col += colShift;
        }
    }

    return positions;
}
