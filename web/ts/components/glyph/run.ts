/**
 * Glyph Run - The universal container for glyphs
 *
 * Design: Glyphs are visual entities that morph between three states:
 * 1. Collapsed (8px square) - minimal visual footprint
 * 2. Proximity expanded (220px) - reveals title text on hover
 * 3. Window state - full application window with content
 *
 * The same DOM element transforms through all states via animation.
 *
 * AXIOM: A Glyph is exactly ONE DOM element for its entire lifetime.
 *
 * FORBIDDEN OPERATIONS (will throw errors):
 * - cloneNode on a Glyph element
 * - document.createElement to represent an existing Glyph
 * - Re-rendering a Glyph via renderItems, add, remove, or diffing logic
 * - Having two elements with the same data-glyph-id
 * - "Fading out" one element while "fading in" another
 * - Recreating a Glyph to "simplify animation"
 *
 * ALLOWED OPERATIONS:
 * - Reparenting the same DOM element (body ↔ indicator container)
 * - Changing position, transform, top/left, width/height
 * - Changing border-radius, background, opacity
 * - Temporarily detaching a Glyph from layout flow
 * - Delaying content mount until after morph completion
 *
 * All Glyph DOM elements MUST be created through createGlyphElement factory.
 */

import { log, SEG } from '../../logger';
import { stripHtml } from '../../html-utils';
import { uiState } from '../../state/ui';
import { GlyphProximity } from './proximity';
import { type Glyph, getMaximizeDuration } from './glyph';
import { isInWindowState, setGlyphId } from './dataset';
import { morphToWindow } from './manifestations/window';
import { morphToCanvas } from './manifestations/canvas';

// Re-export Glyph interface for external use
export type { Glyph } from './glyph';


class GlyphRunImpl {
    // Track all created glyph elements to enforce single-element axiom
    private glyphElements: Map<string, HTMLElement> = new Map();

    // Track click handlers separately for proper cleanup (prevents memory leaks)
    private glyphClickHandlers: WeakMap<HTMLElement, (e: MouseEvent) => void> = new WeakMap();

    // Proximity morphing handler
    private proximity: GlyphProximity = new GlyphProximity();

    /**
     * SINGLE FACTORY for creating glyph DOM elements
     * This is the ONLY place that calls document.createElement for glyphs
     *
     * CRITICAL: This is not a UX preference.
     * This is a structural invariant required for future attestations and reasoning.
     *
     * The persistent DOM identity enables:
     * - Attestations about glyph state and transitions
     * - Reasoning about glyph relationships and dependencies
     * - Tracking provenance and lifecycle events
     * - Maintaining coherence between frontend and backend models
     *
     * The glyph's DOM element IS its identity, not a representation of it.
     */
    private createGlyphElement(item: Glyph): HTMLElement {
        // Check if element already exists - THIS SHOULD NEVER HAPPEN
        if (this.glyphElements.has(item.id)) {
            throw new Error(`AXIOM VIOLATION: Attempted to create duplicate glyph element for ${item.id}`);
        }

        const existing = document.querySelector(`[data-glyph-id="${item.id}"]`);
        if (existing) {
            throw new Error(`AXIOM VIOLATION: Glyph element ${item.id} already exists in DOM`);
        }

        // CREATE THE ELEMENT - ONCE AND ONLY ONCE
        const glyph = document.createElement('div');
        glyph.className = 'glyph-run-glyph';
        setGlyphId(glyph, item.id);

        // Track this element
        this.glyphElements.set(item.id, glyph);

        // Attach click handler that will persist with the element forever
        const clickHandler = (e: MouseEvent) => {
            e.stopPropagation();
            log.debug(SEG.GLYPH, `[Glyph ${item.id}] Click detected, windowState:`, isInWindowState(glyph));

            // Only morph if in collapsed state (not already a window)
            if (!isInWindowState(glyph)) {
                this.isRestoring = true;

                // Route to correct manifestation based on type
                const manifestationType = item.manifestationType || 'window';
                if (manifestationType === 'fullscreen' || manifestationType === 'canvas') {
                    morphToCanvas(
                        glyph,
                        item,
                        (id, element) => this.verifyElementTracking(id, element),
                        (element, g) => this.reattachGlyphToIndicator(element, g)
                    );
                } else {
                    morphToWindow(
                        glyph,
                        item,
                        (id, element) => this.verifyElementTracking(id, element),
                        (id) => this.remove(id),
                        (element, g) => this.reattachGlyphToIndicator(element, g)
                    );
                }

                // Re-enable proximity morphing after animation
                setTimeout(() => {
                    this.isRestoring = false;
                }, getMaximizeDuration());
            }
        };

        // Store handler in WeakMap for proper cleanup
        this.glyphClickHandlers.set(glyph, clickHandler);
        glyph.addEventListener('click', clickHandler);

        return glyph;
    }
    // Deferred items to add after init
    private deferredItems: Glyph[] = [];
    private readonly MAX_DEFERRED_ITEMS = 100; // Prevent unbounded growth
    private deferredItemsTimeout: ReturnType<typeof setTimeout> | null = null;

    // Component state
    private element: HTMLElement | null = null;
    private indicatorContainer: HTMLElement | null = null;
    private items: Map<string, Glyph> = new Map();
    private isRestoring: boolean = false; // Disable proximity morphing during restore

    /**
     * Initialize the tray and attach to DOM
     * Call this once when the app starts
     */
    public init(): void {
        if (this.element) return; // Already initialized

        const graphContainer = document.getElementById('graph-container');
        if (!graphContainer) {
            log.warn(SEG.GLYPH, 'GlyphRun: #graph-container not found, deferring init');
            return;
        }

        this.element = document.createElement('div');
        this.element.className = 'glyph-run';
        this.element.setAttribute('data-empty', 'true');

        // Container for collapsed glyphs
        this.indicatorContainer = document.createElement('div');
        this.indicatorContainer.className = 'glyph-run-indicators';
        this.element.appendChild(this.indicatorContainer);

        graphContainer.appendChild(this.element);

        this.setupEventListeners();

        // Process any deferred items that tried to add before init
        if (this.deferredItems.length > 0) {
            // Clear timeout as we're processing items now
            if (this.deferredItemsTimeout) {
                clearTimeout(this.deferredItemsTimeout);
                this.deferredItemsTimeout = null;
            }

            const itemsToAdd = [...this.deferredItems];
            this.deferredItems = [];
            itemsToAdd.forEach(item => {
                // Use the add method which uses the factory
                this.add(item, false);
            });
        }
    }

    private setupEventListeners(): void {
        if (!this.element) return;

        // Set up proximity morphing on mouse movement
        document.addEventListener('mousemove', () => {
            this.updateProximity();
        });

        // Note: mouseenter/mouseleave removed - proximity morphing replaces reveal behavior
        // Container has pointer-events: none, only dots are interactive
    }


    /**
     * Trigger proximity-based morphing update
     * Delegates to the proximity handler which modifies styles in place
     */
    private updateProximity(): void {
        this.proximity.updateProximity(
            this.indicatorContainer,
            this.items,
            stripHtml,
            this.isRestoring
        );
    }

    /**
     * Load tray state from uiState
     * Returns array of window IDs that were minimized
     */
    public loadState(): string[] {
        return uiState.getMinimizedWindows();
    }

    /**
     * Add a minimized window to the tray
     * Creates the glyph DOM element ONCE via factory - this element persists forever
     */
    public add(item: Glyph, skipSave: boolean = false): void {
        // Try to initialize, but if it fails, defer the item
        this.init();

        if (!this.element) {
            // Tray not ready yet, defer this item (with safeguards)
            if (this.deferredItems.length >= this.MAX_DEFERRED_ITEMS) {
                log.warn(SEG.GLYPH, `GlyphRun: Deferred items limit reached (${this.MAX_DEFERRED_ITEMS}), dropping oldest`);
                this.deferredItems.shift(); // Remove oldest to make room
            }

            this.deferredItems.push(item);

            // Set a timeout to clear deferred items if init never happens
            if (!this.deferredItemsTimeout) {
                this.deferredItemsTimeout = setTimeout(() => {
                    log.warn(SEG.GLYPH, `GlyphRun: Clearing ${this.deferredItems.length} deferred items after 30s timeout`);
                    this.deferredItems = [];
                    this.deferredItemsTimeout = null;
                }, 30000); // Clear after 30 seconds
            }

            return;
        }

        if (this.items.has(item.id)) {
            return; // Already in tray
        }

        // Verify no duplicate elements exist (hard error if violated)
        this.verifyNoDuplicateElements(item.id);

        this.items.set(item.id, item);

        // USE THE FACTORY - THE ONLY WAY TO CREATE A GLYPH
        const glyph = this.createGlyphElement(item);

        // Add to indicator container
        this.indicatorContainer!.appendChild(glyph);

        this.element.setAttribute('data-empty', 'false');

        // Only save state if not skipping (skip during restore from uiState)
        if (!skipSave) {
            uiState.addMinimizedWindow(item.id);
        }
    }

    /**
     * Verify no duplicate glyph elements exist in DOM
     * Hard errors if duplicates found - this is an AXIOM VIOLATION
     */
    private verifyNoDuplicateElements(glyphId: string): void {
        const elements = document.querySelectorAll(`[data-glyph-id="${glyphId}"]`);
        if (elements.length > 1) {
            throw new Error(
                `AXIOM VIOLATION: ${elements.length} elements found with data-glyph-id="${glyphId}". ` +
                `A glyph must be exactly ONE DOM element. This is a critical error.`
            );
        }
        if (elements.length === 1) {
            throw new Error(
                `AXIOM VIOLATION: Element with data-glyph-id="${glyphId}" already exists. ` +
                `Cannot create duplicate. A glyph must be exactly ONE DOM element.`
            );
        }
    }

    /**
     * Remove a glyph completely (when closed via X button)
     * This is the ONLY time we destroy the DOM element
     */
    public remove(id: string): void {
        if (!this.items.has(id)) return;

        this.items.delete(id);

        // Remove from tracking
        const tracked = this.glyphElements.get(id);
        if (tracked) {
            // Verify it's the same element in DOM
            const inDom = document.querySelector(`[data-glyph-id="${id}"]`);
            if (inDom && inDom !== tracked) {
                throw new Error(
                    `AXIOM VIOLATION: Tracked element for ${id} doesn't match DOM element. ` +
                    `This indicates element recreation.`
                );
            }
            // Remove click handler before removing element
            const handler = this.glyphClickHandlers.get(tracked);
            if (handler) {
                tracked.removeEventListener('click', handler);
                // WeakMap will automatically clean up when element is GC'd
            }
            tracked.remove();
            this.glyphElements.delete(id);
        }

        if (this.items.size === 0) {
            this.element?.setAttribute('data-empty', 'true');
        }

        // Remove from uiState
        uiState.removeMinimizedWindow(id);
    }

    /**
     * Check if a window is in the tray
     */
    public has(id: string): boolean {
        return this.items.has(id);
    }

    /**
     * Get the tray element position for minimize animation target
     */
    public getTargetPosition(): { x: number; y: number } | null {
        if (!this.element) return null;
        const rect = this.element.getBoundingClientRect();
        return {
            x: rect.left + rect.width / 2,
            y: rect.top + rect.height / 2
        };
    }



    /**
     * Verify element tracking for morph operations
     */
    private verifyElementTracking(glyphId: string, element: HTMLElement): void {
        const tracked = this.glyphElements.get(glyphId);
        if (tracked !== element) {
            throw new Error(
                `AXIOM VIOLATION: Element for ${glyphId} doesn't match tracked element. ` +
                `This indicates element recreation somewhere.`
            );
        }
    }

    /**
     * Re-attach a morphed glyph back to the indicator container
     */
    private reattachGlyphToIndicator(glyphElement: HTMLElement, glyph: Glyph): void {
        if (!this.indicatorContainer) return;

        // Remove any existing handler to avoid duplicates
        const existingHandler = this.glyphClickHandlers.get(glyphElement);
        if (existingHandler) {
            glyphElement.removeEventListener('click', existingHandler);
        }

        // Re-attach the click handler
        // (Event listeners can be lost during certain DOM manipulations)
        const clickHandler = (e: MouseEvent) => {
            e.stopPropagation();
            log.debug(SEG.GLYPH, `[Glyph ${glyph.id}] Click detected, windowState:`, isInWindowState(glyphElement));

            if (!isInWindowState(glyphElement)) {
                this.isRestoring = true;

                // Route to correct manifestation based on type
                const manifestationType = glyph.manifestationType || 'window';
                if (manifestationType === 'fullscreen' || manifestationType === 'canvas') {
                    morphToCanvas(
                        glyphElement,
                        glyph,
                        (id, element) => this.verifyElementTracking(id, element),
                        (element, g) => this.reattachGlyphToIndicator(element, g)
                    );
                } else {
                    morphToWindow(
                        glyphElement,
                        glyph,
                        (id, element) => this.verifyElementTracking(id, element),
                        (id) => this.remove(id),
                        (element, g) => this.reattachGlyphToIndicator(element, g)
                    );
                }

                setTimeout(() => {
                    this.isRestoring = false;
                }, getMaximizeDuration());
            }
        };

        this.glyphClickHandlers.set(glyphElement, clickHandler);
        glyphElement.addEventListener('click', clickHandler);

        // Insert at the correct position in the indicator container
        const glyphIndex = Array.from(this.items.keys()).indexOf(glyph.id);
        const glyphs = Array.from(this.indicatorContainer.children);
        if (glyphIndex < glyphs.length) {
            this.indicatorContainer.insertBefore(glyphElement, glyphs[glyphIndex]);
        } else {
            this.indicatorContainer.appendChild(glyphElement);
        }

        // Re-enable proximity morphing
        this.isRestoring = false;
    }


    /**
     * Get count of minimized windows
     */
    public get count(): number {
        return this.items.size;
    }

    /**
     * Verify the structural invariant: Each glyph is exactly ONE DOM element
     * Call this to ensure the system maintains coherence
     *
     * The Glyph must remain the same DOM element across dot → proximity → window → dot.
     * Any implementation that violates this, even invisibly, is incorrect.
     */
    public verifyInvariant(): void {
        // Check that tracked elements match DOM
        this.glyphElements.forEach((trackedElement, id) => {
            const inDom = document.querySelector(`[data-glyph-id="${id}"]`);

            // Verify element exists
            if (!inDom) {
                throw new Error(
                    `INVARIANT VIOLATION: Tracked element for ${id} not found in DOM`
                );
            }

            // Verify it's the SAME element (not a recreation)
            if (inDom !== trackedElement) {
                throw new Error(
                    `INVARIANT VIOLATION: DOM element for ${id} is different from tracked element. ` +
                    `Element was recreated, violating the single-element axiom.`
                );
            }

            // Verify no duplicates
            const allWithId = document.querySelectorAll(`[data-glyph-id="${id}"]`);
            if (allWithId.length !== 1) {
                throw new Error(
                    `INVARIANT VIOLATION: Found ${allWithId.length} elements with data-glyph-id="${id}". ` +
                    `Must be exactly one.`
                );
            }
        });

        // Check that all DOM glyphs are tracked
        document.querySelectorAll('[data-glyph-id]').forEach((element) => {
            const id = element.getAttribute('data-glyph-id');
            if (id && !this.glyphElements.has(id)) {
                throw new Error(
                    `INVARIANT VIOLATION: DOM element with data-glyph-id="${id}" is not tracked. ` +
                    `Element was created outside the factory.`
                );
            }
        });

        log.info(SEG.GLYPH, `✓ Invariant verified: ${this.glyphElements.size} glyphs maintain single-element axiom`);
    }
}

// Singleton instance
export const glyphRun = new GlyphRunImpl();
