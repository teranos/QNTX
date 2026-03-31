/**
 * Composition types and pure helpers.
 *
 * These are the canonical types for glyph compositions — the package owns them.
 * Proto-generated types (from canvas.proto) are wire format, mapped at the
 * API boundary in web/.
 */

/**
 * Edge direction in the composition DAG.
 * 'right' = horizontal data flow, 'bottom' = vertical attachment, 'top' = reserved.
 */
export type EdgeDirection = 'right' | 'bottom' | 'top';

/**
 * A directed edge in the composition DAG.
 * Supports multi-directional melding: horizontal (right), vertical (top/bottom).
 */
export interface CompositionEdge {
    /** source glyph ID */
    from: string;
    /** target glyph ID */
    to: string;
    /** 'right', 'top', 'bottom' */
    direction: string;
    /** ordering for multiple edges in same direction */
    position: number;
}

/**
 * A composition of melded glyphs — a DAG with spatial anchor.
 * Edges define the graph structure, x/y anchor the composition on canvas.
 */
export interface CompositionState {
    id: string;
    edges: CompositionEdge[];
    /** anchor X position in pixels */
    x: number;
    /** anchor Y position in pixels */
    y: number;
    created_at?: string;
    updated_at?: string;
}

/**
 * Build edges from a linear chain of glyph IDs.
 * Creates consecutive edges connecting glyphs in order.
 */
export function buildEdgesFromChain(
    glyphIds: string[],
    direction: EdgeDirection = 'right'
): CompositionEdge[] {
    if (glyphIds.length < 2) {
        return [];
    }

    const edges: CompositionEdge[] = [];
    for (let i = 0; i < glyphIds.length - 1; i++) {
        edges.push({
            from: glyphIds[i],
            to: glyphIds[i + 1],
            direction,
            position: i
        });
    }
    return edges;
}

/**
 * Extract all unique glyph IDs from edges.
 * Returns deduplicated array of glyph IDs.
 */
export function extractGlyphIds(edges: CompositionEdge[]): string[] {
    const ids = new Set<string>();
    for (const edge of edges) {
        ids.add(edge.from);
        ids.add(edge.to);
    }
    return Array.from(ids);
}
