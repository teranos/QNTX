/**
 * Edge graph — pure DAG traversal and layout for composition edges.
 *
 * All functions are pure: edges in, data out. No DOM, no QNTX deps.
 * Used by both the real meld system and standalone demos.
 */

import type { CompositionEdge } from './composition';
import type { FocusGraph } from './manifestations/focus';

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

// ---------------------------------------------------------------------------
// Focus graph from edges
// ---------------------------------------------------------------------------

/**
 * Build a FocusGraph for a glyph from composition edges.
 *
 * Walks the DAG to find:
 * - The vertical thread (bottom edges) containing the focused glyph
 * - Horizontal neighbors (right edges) for each thread member, ordered by distance
 * - Each sibling's own vertical thread
 *
 * Pure function: edges in, FocusGraph out. No DOM.
 */
export function buildFocusGraph(
    edges: CompositionEdge[],
    glyphId: string,
): FocusGraph {
    const empty: FocusGraph = {
        thread: [glyphId],
        focusIndex: 0,
        leftSiblings: new Map(),
        rightSiblings: new Map(),
        siblingThreads: new Map(),
    };

    if (edges.length === 0) return empty;

    // Build adjacency maps for bottom and right edges
    const bottomOut = new Map<string, string>(); // from → to (bottom)
    const bottomIn = new Map<string, string>();  // to → from (bottom)
    const rightOut = new Map<string, string>();   // from → to (right)
    const rightIn = new Map<string, string>();    // to → from (right)

    for (const e of edges) {
        if (e.direction === 'bottom') {
            bottomOut.set(e.from, e.to);
            bottomIn.set(e.to, e.from);
        } else if (e.direction === 'right') {
            rightOut.set(e.from, e.to);
            rightIn.set(e.to, e.from);
        }
    }

    // Check if this glyph participates in any edge
    const allIds = new Set<string>();
    for (const e of edges) {
        allIds.add(e.from);
        allIds.add(e.to);
    }
    if (!allIds.has(glyphId)) return empty;

    // Walk the vertical chain containing this glyph.
    // First, find the root of the glyph's bottom-edge chain.
    let threadRoot = glyphId;
    while (bottomIn.has(threadRoot)) {
        threadRoot = bottomIn.get(threadRoot)!;
    }

    // Walk down from root to build the thread
    const thread: string[] = [threadRoot];
    let current = threadRoot;
    while (bottomOut.has(current)) {
        current = bottomOut.get(current)!;
        thread.push(current);
    }

    const focusIndex = thread.indexOf(glyphId);

    // If the glyph isn't in a bottom chain, it's a standalone node in a horizontal chain.
    // Thread is just [glyphId].
    if (focusIndex === -1) {
        return {
            thread: [glyphId],
            focusIndex: 0,
            leftSiblings: buildHorizontalNeighbors([glyphId], rightOut, rightIn),
            rightSiblings: buildHorizontalNeighbors([glyphId], rightOut, rightIn, true),
            siblingThreads: buildSiblingThreads([glyphId], rightOut, rightIn, bottomOut),
        };
    }

    const leftSiblings = buildHorizontalNeighbors(thread, rightOut, rightIn);
    const rightSiblings = buildHorizontalNeighbors(thread, rightOut, rightIn, true);

    // Collect all sibling IDs to build their threads
    const allSiblingIds = new Set<string>();
    for (const sibs of leftSiblings.values()) for (const s of sibs) allSiblingIds.add(s);
    for (const sibs of rightSiblings.values()) for (const s of sibs) allSiblingIds.add(s);

    const siblingThreads = new Map<string, string[]>();
    for (const sibId of allSiblingIds) {
        const sibThread = walkBottomChain(sibId, bottomOut, bottomIn);
        if (sibThread.length > 1) {
            siblingThreads.set(sibId, sibThread);
        }
    }

    return { thread, focusIndex, leftSiblings, rightSiblings, siblingThreads };
}

/**
 * For each thread member, walk horizontal edges outward to collect neighbors.
 * If right=true, walks rightOut (forward). If right=false, walks rightIn (backward = left).
 * Returns Map<threadMemberId, orderedNeighborIds> — closest first.
 */
function buildHorizontalNeighbors(
    thread: string[],
    rightOut: Map<string, string>,
    rightIn: Map<string, string>,
    right = false,
): Map<string, string[]> {
    const result = new Map<string, string[]>();
    const forward = right ? rightOut : rightIn;

    for (const memberId of thread) {
        const neighbors: string[] = [];
        let current = memberId;
        while (forward.has(current)) {
            current = forward.get(current)!;
            neighbors.push(current);
        }
        if (neighbors.length > 0) {
            result.set(memberId, neighbors);
        }
    }
    return result;
}

/**
 * Build sibling threads for all horizontal neighbors of thread members.
 */
function buildSiblingThreads(
    thread: string[],
    rightOut: Map<string, string>,
    rightIn: Map<string, string>,
    bottomOut: Map<string, string>,
): Map<string, string[]> {
    const allSiblingIds = new Set<string>();
    const bottomIn = new Map<string, string>();
    // Rebuild bottomIn from bottomOut
    for (const [from, to] of bottomOut) {
        bottomIn.set(to, from);
    }

    // Collect all horizontal neighbors
    for (const memberId of thread) {
        let cur = memberId;
        while (rightOut.has(cur)) { cur = rightOut.get(cur)!; allSiblingIds.add(cur); }
        cur = memberId;
        while (rightIn.has(cur)) { cur = rightIn.get(cur)!; allSiblingIds.add(cur); }
    }

    const result = new Map<string, string[]>();
    for (const sibId of allSiblingIds) {
        const sibThread = walkBottomChain(sibId, bottomOut, bottomIn);
        if (sibThread.length > 1) {
            result.set(sibId, sibThread);
        }
    }
    return result;
}

/**
 * Given a thread, member heights, and the viewport center Y in thread-local
 * coordinates, return which thread member is currently visible (centered).
 * Used by horizontal pivot to pick the right sibling set.
 */
export function visibleThreadMember(
    thread: string[],
    heights: number[],
    gap: number,
    viewportCenterY: number,
): string {
    if (thread.length === 1) return thread[0];

    let y = 0;
    for (let i = 0; i < thread.length; i++) {
        const memberCenter = y + heights[i] / 2;
        const nextY = y + heights[i] + gap;
        // If viewport center is before the midpoint between this member and next, pick this one
        if (i === thread.length - 1 || viewportCenterY < (memberCenter + nextY + heights[i + 1] / 2) / 2) {
            return thread[i];
        }
        y = nextY;
    }
    return thread[thread.length - 1];
}

/** Walk the bottom chain downward from a glyph (glyph to leaf). */
function walkBottomChain(
    glyphId: string,
    bottomOut: Map<string, string>,
    _bottomIn: Map<string, string>,
): string[] {
    const chain: string[] = [glyphId];
    let current = glyphId;
    while (bottomOut.has(current)) {
        current = bottomOut.get(current)!;
        chain.push(current);
    }
    return chain;
}
