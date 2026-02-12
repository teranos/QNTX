/**
 * Meldability - Port-aware glyph melding rules
 *
 * Melding is QNTX's spatial composition model where glyphs physically fuse
 * through proximity rather than connecting via wires.
 *
 * Each glyph has directional ports that define valid connections:
 * - right: horizontal data flow (ax → py → prompt, py → py)
 * - left: incoming horizontal flow (for canvas in-port)
 * - bottom: result/output attachment (py ↓ result, prompt ↓ result)
 * - top: upward connections (for canvas top-port)
 */

export type EdgeDirection = 'right' | 'left' | 'bottom' | 'top';

export interface PortRule {
    direction: EdgeDirection;
    targets: readonly string[];
}

/**
 * Port-aware meldability rules: maps glyph classes to their output ports
 * Each port specifies a direction and which target classes can connect there
 */
export const MELDABILITY: Record<string, readonly PortRule[]> = {
    'canvas-ax-glyph': [
        { direction: 'right', targets: ['canvas-prompt-glyph', 'canvas-py-glyph'] }
    ],
    'canvas-py-glyph': [
        { direction: 'right', targets: ['canvas-prompt-glyph', 'canvas-py-glyph'] },
        { direction: 'bottom', targets: ['canvas-result-glyph'] }
    ],
    'canvas-prompt-glyph': [
        { direction: 'bottom', targets: ['canvas-result-glyph'] }
    ],
    'canvas-note-glyph': [
        { direction: 'bottom', targets: ['canvas-prompt-glyph'] }
    ],
    'canvas-subcanvas-glyph': [
        { direction: 'right', targets: ['canvas-py-glyph', 'canvas-ts-glyph', 'canvas-prompt-glyph', 'canvas-ax-glyph', 'canvas-subcanvas-glyph'] },
        { direction: 'left', targets: ['canvas-py-glyph', 'canvas-ts-glyph', 'canvas-subcanvas-glyph'] },
        { direction: 'bottom', targets: ['canvas-py-glyph', 'canvas-ts-glyph', 'canvas-prompt-glyph', 'canvas-result-glyph', 'canvas-subcanvas-glyph'] },
        { direction: 'top', targets: ['canvas-py-glyph', 'canvas-ts-glyph', 'canvas-ax-glyph', 'canvas-subcanvas-glyph'] },
    ]
} as const;

// Cached class lists — derived from static MELDABILITY registry
const _initiatorClasses: string[] = Object.keys(MELDABILITY);
const _targetClasses: string[] = (() => {
    const targets = new Set<string>();
    for (const ports of Object.values(MELDABILITY)) {
        for (const port of ports) {
            for (const target of port.targets) {
                targets.add(target);
            }
        }
    }
    return [...targets];
})();

/**
 * Get all classes that can initiate melding
 */
export function getInitiatorClasses(): readonly string[] {
    return _initiatorClasses;
}

/**
 * Get all classes that can receive meld (across all directions)
 */
export function getTargetClasses(): readonly string[] {
    return _targetClasses;
}

/**
 * Get compatible target classes for a given initiator (across all directions)
 */
export function getCompatibleTargets(initiatorClass: string): string[] {
    const ports = MELDABILITY[initiatorClass];
    if (!ports) return [];
    const targets = new Set<string>();
    for (const port of ports) {
        for (const target of port.targets) {
            targets.add(target);
        }
    }
    return [...targets];
}

/**
 * Check if two glyph classes are compatible for melding
 * Returns the edge direction if compatible, null if not
 */
export function areClassesCompatible(initiatorClass: string, targetClass: string): EdgeDirection | null {
    const ports = MELDABILITY[initiatorClass];
    if (!ports) return null;
    for (const port of ports) {
        if (port.targets.includes(targetClass)) {
            return port.direction;
        }
    }
    return null;
}

/**
 * Extract glyph IDs from a composition element's children
 */
export function getCompositionGlyphIds(composition: HTMLElement): string[] {
    const glyphElements = composition.querySelectorAll('[data-glyph-id]');
    const glyphIds: string[] = [];

    glyphElements.forEach(el => {
        const id = el.getAttribute('data-glyph-id');
        if (id) glyphIds.push(id);
    });

    return glyphIds;
}

const GLYPH_CLASS_RE = /^canvas-\w+-glyph$/;

/**
 * Extract the canonical glyph class (e.g. 'canvas-py-glyph') from an element's classList
 */
export function getGlyphClass(element: HTMLElement): string | null {
    for (const cls of element.classList) {
        if (GLYPH_CLASS_RE.test(cls)) return cls;
    }
    return null;
}

/**
 * Find leaf nodes — glyphs with no outgoing edges (graph sinks)
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
 * Find root nodes — glyphs with no incoming edges (graph sources)
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
 * Check if a glyph's port is free (no existing edge occupies it)
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

export interface MeldOption {
    /** The glyph in the composition that the incoming glyph connects with */
    glyphId: string;
    /** Edge direction for this connection */
    direction: EdgeDirection;
    /** Whether the incoming glyph is the 'from' (prepend) or 'to' (append) in the edge */
    incomingRole: 'from' | 'to';
}

/**
 * Get all possible ways an incoming glyph could meld with a composition.
 *
 * Checks every glyph in the composition for a free port:
 * 1. Append (incoming is 'to'): glyph's outgoing port in the compatible direction must be unoccupied
 * 2. Prepend (incoming is 'from'): glyph's incoming port in the compatible direction must be unoccupied
 *
 * Axiom: each side of a glyph accepts at most one connection.
 */
export function getMeldOptions(
    incomingClass: string,
    compositionElement: HTMLElement,
    edges: Array<{ from: string; to: string; direction: string }>
): MeldOption[] {
    const options: MeldOption[] = [];

    // Collect all glyph IDs from edges
    const allIds = new Set<string>();
    for (const edge of edges) {
        allIds.add(edge.from);
        allIds.add(edge.to);
    }

    // Build port occupancy: which directions are taken for each glyph
    const outgoing = new Map<string, Set<string>>();
    const incoming = new Map<string, Set<string>>();
    for (const edge of edges) {
        if (!outgoing.has(edge.from)) outgoing.set(edge.from, new Set());
        outgoing.get(edge.from)!.add(edge.direction);
        if (!incoming.has(edge.to)) incoming.set(edge.to, new Set());
        incoming.get(edge.to)!.add(edge.direction);
    }

    for (const glyphId of allIds) {
        const el = compositionElement.querySelector(`[data-glyph-id="${glyphId}"]`) as HTMLElement | null;
        if (!el) continue;
        const cls = getGlyphClass(el);
        if (!cls) continue;

        // 1. Append: this glyph sends to the incoming glyph (outgoing port)
        const appendDir = areClassesCompatible(cls, incomingClass);
        if (appendDir && !outgoing.get(glyphId)?.has(appendDir)) {
            options.push({ glyphId, direction: appendDir, incomingRole: 'to' });
        }

        // 2. Prepend: the incoming glyph sends to this glyph (incoming port)
        const prependDir = areClassesCompatible(incomingClass, cls);
        if (prependDir && !incoming.get(glyphId)?.has(prependDir)) {
            options.push({ glyphId, direction: prependDir, incomingRole: 'from' });
        }
    }

    return options;
}

/**
 * Compute grid row/col for each glyph in an edge DAG.
 * BFS from roots: 'right' → same row, next col; 'bottom' → next row, same col.
 * Single source of truth for composition spatial layout.
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

    // BFS from roots
    const queue: string[] = [];
    for (let i = 0; i < roots.length; i++) {
        positions.set(roots[i], { row: 1, col: 1 + i });
        queue.push(roots[i]);
    }

    while (queue.length > 0) {
        const current = queue.shift()!;
        const pos = positions.get(current)!;
        const neighbors = adjacency.get(current);
        if (!neighbors) continue;

        // Track offset for multiple children in the same direction (e.g., two results below py)
        const dirOffset = new Map<string, number>();

        for (const { to, direction } of neighbors) {
            if (positions.has(to)) continue; // first assignment wins

            const offset = dirOffset.get(direction) || 0;

            if (direction === 'right') {
                positions.set(to, { row: pos.row, col: pos.col + 1 + offset });
            } else if (direction === 'left') {
                positions.set(to, { row: pos.row, col: pos.col - 1 - offset });
            } else if (direction === 'bottom') {
                positions.set(to, { row: pos.row + 1 + offset, col: pos.col });
            } else if (direction === 'top') {
                positions.set(to, { row: pos.row - 1 - offset, col: pos.col });
            }

            dirOffset.set(direction, offset + 1);
            queue.push(to);
        }
    }

    return positions;
}
