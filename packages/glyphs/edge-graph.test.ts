/**
 * buildFocusGraph tests — pure edge-walking, no DOM.
 *
 * Each test defines edges as data and asserts the returned FocusGraph
 * is correct: thread, focusIndex, left/right siblings, sibling threads.
 */

import { describe, test, expect } from 'bun:test'
import { buildFocusGraph, visibleThreadMember } from './edge-graph'
import type { CompositionEdge } from './composition'

function edge(from: string, to: string, direction: 'right' | 'bottom', position = 0): CompositionEdge {
    return { from, to, direction, position }
}

describe('buildFocusGraph', () => {
    test('single glyph, no edges', () => {
        const graph = buildFocusGraph([], 'X')
        expect(graph.thread).toEqual(['X'])
        expect(graph.focusIndex).toBe(0)
        expect(graph.leftSiblings.size).toBe(0)
        expect(graph.rightSiblings.size).toBe(0)
        expect(graph.siblingThreads.size).toBe(0)
    })

    test('linear horizontal chain — focus middle', () => {
        // A →right→ B →right→ C
        const edges = [edge('A', 'B', 'right'), edge('B', 'C', 'right')]
        const graph = buildFocusGraph(edges, 'B')

        // B has no bottom edges, thread is just [B]
        expect(graph.thread).toEqual(['B'])
        expect(graph.focusIndex).toBe(0)

        // A is to the left of B, C is to the right
        expect(graph.leftSiblings.get('B')).toEqual(['A'])
        expect(graph.rightSiblings.get('B')).toEqual(['C'])
    })

    test('linear horizontal chain — focus left end', () => {
        // A →right→ B →right→ C
        const edges = [edge('A', 'B', 'right'), edge('B', 'C', 'right')]
        const graph = buildFocusGraph(edges, 'A')

        expect(graph.thread).toEqual(['A'])
        expect(graph.focusIndex).toBe(0)
        expect(graph.leftSiblings.get('A')).toBeUndefined()
        expect(graph.rightSiblings.get('A')).toEqual(['B', 'C'])
    })

    test('linear horizontal chain — focus right end', () => {
        // A →right→ B →right→ C
        const edges = [edge('A', 'B', 'right'), edge('B', 'C', 'right')]
        const graph = buildFocusGraph(edges, 'C')

        expect(graph.thread).toEqual(['C'])
        expect(graph.focusIndex).toBe(0)
        expect(graph.leftSiblings.get('C')).toEqual(['B', 'A'])
        expect(graph.rightSiblings.get('C')).toBeUndefined()
    })

    test('linear vertical chain — focus middle', () => {
        // A →bottom→ B →bottom→ C
        const edges = [edge('A', 'B', 'bottom'), edge('B', 'C', 'bottom')]
        const graph = buildFocusGraph(edges, 'B')

        expect(graph.thread).toEqual(['A', 'B', 'C'])
        expect(graph.focusIndex).toBe(1)
        expect(graph.leftSiblings.size).toBe(0)
        expect(graph.rightSiblings.size).toBe(0)
    })

    test('linear vertical chain — focus root', () => {
        const edges = [edge('A', 'B', 'bottom'), edge('B', 'C', 'bottom')]
        const graph = buildFocusGraph(edges, 'A')

        expect(graph.thread).toEqual(['A', 'B', 'C'])
        expect(graph.focusIndex).toBe(0)
    })

    test('linear vertical chain — focus leaf', () => {
        const edges = [edge('A', 'B', 'bottom'), edge('B', 'C', 'bottom')]
        const graph = buildFocusGraph(edges, 'C')

        expect(graph.thread).toEqual(['A', 'B', 'C'])
        expect(graph.focusIndex).toBe(2)
    })

    test('mixed DAG — focus glyph with both vertical and horizontal neighbors', () => {
        // A →right→ B →right→ C
        //           B →bottom→ D →bottom→ E
        //                      D →right→ F
        const edges = [
            edge('A', 'B', 'right'),
            edge('B', 'C', 'right'),
            edge('B', 'D', 'bottom'),
            edge('D', 'E', 'bottom'),
            edge('D', 'F', 'right'),
        ]
        const graph = buildFocusGraph(edges, 'B')

        // Thread: B's vertical chain is B → D → E
        expect(graph.thread).toEqual(['B', 'D', 'E'])
        expect(graph.focusIndex).toBe(0)

        // B: A left, C right
        expect(graph.leftSiblings.get('B')).toEqual(['A'])
        expect(graph.rightSiblings.get('B')).toEqual(['C'])

        // D: F right (no left neighbor)
        expect(graph.leftSiblings.get('D')).toBeUndefined()
        expect(graph.rightSiblings.get('D')).toEqual(['F'])

        // E: no horizontal neighbors
        expect(graph.leftSiblings.get('E')).toBeUndefined()
        expect(graph.rightSiblings.get('E')).toBeUndefined()
    })

    test('mixed DAG — focus a leaf in the vertical chain', () => {
        const edges = [
            edge('A', 'B', 'right'),
            edge('B', 'C', 'right'),
            edge('B', 'D', 'bottom'),
            edge('D', 'E', 'bottom'),
            edge('D', 'F', 'right'),
        ]
        const graph = buildFocusGraph(edges, 'E')

        // Same thread, different focusIndex
        expect(graph.thread).toEqual(['B', 'D', 'E'])
        expect(graph.focusIndex).toBe(2)

        // Siblings still come from each thread member
        expect(graph.leftSiblings.get('B')).toEqual(['A'])
        expect(graph.rightSiblings.get('B')).toEqual(['C'])
        expect(graph.rightSiblings.get('D')).toEqual(['F'])
    })

    test('mixed DAG — focus a horizontal sibling', () => {
        // A →right→ B →right→ C
        //           B →bottom→ D
        const edges = [
            edge('A', 'B', 'right'),
            edge('B', 'C', 'right'),
            edge('B', 'D', 'bottom'),
        ]
        const graph = buildFocusGraph(edges, 'C')

        // C has no bottom edges — thread is [C]
        expect(graph.thread).toEqual(['C'])
        expect(graph.focusIndex).toBe(0)

        // C's left neighbors: B (closest), then A
        expect(graph.leftSiblings.get('C')).toEqual(['B', 'A'])

        // B has a vertical thread — should appear in siblingThreads
        expect(graph.siblingThreads.get('B')).toEqual(['B', 'D'])
    })

    test('sibling threads are walked for horizontal neighbors', () => {
        // A →right→ B →right→ C
        //           B →bottom→ D →bottom→ E
        const edges = [
            edge('A', 'B', 'right'),
            edge('B', 'C', 'right'),
            edge('B', 'D', 'bottom'),
            edge('D', 'E', 'bottom'),
        ]
        const graph = buildFocusGraph(edges, 'A')

        // A's thread is just [A]
        expect(graph.thread).toEqual(['A'])
        expect(graph.focusIndex).toBe(0)

        // A's right neighbor: B, then C
        expect(graph.rightSiblings.get('A')).toEqual(['B', 'C'])

        // B has a vertical thread
        expect(graph.siblingThreads.get('B')).toEqual(['B', 'D', 'E'])
    })

    test('horizontal neighbors are ordered by distance — closest first', () => {
        // A →right→ B →right→ C →right→ D →right→ E
        const edges = [
            edge('A', 'B', 'right'),
            edge('B', 'C', 'right'),
            edge('C', 'D', 'right'),
            edge('D', 'E', 'right'),
        ]
        const graph = buildFocusGraph(edges, 'C')

        // Left: B first (closer), then A
        expect(graph.leftSiblings.get('C')).toEqual(['B', 'A'])
        // Right: D first (closer), then E
        expect(graph.rightSiblings.get('C')).toEqual(['D', 'E'])
    })

    test('sibling thread starts from the sibling, not its root', () => {
        // B →bottom→ R1 →bottom→ R2
        // R1 →right→ L1
        // Focus L1: left sibling is R1. R1's sibling thread should be [R1, R2], NOT [B, R1, R2].
        const edges = [
            edge('B', 'R1', 'bottom'),
            edge('R1', 'R2', 'bottom'),
            edge('R1', 'L1', 'right'),
        ]
        const graph = buildFocusGraph(edges, 'L1')

        expect(graph.thread).toEqual(['L1'])
        expect(graph.leftSiblings.get('L1')).toEqual(['R1'])
        expect(graph.siblingThreads.get('R1')).toEqual(['R1', 'R2'])
    })

    test('demo DAG: each thread member has its own right siblings', () => {
        // Full demo DAG
        const edges = [
            edge('A', 'B', 'right'),
            edge('B', 'C', 'right'),
            edge('C', 'D', 'right'),
            edge('D', 'E', 'right'),
            edge('B', 'R1', 'bottom'),
            edge('R1', 'R2', 'bottom'),
            edge('R2', 'R3', 'bottom'),
            edge('R1', 'L1', 'right'),
            edge('L1', 'L2', 'right'),
        ]
        const graph = buildFocusGraph(edges, 'B')

        expect(graph.thread).toEqual(['B', 'R1', 'R2', 'R3'])
        expect(graph.focusIndex).toBe(0)

        // B's right sibling is C (ts-glyph)
        expect(graph.rightSiblings.get('B')![0]).toBe('C')

        // R1's right sibling is L1 (log-1) — different thread member, different neighbors
        expect(graph.rightSiblings.get('R1')![0]).toBe('L1')

        // Pivoting right uses the FOCUSED glyph's siblings.
        // If focusedGlyphId is R1, pivot right → L1 (log-1).
        // If focusedGlyphId is B, pivot right → C (ts-glyph).
        // Scrolling to see B at the top does NOT change focusedGlyphId.
    })

    test('glyph not in edges returns single-glyph thread', () => {
        const edges = [edge('A', 'B', 'right')]
        const graph = buildFocusGraph(edges, 'Z')

        expect(graph.thread).toEqual(['Z'])
        expect(graph.focusIndex).toBe(0)
        expect(graph.leftSiblings.size).toBe(0)
        expect(graph.rightSiblings.size).toBe(0)
    })
})

describe('visibleThreadMember', () => {
    // Thread members have positions (top, height). The viewport center
    // determines which member is "visible" for horizontal pivot purposes.
    // viewportCenterY is in thread-local coordinates (0 = thread top).

    test('returns first member when viewport is at the top', () => {
        // Thread: [A(h=100), B(h=100), C(h=100)], viewport center at y=50 (middle of A)
        const heights = [100, 100, 100]
        const gap = 6
        expect(visibleThreadMember(['A', 'B', 'C'], heights, gap, 50)).toBe('A')
    })

    test('returns second member when scrolled to it', () => {
        // A occupies 0..100, gap 6, B occupies 106..206
        // viewport center at 156 (middle of B)
        const heights = [100, 100, 100]
        const gap = 6
        expect(visibleThreadMember(['A', 'B', 'C'], heights, gap, 156)).toBe('B')
    })

    test('returns last member when scrolled to bottom', () => {
        const heights = [100, 100, 100]
        const gap = 6
        // C starts at 212, center at 262
        expect(visibleThreadMember(['A', 'B', 'C'], heights, gap, 262)).toBe('C')
    })

    test('returns closest member when between two', () => {
        // A center=50, B center=156. viewport at 90 — clearly closer to A
        const heights = [100, 100, 100]
        const gap = 6
        expect(visibleThreadMember(['A', 'B', 'C'], heights, gap, 90)).toBe('A')
    })

    test('single member always returned', () => {
        expect(visibleThreadMember(['X'], [200], 6, 50)).toBe('X')
    })
})
