/**
 * Meldability - Port-aware glyph melding rules
 *
 * Melding is QNTX's spatial composition model where glyphs physically fuse
 * through proximity rather than connecting via wires.
 *
 * Each glyph has directional ports that define valid connections:
 * - right: horizontal data flow (ax → py → prompt, py → py)
 * - bottom: result/output attachment (py ↓ result, prompt ↓ result)
 * - top: (reserved for future upward connections)
 */

export type EdgeDirection = 'right' | 'bottom' | 'top'; // 'top' reserved for future upward connections

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
    ]
} as const;

/**
 * Get all classes that can initiate melding
 */
export function getInitiatorClasses(): string[] {
    return Object.keys(MELDABILITY);
}

/**
 * Get all classes that can receive meld (across all directions)
 */
export function getTargetClasses(): string[] {
    const targets = new Set<string>();
    for (const ports of Object.values(MELDABILITY)) {
        for (const port of ports) {
            for (const target of port.targets) {
                targets.add(target);
            }
        }
    }
    return [...targets];
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
 * Get the glyph classes at the boundary of an element (standalone glyph or composition).
 *
 * For a standalone glyph: returns its own class.
 * For a composition: returns the leaf or root glyph classes by querying the DOM.
 */
export function getElementBoundaryClasses(
    element: HTMLElement,
    boundary: 'leaves' | 'roots',
    edges?: Array<{ from: string; to: string; direction: string }>
): string[] {
    // Standalone glyph — just return its own class
    if (!element.classList.contains('melded-composition')) {
        const cls = getGlyphClass(element);
        return cls ? [cls] : [];
    }

    // Composition — need edges to find boundary nodes
    if (!edges) return [];

    const boundaryIds = boundary === 'leaves'
        ? getLeafGlyphIds(edges)
        : getRootGlyphIds(edges);

    const classes: string[] = [];
    for (const id of boundaryIds) {
        const el = element.querySelector(`[data-glyph-id="${id}"]`) as HTMLElement | null;
        if (!el) continue;
        const cls = getGlyphClass(el);
        if (cls) classes.push(cls);
    }
    return classes;
}

/**
 * Check if two elements (standalone glyphs or compositions) have compatible boundaries.
 *
 * Checks initiator's leaf classes against target's root classes.
 * Returns the first matching edge direction, or null if incompatible.
 */
export function getCompositionCompatibility(
    initiator: HTMLElement,
    target: HTMLElement,
    initiatorEdges?: Array<{ from: string; to: string; direction: string }>,
    targetEdges?: Array<{ from: string; to: string; direction: string }>
): EdgeDirection | null {
    const leafClasses = getElementBoundaryClasses(initiator, 'leaves', initiatorEdges);
    const rootClasses = getElementBoundaryClasses(target, 'roots', targetEdges);

    for (const leafClass of leafClasses) {
        for (const rootClass of rootClasses) {
            const direction = areClassesCompatible(leafClass, rootClass);
            if (direction) return direction;
        }
    }
    return null;
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
 * Checks two things:
 * 1. Leaf nodes: can any leaf's output port accept the incoming glyph? (append, incoming is 'to')
 * 2. Root nodes: can the incoming glyph's output port connect to any root? (prepend, incoming is 'from')
 */
export function getMeldOptions(
    incomingClass: string,
    compositionElement: HTMLElement,
    edges: Array<{ from: string; to: string; direction: string }>
): MeldOption[] {
    const options: MeldOption[] = [];

    // 1. Check leaf nodes — incoming glyph appends as target
    const leafIds = getLeafGlyphIds(edges);
    for (const leafId of leafIds) {
        const leafElement = compositionElement.querySelector(`[data-glyph-id="${leafId}"]`) as HTMLElement | null;
        if (!leafElement) continue;

        const leafClass = getGlyphClass(leafElement);
        if (!leafClass) continue;

        const direction = areClassesCompatible(leafClass, incomingClass);
        if (direction) {
            options.push({ glyphId: leafId, direction, incomingRole: 'to' });
        }
    }

    // 2. Check root nodes — incoming glyph prepends as source
    const rootIds = getRootGlyphIds(edges);
    for (const rootId of rootIds) {
        const rootElement = compositionElement.querySelector(`[data-glyph-id="${rootId}"]`) as HTMLElement | null;
        if (!rootElement) continue;

        const rootClass = getGlyphClass(rootElement);
        if (!rootClass) continue;

        const direction = areClassesCompatible(incomingClass, rootClass);
        if (direction) {
            options.push({ glyphId: rootId, direction, incomingRole: 'from' });
        }
    }

    return options;
}
