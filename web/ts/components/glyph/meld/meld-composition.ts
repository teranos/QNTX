/**
 * Meld composition — create, extend, reconstruct, and unmeld glyph compositions.
 *
 * CRITICAL: This implementation respects the core glyph axiom:
 * "A Glyph is exactly ONE DOM element for its entire lifetime"
 *
 * NO cloneNode. NO createElement for existing glyphs.
 * Melding is achieved through reparenting, not cloning.
 *
 * Layout: Absolute positioning derived from the edge DAG via computeGridPositions().
 * Elements are measured and positioned with pixel offsets, avoiding both
 * CSS grid's row-height coupling and flexbox's inability to express row offsets.
 */

import { log, SEG } from '../../../logger';
import type { Glyph } from '../glyph';
import type { CompositionEdge } from '../../../state/ui';
import type { EdgeDirection } from './meldability';
import { computeGridPositions } from './meldability';
import { addComposition, removeComposition, extractGlyphIds, findCompositionByGlyph } from '../../../state/compositions';
import { clearMeldFeedback } from './meld-feedback';
import { getTransform } from '../canvas/canvas-pan';

const UNMELD_OFFSET = 20; // px - spacing between glyphs when unmelding

/**
 * Apply absolute positioning layout to a composition based on its edge graph.
 * Single source of truth — used by performMeld, extendComposition, and reconstructMeld.
 *
 * Computes pixel positions from the DAG grid positions, measuring actual element
 * sizes to accumulate row/column offsets. This avoids both CSS grid's row-height
 * coupling and flexbox's inability to express row offsets.
 */
function applyColumnLayout(
    composition: HTMLElement,
    elements: HTMLElement[],
    edges: CompositionEdge[]
): void {
    const positions = computeGridPositions(edges);

    // Remove existing column wrappers (migration from flexbox), moving children back
    composition.querySelectorAll('.meld-column').forEach(col => {
        while (col.firstChild) {
            composition.appendChild(col.firstChild);
        }
        col.remove();
    });

    // Build a map of element by id for quick lookup
    const elementById = new Map<string, HTMLElement>();
    for (const el of elements) {
        const id = el.getAttribute('data-glyph-id') || '';
        elementById.set(id, el);
    }

    // Group elements by (row, col) for layout computation
    const rows = new Map<number, Map<number, { el: HTMLElement; id: string }>>();
    let maxRow = 0;
    let maxCol = 0;
    for (const el of elements) {
        const id = el.getAttribute('data-glyph-id') || '';
        const pos = positions.get(id);
        if (!pos) {
            log.warn(SEG.GLYPH, `[MeldSystem] No grid position computed for glyph ${id}`);
            continue;
        }
        if (!rows.has(pos.row)) rows.set(pos.row, new Map());
        rows.get(pos.row)!.set(pos.col, { el, id });
        maxRow = Math.max(maxRow, pos.row);
        maxCol = Math.max(maxCol, pos.col);
    }

    // Clear old layout styles and ensure children are direct children of composition
    for (const el of elements) {
        el.style.gridRow = '';
        el.style.gridColumn = '';
        el.style.position = 'absolute';
        if (el.parentElement !== composition) {
            composition.appendChild(el);
        }
    }

    // Force browser to compute layout before measuring:
    // Set a large temporary size so absolutely positioned children aren't constrained,
    // then trigger a synchronous reflow via offsetHeight read.
    composition.style.width = '10000px';
    composition.style.height = '10000px';
    void composition.offsetHeight;

    // Measure element sizes (must be in DOM and laid out to get dimensions)
    // getBoundingClientRect returns screen pixels (scaled by CSS transform),
    // but element left/top are in the content layer's local coordinate space.
    // Divide by scale to get unscaled dimensions.
    const canvasEl = composition.closest('.canvas-workspace');
    const canvasId = canvasEl?.getAttribute('data-canvas-id') || 'canvas-workspace';
    const { scale } = getTransform(canvasId);

    const colWidths = new Map<number, number>();
    const rowHeights = new Map<number, number>();

    for (const [id, el] of elementById) {
        const pos = positions.get(id);
        if (!pos) continue;
        const rect = el.getBoundingClientRect();
        const w = (rect.width / scale) || el.offsetWidth || 200;
        const h = (rect.height / scale) || el.offsetHeight || 150;
        colWidths.set(pos.col, Math.max(colWidths.get(pos.col) || 0, w));
        rowHeights.set(pos.row, Math.max(rowHeights.get(pos.row) || 0, h));
    }

    // Compute pixel offsets: accumulate column widths and row heights
    const colOffsets = new Map<number, number>();
    let xAccum = 0;
    for (let c = 1; c <= maxCol; c++) {
        colOffsets.set(c, xAccum);
        xAccum += (colWidths.get(c) || 0);
    }

    const rowOffsets = new Map<number, number>();
    let yAccum = 0;
    for (let r = 1; r <= maxRow; r++) {
        rowOffsets.set(r, yAccum);
        yAccum += (rowHeights.get(r) || 0);
    }

    // Position each element
    for (const [id, el] of elementById) {
        const pos = positions.get(id);
        if (!pos) continue;
        el.style.left = `${colOffsets.get(pos.col) || 0}px`;
        el.style.top = `${rowOffsets.get(pos.row) || 0}px`;
    }

    // Size the composition to contain all children
    composition.style.width = `${xAccum}px`;
    composition.style.height = `${yAccum}px`;
}

/**
 * Perform meld operation
 * CRITICAL: This reparents the actual DOM elements, does NOT clone them
 *
 * Compositions are persisted to storage and survive page refresh.
 * Supports multi-directional melding: right (horizontal) and bottom (vertical).
 */
export function performMeld(
    initiatorElement: HTMLElement,
    targetElement: HTMLElement,
    initiatorGlyph: Glyph,
    targetGlyph: Glyph,
    direction: EdgeDirection = 'right'
): HTMLElement {
    const canvas = initiatorElement.parentElement;
    if (!canvas) {
        throw new Error(`Cannot meld: no canvas parent for initiator ${initiatorGlyph.id}`);
    }

    log.info(SEG.GLYPH, '[MeldSystem] Performing meld - reparenting elements', { direction });

    // Generate composition ID
    const compositionId = `melded-${initiatorGlyph.id}-${targetGlyph.id}`;

    // Create edge with the actual direction from proximity detection
    const edges: CompositionEdge[] = [{
        from: initiatorGlyph.id,
        to: targetGlyph.id,
        direction,
        position: 0
    }];

    // Create composition container
    const composition = document.createElement('div');
    composition.className = 'melded-composition';
    composition.setAttribute('data-melded', 'true');
    composition.setAttribute('data-glyph-id', compositionId);

    // Position at initiator location
    composition.style.position = 'absolute';
    composition.style.left = initiatorElement.style.left;
    composition.style.top = initiatorElement.style.top;

    // Parse position for storage
    const x = parseInt(initiatorElement.style.left || '0', 10);
    const y = parseInt(initiatorElement.style.top || '0', 10);

    if (isNaN(x) || isNaN(y)) {
        log.warn(SEG.GLYPH, '[MeldSystem] Invalid position during meld', {
            rawLeft: initiatorElement.style.left,
            rawTop: initiatorElement.style.top,
            parsedX: x,
            parsedY: y
        });
    }

    // Clear positioning from glyphs (they're now relative to composition)
    initiatorElement.style.position = 'relative';
    initiatorElement.style.left = '0';
    initiatorElement.style.top = '0';
    targetElement.style.position = 'relative';
    targetElement.style.left = '0';
    targetElement.style.top = '0';

    // Clear meld feedback
    clearMeldFeedback(initiatorElement);
    clearMeldFeedback(targetElement);

    // REPARENT the actual elements (NOT clones!)
    composition.appendChild(initiatorElement);
    composition.appendChild(targetElement);

    // Add to canvas BEFORE layout so elements are in the DOM for measurement
    canvas.appendChild(composition);

    // Apply grid layout from edge graph
    applyColumnLayout(composition, [initiatorElement, targetElement], edges);

    // Persist composition to storage
    addComposition({
        id: compositionId,
        edges,
        x: isNaN(x) ? 0 : x,
        y: isNaN(y) ? 0 : y
    });

    log.info(SEG.GLYPH, '[MeldSystem] Meld complete - elements reparented and persisted', {
        compositionId,
        edges: edges.length,
        glyphs: extractGlyphIds(edges)
    });

    return composition;
}

/**
 * Extend an existing composition by adding a glyph at a leaf (append) or root (prepend)
 *
 * CRITICAL: Reparents the incoming element into the existing composition container.
 * Regenerates composition ID and updates storage.
 */
export function extendComposition(
    compositionElement: HTMLElement,
    incomingElement: HTMLElement,
    incomingGlyphId: string,
    anchorGlyphId: string,
    direction: EdgeDirection,
    incomingRole: 'from' | 'to'
): HTMLElement {
    // Look up existing composition state
    const existingComp = findCompositionByGlyph(anchorGlyphId);
    if (!existingComp) {
        throw new Error(`Cannot extend composition: no composition found for glyph ${anchorGlyphId}`);
    }

    const oldId = existingComp.id;

    // Build new edge based on role
    const newEdge: CompositionEdge = incomingRole === 'to'
        ? { from: anchorGlyphId, to: incomingGlyphId, direction, position: existingComp.edges.length }
        : { from: incomingGlyphId, to: anchorGlyphId, direction, position: existingComp.edges.length };

    // Regenerate composition ID from the new edge
    const newId = `melded-${newEdge.from}-${newEdge.to}`;

    log.info(SEG.GLYPH, '[MeldSystem] Extending composition', {
        oldId,
        newId,
        anchor: anchorGlyphId,
        incoming: incomingGlyphId,
        direction,
        incomingRole
    });

    // Clear positioning and meld feedback on incoming element
    incomingElement.style.position = 'relative';
    incomingElement.style.left = '0';
    incomingElement.style.top = '0';
    clearMeldFeedback(incomingElement);

    // Clear feedback on all existing glyphs in the composition (anchor glyph may still glow)
    compositionElement.querySelectorAll('[data-glyph-id]').forEach(el => {
        (el as HTMLElement).style.boxShadow = '';
        el.classList.remove('meld-ready', 'meld-target');
    });

    // Append incoming as direct child — grid handles placement
    compositionElement.appendChild(incomingElement);

    // Update composition ID on DOM
    compositionElement.setAttribute('data-glyph-id', newId);

    // Update storage: remove old, add new with all edges
    const allEdges = [...existingComp.edges, newEdge];
    removeComposition(oldId);
    addComposition({
        id: newId,
        edges: allEdges,
        x: existingComp.x,
        y: existingComp.y
    });

    // Rebuild grid positions for all children
    const allChildren = Array.from(
        compositionElement.querySelectorAll('[data-glyph-id]')
    ) as HTMLElement[];
    applyColumnLayout(compositionElement, allChildren, allEdges);

    log.info(SEG.GLYPH, '[MeldSystem] Composition extended', {
        newId,
        edges: allEdges.length,
        glyphs: extractGlyphIds(allEdges)
    });

    return compositionElement;
}

/**
 * Reconstruct a melded composition from storage (without persisting)
 * Used when restoring compositions on page load
 */
export function reconstructMeld(
    glyphElements: HTMLElement[],
    edges: CompositionEdge[],
    compositionId: string,
    x: number,
    y: number
): HTMLElement {
    if (glyphElements.length === 0) {
        throw new Error(`Cannot reconstruct meld: no glyph elements provided for composition ${compositionId}`);
    }

    const canvas = glyphElements[0].parentElement;
    if (!canvas) {
        throw new Error(`Cannot reconstruct meld: no canvas parent for composition ${compositionId}`);
    }

    log.info(SEG.GLYPH, '[MeldSystem] Reconstructing meld from storage', {
        glyphCount: glyphElements.length,
        edgeCount: edges.length
    });

    // Create composition container
    const composition = document.createElement('div');
    composition.className = 'melded-composition';
    composition.setAttribute('data-melded', 'true');
    composition.setAttribute('data-glyph-id', compositionId);

    // Position at saved location
    composition.style.position = 'absolute';
    composition.style.left = `${x}px`;
    composition.style.top = `${y}px`;

    // Clear positioning from glyphs and reparent
    glyphElements.forEach(element => {
        element.style.position = 'relative';
        element.style.left = '0';
        element.style.top = '0';
        composition.appendChild(element);
    });

    // Add to canvas BEFORE layout so elements are in the DOM for measurement
    canvas.appendChild(composition);

    // Apply grid layout from edge graph
    applyColumnLayout(composition, glyphElements, edges);

    log.info(SEG.GLYPH, '[MeldSystem] Meld reconstructed', {
        compositionId,
        glyphCount: glyphElements.length
    });

    return composition;
}

/**
 * Check if remaining node IDs form a connected graph when treating edges as undirected.
 * Used by detachGlyph to decide between partial detach and full unmeld.
 */
function isConnectedGraph(edges: CompositionEdge[]): boolean {
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

/**
 * Detach a single glyph from a composition, keeping the rest melded if possible.
 *
 * - If only 2 glyphs: delegates to unmeldComposition (can't have 1-glyph composition)
 * - If removing the glyph disconnects the graph: delegates to unmeldComposition
 * - Otherwise: removes the glyph, updates edges/storage, rebuilds layout
 *
 * Returns the detached element and remaining composition (null if fully unmelded).
 */
export function detachGlyph(glyphId: string, composition: HTMLElement): {
    detachedElement: HTMLElement;
    remainingComposition: HTMLElement | null;
} | null {
    if (!isMeldedComposition(composition)) {
        log.warn(SEG.GLYPH, '[MeldSystem] detachGlyph: not a melded composition');
        return null;
    }

    const canvas = composition.parentElement;
    if (!canvas) {
        log.error(SEG.GLYPH, '[MeldSystem] detachGlyph: composition has no parent canvas');
        return null;
    }

    const compositionId = composition.getAttribute('data-glyph-id') || '';
    const storedComp = findCompositionByGlyph(glyphId);
    if (!storedComp) {
        log.warn(SEG.GLYPH, `[MeldSystem] detachGlyph: no stored composition for glyph ${glyphId}`);
        return null;
    }

    const allGlyphIds = extractGlyphIds(storedComp.edges);

    // 2-glyph composition → full unmeld
    if (allGlyphIds.length <= 2) {
        log.info(SEG.GLYPH, '[MeldSystem] detachGlyph: 2-glyph composition, delegating to full unmeld');
        const result = unmeldComposition(composition);
        if (!result) return null;
        const detached = result.glyphElements.find(
            el => (el.getAttribute('data-glyph-id') || el.dataset.glyphId) === glyphId
        );
        if (!detached) return null;
        return { detachedElement: detached, remainingComposition: null };
    }

    // Filter edges: remove all edges involving this glyph
    const remainingEdges = storedComp.edges.filter(
        e => e.from !== glyphId && e.to !== glyphId
    );

    // Check if remaining edges form a connected graph
    if (!isConnectedGraph(remainingEdges)) {
        log.info(SEG.GLYPH, `[MeldSystem] detachGlyph: removing ${glyphId} disconnects graph, delegating to full unmeld`);
        const result = unmeldComposition(composition);
        if (!result) return null;
        const detached = result.glyphElements.find(
            el => (el.getAttribute('data-glyph-id') || el.dataset.glyphId) === glyphId
        );
        if (!detached) return null;
        return { detachedElement: detached, remainingComposition: null };
    }

    // Partial detach: reparent detached element to canvas
    const detachedEl = composition.querySelector(`[data-glyph-id="${glyphId}"]`) as HTMLElement | null;
    if (!detachedEl) {
        log.error(SEG.GLYPH, `[MeldSystem] detachGlyph: element not found for glyph ${glyphId}`);
        return null;
    }

    // Position the detached element near the composition
    const compLeft = parseInt(composition.style.left || '0', 10) || 0;
    const compTop = parseInt(composition.style.top || '0', 10) || 0;
    detachedEl.style.position = 'absolute';
    detachedEl.style.left = `${compLeft + UNMELD_OFFSET}px`;
    detachedEl.style.top = `${compTop - UNMELD_OFFSET - 40}px`;
    detachedEl.style.gridRow = '';
    detachedEl.style.gridColumn = '';

    // Reparent to canvas
    canvas.insertBefore(detachedEl, composition);

    // Update storage: remove old composition, add new with remaining edges
    const newId = `melded-${remainingEdges[0].from}-${remainingEdges[0].to}`;
    removeComposition(storedComp.id);
    composition.setAttribute('data-glyph-id', newId);
    addComposition({
        id: newId,
        edges: remainingEdges,
        x: storedComp.x,
        y: storedComp.y
    });

    // Rebuild layout for remaining elements
    const remainingElements = Array.from(
        composition.querySelectorAll('[data-glyph-id]')
    ) as HTMLElement[];
    applyColumnLayout(composition, remainingElements, remainingEdges);

    log.info(SEG.GLYPH, `[MeldSystem] Detached glyph ${glyphId} from composition`, {
        oldId: compositionId,
        newId,
        remainingEdges: remainingEdges.length,
        remainingGlyphs: extractGlyphIds(remainingEdges)
    });

    return { detachedElement: detachedEl, remainingComposition: composition };
}

/**
 * Check if element is a melded composition
 */
export function isMeldedComposition(element: HTMLElement): boolean {
    return element.classList.contains('melded-composition');
}

/**
 * Unmeld a composition back to individual glyphs
 * Restores the original elements to canvas and removes from storage
 *
 * Returns the unmelded elements so caller can restore drag handlers.
 */
export function unmeldComposition(composition: HTMLElement): {
    glyphElements: HTMLElement[];
} | null {
    if (!isMeldedComposition(composition)) {
        log.warn(SEG.GLYPH, '[MeldSystem] Not a melded composition');
        return null;
    }

    const canvas = composition.parentElement;
    if (!canvas) {
        log.error(SEG.GLYPH, '[MeldSystem] Composition has no parent canvas');
        return null;
    }

    // Get composition ID for storage removal
    const compositionId = composition.getAttribute('data-glyph-id') || '';

    // Find all child glyphs in composition
    const glyphElements = Array.from(composition.querySelectorAll('[data-glyph-id]')) as HTMLElement[];

    if (glyphElements.length === 0) {
        log.error(SEG.GLYPH, '[MeldSystem] No glyphs found in composition - removing corrupted composition');
        if (compositionId) {
            removeComposition(compositionId);
        }
        composition.remove();
        return null;
    }

    // Restore absolute positioning
    const compLeft = parseInt(composition.style.left || '0', 10);
    const compTop = parseInt(composition.style.top || '0', 10);

    // Validate parsed values - fallback to 0 if NaN
    if (isNaN(compLeft)) {
        log.warn(SEG.GLYPH, `[MeldSystem] Invalid composition.style.left: "${composition.style.left}", using 0`);
    }
    if (isNaN(compTop)) {
        log.warn(SEG.GLYPH, `[MeldSystem] Invalid composition.style.top: "${composition.style.top}", using 0`);
    }
    const left = isNaN(compLeft) ? 0 : compLeft;
    const top = isNaN(compTop) ? 0 : compTop;

    // TODO(#448): binary heuristic — mixed-direction compositions spread into a flat row
    const firstChildId = glyphElements[0].getAttribute('data-glyph-id') || '';
    const storedComp = findCompositionByGlyph(firstChildId);
    const isVertical = storedComp?.edges.some(e => e.direction === 'bottom' || e.direction === 'top')
        && !storedComp?.edges.some(e => e.direction === 'right');

    // TODO(#450): animate the separation instead of instant repositioning
    let currentX = left;
    let currentY = top;
    glyphElements.forEach((element) => {
        element.style.position = 'absolute';
        element.style.left = `${currentX}px`;
        element.style.top = `${currentY}px`;
        element.style.gridRow = '';
        element.style.gridColumn = '';

        // Reparent back to canvas
        canvas.insertBefore(element, composition);

        // Accumulate position for next glyph along the original axis
        const rect = element.getBoundingClientRect();
        if (isVertical) {
            currentY += rect.height + UNMELD_OFFSET;
        } else {
            currentX += rect.width + UNMELD_OFFSET;
        }
    });

    // Remove composition from storage
    if (compositionId) {
        removeComposition(compositionId);
    }

    // Remove composition container
    composition.remove();

    log.info(SEG.GLYPH, '[MeldSystem] Unmeld complete - elements restored and removed from storage', {
        compositionId,
        glyphCount: glyphElements.length
    });

    // Return elements so caller can restore drag handlers
    return {
        glyphElements
    };
}
