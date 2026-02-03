/**
 * GridStack-based Glyph Melding Implementation
 *
 * This module implements proximity-based glyph melding using GridStack.js's
 * grid system and widget capabilities. Glyphs start as single-cell widgets
 * that can meld into multi-cell compositions through proximity detection.
 *
 * Key Features:
 * - Fine-grained grid (50x50) for smooth movement
 * - Proximity-based melding with magnetic snapping
 * - Nested widgets for melded compositions
 * - Smooth animations during meld/unmeld
 * - Heat-based visual feedback
 */

import { GridStack, type GridStackWidget, type GridStackOptions, type GridStackNode } from 'gridstack';
import 'gridstack/dist/gridstack.css';
import type { Glyph } from './glyph';
import { log, SEG } from '../../logger';
import { AX, SO } from '@generated/sym.js';

// Grid configuration
const GRID_COLUMNS = 50;
const GRID_CELL_HEIGHT = 10;
const GRID_CELL_WIDTH = 10;

// Melding thresholds (in grid cells)
const PROXIMITY_THRESHOLD = 15;  // Start showing visual feedback
const MELD_THRESHOLD = 4;        // Actually meld when this close
const UNMELD_VELOCITY_THRESHOLD = 5; // Velocity needed to unmeld

// Visual feedback colors (heat-based)
const COLOR_MELDED = 'rgb(255, 100, 50)';     // Red-orange when melded
const COLOR_READY = 'rgb(255, 150, 50)';      // Orange when ready to meld
const COLOR_DISTANT = 'rgb(255, 240, 150)';   // Faint yellow when distant

// Type compatibility rules
const MELD_COMPATIBILITY: Record<string, string[]> = {
    [AX]: [SO],  // Ax can meld with Prompt
    // Add more compatibility rules as needed
};

interface MeldableWidget extends GridStackWidget {
    glyphId?: string;
    glyphSymbol?: string;
    isMelded?: boolean;
    meldedWith?: string[];
}

interface GridStackGlyphManager {
    grid: GridStack;
    glyphToWidget: Map<string, MeldableWidget>;
    widgetToGlyph: Map<string, Glyph>;
    activeProximities: Map<string, Set<string>>;
    meldedCompositions: Map<string, string[]>;
}

let manager: GridStackGlyphManager | null = null;

/**
 * Initialize GridStack for the canvas
 */
export function initializeGridStack(canvasElement: HTMLElement): GridStack {
    // Add GridStack-specific styles
    canvasElement.classList.add('grid-stack');

    const options: GridStackOptions = {
        column: GRID_COLUMNS,
        cellHeight: GRID_CELL_HEIGHT,
        minRow: 1,
        float: true,  // Allow free positioning
        animate: true, // Enable smooth animations
        resizable: {
            handles: 'se' // Only bottom-right resize handle
        },
        draggable: {
            handle: '.glyph-title-bar'
        },
        disableOneColumnMode: true,
        staticGrid: false
    };

    const grid = GridStack.init(options, canvasElement);

    // Initialize manager
    manager = {
        grid,
        glyphToWidget: new Map(),
        widgetToGlyph: new Map(),
        activeProximities: new Map(),
        meldedCompositions: new Map()
    };

    // Set up event handlers
    setupGridEventHandlers(grid);

    log.debug(SEG.UI, '[GridStack] Initialized grid with fine granularity', {
        columns: GRID_COLUMNS,
        cellHeight: GRID_CELL_HEIGHT
    });

    return grid;
}

/**
 * Add a glyph to the GridStack grid
 */
export function addGlyphToGrid(glyph: Glyph, element: HTMLElement): void {
    if (!manager) {
        log.error(SEG.UI, '[GridStack] Manager not initialized');
        return;
    }

    // Calculate initial grid position
    const gridX = glyph.gridX ?? Math.floor(Math.random() * 10) + 5;
    const gridY = glyph.gridY ?? Math.floor(Math.random() * 10) + 5;

    // Default size for glyphs (can be adjusted per type)
    let width = 4;
    let height = 4;

    if (glyph.symbol === AX) {
        width = 8;
        height = 6;
    } else if (glyph.symbol === SO) {
        width = 10;
        height = 8;
    }

    const widget: MeldableWidget = {
        x: gridX,
        y: gridY,
        w: width,
        h: height,
        content: element.outerHTML,
        glyphId: glyph.id,
        glyphSymbol: glyph.symbol,
        isMelded: false
    };

    // Add widget to grid
    const addedWidget = manager.grid.addWidget(widget);

    if (addedWidget) {
        // Store mappings
        manager.glyphToWidget.set(glyph.id, widget);
        manager.widgetToGlyph.set(glyph.id, glyph);

        // Add glyph-specific classes
        const widgetElement = addedWidget as HTMLElement;
        widgetElement.classList.add('glyph-widget');
        widgetElement.classList.add(`glyph-${glyph.symbol}`);
        widgetElement.dataset.glyphId = glyph.id;
        widgetElement.dataset.glyphSymbol = glyph.symbol || '';

        log.debug(SEG.UI, `[GridStack] Added glyph ${glyph.id} to grid`, {
            symbol: glyph.symbol,
            position: { x: gridX, y: gridY },
            size: { w: width, h: height }
        });
    }
}

/**
 * Set up GridStack event handlers for melding behavior
 */
function setupGridEventHandlers(grid: GridStack): void {
    // Track drag velocity for unmeld detection
    let lastDragTime = 0;
    let lastDragX = 0;
    let lastDragY = 0;
    let dragVelocity = 0;

    // Handle dragging for proximity detection
    grid.on('drag', (event: Event, el: GridStackWidget) => {
        const widget = el as MeldableWidget;
        if (!widget.glyphId || !manager) return;

        const currentTime = Date.now();
        const node = (el as any).gridstackNode as GridStackNode;

        // Calculate velocity
        if (lastDragTime > 0) {
            const timeDelta = currentTime - lastDragTime;
            const distX = node.x - lastDragX;
            const distY = node.y - lastDragY;
            const distance = Math.sqrt(distX * distX + distY * distY);
            dragVelocity = distance / timeDelta * 1000; // cells per second
        }

        lastDragTime = currentTime;
        lastDragX = node.x;
        lastDragY = node.y;

        // Check for unmeld if dragging a melded composition
        if (widget.isMelded && dragVelocity > UNMELD_VELOCITY_THRESHOLD) {
            unmeldComposition(widget.glyphId);
            return;
        }

        // Find nearby widgets for proximity effects
        const nearby = findNearbyWidgets(widget, PROXIMITY_THRESHOLD);

        // Update visual feedback
        updateProximityFeedback(widget, nearby);

        // Apply magnetic snapping if close enough
        const closest = findClosestCompatible(widget, nearby);
        if (closest) {
            applyMagneticForce(widget, closest.widget, closest.distance);
        }
    });

    // Handle drag stop for melding
    grid.on('dragstop', (event: Event, el: GridStackWidget) => {
        const widget = el as MeldableWidget;
        if (!widget.glyphId || !manager) return;

        // Reset drag tracking
        lastDragTime = 0;
        dragVelocity = 0;

        // Find widgets within meld threshold
        const nearby = findNearbyWidgets(widget, MELD_THRESHOLD);

        // Meld with compatible widgets
        for (const target of nearby) {
            if (shouldMeld(widget, target)) {
                meldWidgets(widget, target);
                break; // Only meld with one widget at a time
            }
        }

        // Clear proximity feedback
        clearProximityFeedback(widget);
    });

    // Handle resize events
    grid.on('resizestop', (event: Event, el: GridStackWidget) => {
        const widget = el as MeldableWidget;
        if (!widget.glyphId || !manager) return;

        const glyph = manager.widgetToGlyph.get(widget.glyphId);
        if (glyph) {
            const node = (el as any).gridstackNode as GridStackNode;
            glyph.width = node.w * GRID_CELL_WIDTH;
            glyph.height = node.h * GRID_CELL_HEIGHT;

            log.debug(SEG.UI, `[GridStack] Resized glyph ${glyph.id}`, {
                newSize: { w: node.w, h: node.h }
            });
        }
    });
}

/**
 * Find widgets within a certain distance
 */
function findNearbyWidgets(widget: MeldableWidget, threshold: number): MeldableWidget[] {
    if (!manager) return [];

    const nearby: MeldableWidget[] = [];
    const node = (widget as any).gridstackNode as GridStackNode;
    if (!node) return [];

    const widgets = manager.grid.getGridItems();

    for (const el of widgets) {
        const other = el as MeldableWidget;
        if (other.glyphId === widget.glyphId) continue;

        const otherNode = (other as any).gridstackNode as GridStackNode;
        if (!otherNode) continue;

        // Calculate distance between widget edges
        const distance = calculateEdgeDistance(node, otherNode);

        if (distance <= threshold) {
            nearby.push(other);
        }
    }

    return nearby;
}

/**
 * Calculate minimum distance between widget edges
 */
function calculateEdgeDistance(node1: GridStackNode, node2: GridStackNode): number {
    // Calculate bounds
    const left1 = node1.x;
    const right1 = node1.x + node1.w;
    const top1 = node1.y;
    const bottom1 = node1.y + node1.h;

    const left2 = node2.x;
    const right2 = node2.x + node2.w;
    const top2 = node2.y;
    const bottom2 = node2.y + node2.h;

    // Calculate horizontal and vertical distances
    let dx = 0;
    if (right1 < left2) dx = left2 - right1;
    else if (right2 < left1) dx = left1 - right2;

    let dy = 0;
    if (bottom1 < top2) dy = top2 - bottom1;
    else if (bottom2 < top1) dy = top1 - bottom2;

    return Math.sqrt(dx * dx + dy * dy);
}

/**
 * Check if two widgets should meld
 */
function shouldMeld(widget1: MeldableWidget, widget2: MeldableWidget): boolean {
    if (!widget1.glyphSymbol || !widget2.glyphSymbol) return false;

    // Check type compatibility
    const compatible1 = MELD_COMPATIBILITY[widget1.glyphSymbol];
    const compatible2 = MELD_COMPATIBILITY[widget2.glyphSymbol];

    const canMeld = (compatible1?.includes(widget2.glyphSymbol)) ||
                     (compatible2?.includes(widget1.glyphSymbol));

    if (!canMeld) {
        log.debug(SEG.UI, '[GridStack] Incompatible types for melding', {
            type1: widget1.glyphSymbol,
            type2: widget2.glyphSymbol
        });
        return false;
    }

    // Check if already melded
    if (widget1.isMelded || widget2.isMelded) {
        return false;
    }

    return true;
}

/**
 * Meld two widgets into a composition
 */
function meldWidgets(widget1: MeldableWidget, widget2: MeldableWidget): void {
    if (!manager) return;

    const node1 = (widget1 as any).gridstackNode as GridStackNode;
    const node2 = (widget2 as any).gridstackNode as GridStackNode;

    // Calculate composition bounds
    const x = Math.min(node1.x, node2.x);
    const y = Math.min(node1.y, node2.y);
    const w = Math.max(node1.x + node1.w, node2.x + node2.w) - x;
    const h = Math.max(node1.y + node1.h, node2.y + node2.h) - y;

    // Create composition widget
    const compositionId = `meld-${widget1.glyphId}-${widget2.glyphId}`;
    const composition: MeldableWidget = {
        x,
        y,
        w,
        h,
        content: createMeldedContent(widget1, widget2),
        glyphId: compositionId,
        glyphSymbol: 'melded',
        isMelded: true,
        meldedWith: [widget1.glyphId!, widget2.glyphId!]
    };

    // Remove original widgets
    manager.grid.removeWidget(widget1 as HTMLElement, false);
    manager.grid.removeWidget(widget2 as HTMLElement, false);

    // Add composition widget
    const added = manager.grid.addWidget(composition);
    if (added) {
        const compositionElement = added as HTMLElement;
        compositionElement.classList.add('melded-composition');
        compositionElement.dataset.glyphId = compositionId;

        // Store composition info
        manager.meldedCompositions.set(compositionId, [widget1.glyphId!, widget2.glyphId!]);

        // Apply melded visual style
        applyMeldedStyle(compositionElement);

        log.debug(SEG.UI, '[GridStack] Melded widgets into composition', {
            widget1: widget1.glyphId,
            widget2: widget2.glyphId,
            compositionId,
            bounds: { x, y, w, h }
        });
    }
}

/**
 * Create content for melded composition
 */
function createMeldedContent(widget1: MeldableWidget, widget2: MeldableWidget): string {
    return `
        <div class="melded-container">
            <div class="melded-glyph melded-left" data-original-id="${widget1.glyphId}">
                ${widget1.content}
            </div>
            <div class="melded-glyph melded-right" data-original-id="${widget2.glyphId}">
                ${widget2.content}
            </div>
        </div>
    `;
}

/**
 * Unmeld a composition back into individual widgets
 */
function unmeldComposition(compositionId: string): void {
    if (!manager) return;

    const originalIds = manager.meldedCompositions.get(compositionId);
    if (!originalIds) return;

    // Find composition widget
    const widgets = manager.grid.getGridItems();
    const compositionWidget = widgets.find(w =>
        (w as MeldableWidget).glyphId === compositionId
    ) as MeldableWidget;

    if (!compositionWidget) return;

    const node = (compositionWidget as any).gridstackNode as GridStackNode;

    // Remove composition
    manager.grid.removeWidget(compositionWidget as HTMLElement, false);

    // Restore original widgets with slight offset
    let offsetX = 0;
    for (const originalId of originalIds) {
        const glyph = manager.widgetToGlyph.get(originalId);
        if (glyph) {
            const widget = manager.glyphToWidget.get(originalId);
            if (widget) {
                widget.x = node.x + offsetX;
                widget.y = node.y;
                widget.isMelded = false;

                manager.grid.addWidget(widget);
                offsetX += widget.w + 1; // Add spacing between unmelded widgets
            }
        }
    }

    // Clean up composition data
    manager.meldedCompositions.delete(compositionId);

    log.debug(SEG.UI, '[GridStack] Unmelded composition', {
        compositionId,
        restoredWidgets: originalIds
    });
}

/**
 * Find closest compatible widget for magnetic snapping
 */
function findClosestCompatible(widget: MeldableWidget, nearby: MeldableWidget[]):
    { widget: MeldableWidget; distance: number } | null {

    let closest: { widget: MeldableWidget; distance: number } | null = null;
    let minDistance = Infinity;

    for (const other of nearby) {
        if (shouldMeld(widget, other)) {
            const node1 = (widget as any).gridstackNode as GridStackNode;
            const node2 = (other as any).gridstackNode as GridStackNode;
            const distance = calculateEdgeDistance(node1, node2);

            if (distance < minDistance) {
                minDistance = distance;
                closest = { widget: other, distance };
            }
        }
    }

    return closest;
}

/**
 * Apply magnetic force to snap widgets together
 */
function applyMagneticForce(widget: MeldableWidget, target: MeldableWidget, distance: number): void {
    if (!manager || distance > MELD_THRESHOLD * 2) return;

    const node1 = (widget as any).gridstackNode as GridStackNode;
    const node2 = (target as any).gridstackNode as GridStackNode;

    // Calculate which edges are closest
    const left1 = node1.x;
    const right1 = node1.x + node1.w;
    const left2 = node2.x;
    const right2 = node2.x + node2.w;

    // Snap horizontally if edges are close
    const snapThreshold = 2; // cells

    if (Math.abs(right1 - left2) < snapThreshold) {
        // Snap widget1's right edge to widget2's left edge
        manager.grid.update(widget as HTMLElement, { x: left2 - node1.w });
    } else if (Math.abs(left1 - right2) < snapThreshold) {
        // Snap widget1's left edge to widget2's right edge
        manager.grid.update(widget as HTMLElement, { x: right2 });
    }

    // Align vertically if horizontally aligned
    const verticalAlignThreshold = 3;
    if (Math.abs(node1.y - node2.y) < verticalAlignThreshold) {
        manager.grid.update(widget as HTMLElement, { y: node2.y });
    }
}

/**
 * Update visual feedback based on proximity
 */
function updateProximityFeedback(widget: MeldableWidget, nearby: MeldableWidget[]): void {
    const element = document.querySelector(`[data-glyph-id="${widget.glyphId}"]`) as HTMLElement;
    if (!element) return;

    // Find closest compatible widget
    const closest = findClosestCompatible(widget, nearby);

    if (!closest) {
        element.style.boxShadow = '';
        return;
    }

    // Calculate glow intensity based on distance
    const intensity = 1 - (closest.distance / PROXIMITY_THRESHOLD);
    const clampedIntensity = Math.max(0, Math.min(1, intensity));

    // Apply heat-based color
    let color: string;
    if (closest.distance <= MELD_THRESHOLD) {
        color = COLOR_MELDED;
    } else if (closest.distance <= MELD_THRESHOLD * 2) {
        color = COLOR_READY;
    } else {
        color = COLOR_DISTANT;
    }

    // Apply glow effect with intensity
    const blur = 20 * clampedIntensity;
    const spread = 10 * clampedIntensity;
    element.style.boxShadow = `0 0 ${blur}px ${spread}px ${color}`;
    element.style.transition = 'box-shadow 0.3s ease';
}

/**
 * Clear proximity feedback
 */
function clearProximityFeedback(widget: MeldableWidget): void {
    const element = document.querySelector(`[data-glyph-id="${widget.glyphId}"]`) as HTMLElement;
    if (element) {
        element.style.boxShadow = '';
    }
}

/**
 * Apply visual style to melded composition
 */
function applyMeldedStyle(element: HTMLElement): void {
    element.style.background = 'linear-gradient(90deg, rgba(255,100,50,0.1) 0%, rgba(255,150,50,0.1) 100%)';
    element.style.border = `2px solid ${COLOR_MELDED}`;
    element.style.borderRadius = '8px';
    element.style.boxShadow = `0 0 20px 5px ${COLOR_MELDED}`;
    element.style.transition = 'all 0.3s ease';
}

/**
 * Clean up GridStack when no longer needed
 */
export function destroyGridStack(): void {
    if (manager) {
        manager.grid.destroy(false);
        manager = null;
        log.debug(SEG.UI, '[GridStack] Grid destroyed');
    }
}