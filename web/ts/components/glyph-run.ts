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

import { log, SEG } from '../logger';
import { uiState } from '../state/ui';
import { GlyphProximity } from './glyph-proximity';

export interface Glyph {
    id: string;
    title: string;
    renderContent: () => HTMLElement;    // Function to render window content
    initialWidth?: string;                // Window width (e.g., "800px")
    initialHeight?: string;               // Window height (e.g., "600px")
    defaultX?: number;                    // Window default X position
    defaultY?: number;                    // Window default Y position
    onClose?: () => void;
}

class GlyphRunImpl {
    // Track all created glyph elements to enforce single-element axiom
    private glyphElements: Map<string, HTMLElement> = new Map();

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
        glyph.setAttribute('data-glyph-id', item.id);

        // Track this element
        this.glyphElements.set(item.id, glyph);

        // Attach click handler that will persist with the element forever
        glyph.addEventListener('click', (e) => {
            e.stopPropagation();

            // Only morph if in collapsed state (not already a window)
            if (!glyph.dataset.windowState) {
                this.isRestoring = true;
                this.morphToWindow(glyph, item);
            }
        });

        return glyph;
    }
    // Deferred items to add after init
    private deferredItems: Glyph[] = [];

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
            log.warn(SEG.UI, 'GlyphRun: #graph-container not found, deferring init');
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
            (html) => this.stripHtml(html),
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
            // Tray not ready yet, defer this item
            this.deferredItems.push(item);
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
     * Strip HTML tags from title for plain text display in expanded dots
     */
    private stripHtml(html: string): string {
        const doc = new DOMParser().parseFromString(html, 'text/html');
        return doc.body.textContent || '';
    }

    /**
     * Morph a glyph (dot) into a full window
     * The glyph DOM element itself transforms through animation
     */
    private morphToWindow(glyphElement: HTMLElement, glyph: Glyph): void {
        // AXIOM CHECK: Verify this is the correct element
        const tracked = this.glyphElements.get(glyph.id);
        if (tracked !== glyphElement) {
            throw new Error(
                `AXIOM VIOLATION: morphToWindow called with wrong element for ${glyph.id}. ` +
                `This indicates element recreation somewhere.`
            );
        }

        // Verify no duplicates exist
        const elements = document.querySelectorAll(`[data-glyph-id="${glyph.id}"]`);
        if (elements.length !== 1) {
            throw new Error(
                `AXIOM VIOLATION: Expected exactly 1 element for ${glyph.id}, found ${elements.length}`
            );
        }
        // Get current glyph position and size
        const glyphRect = glyphElement.getBoundingClientRect();

        // Calculate window target position (center of screen by default)
        const windowWidth = parseInt(glyph.initialWidth || '800px');
        const windowHeight = parseInt(glyph.initialHeight || '600px');
        const targetX = glyph.defaultX ?? (window.innerWidth - windowWidth) / 2;
        const targetY = glyph.defaultY ?? (window.innerHeight - windowHeight) / 2;

        // THE GLYPH ITSELF BECOMES THE WINDOW - NO CLONING
        // Remove from indicator container and reparent to body
        glyphElement.remove(); // Detach from current parent (keeps element alive)

        // Apply initial fixed positioning at current location
        glyphElement.className = 'glyph-morphing-to-window';
        glyphElement.style.position = 'fixed';
        glyphElement.style.left = `${glyphRect.left}px`;
        glyphElement.style.top = `${glyphRect.top}px`;
        glyphElement.style.width = `${glyphRect.width}px`;
        glyphElement.style.height = `${glyphRect.height}px`;
        glyphElement.style.zIndex = '1000';

        // Clear any proximity text that might be present
        if (glyphElement.dataset.hasText) {
            glyphElement.textContent = '';
            delete glyphElement.dataset.hasText;
        }

        // Reparent to document body for morphing
        document.body.appendChild(glyphElement);

        // Mark element as in-window-state (but keep glyph ID)
        glyphElement.dataset.windowState = 'true';

        // Trigger morph animation after a frame to ensure initial styles are applied
        requestAnimationFrame(() => {
            // Apply window dimensions and position
            glyphElement.style.left = `${targetX}px`;
            glyphElement.style.top = `${targetY}px`;
            glyphElement.style.width = `${windowWidth}px`;
            glyphElement.style.height = `${windowHeight}px`;
            glyphElement.style.borderRadius = '8px';
            glyphElement.style.backgroundColor = 'var(--bg-primary)';
            glyphElement.style.boxShadow = '0 8px 32px rgba(0, 0, 0, 0.3)';
            glyphElement.style.opacity = '1'; // Ensure it's visible

            // After animation completes, add window content
            setTimeout(() => {
                // Add window chrome (title bar, controls)
                const titleBar = document.createElement('div');
                titleBar.className = 'window-title-bar';
                titleBar.style.height = '32px';
                titleBar.style.backgroundColor = 'var(--bg-secondary)';
                titleBar.style.borderBottom = '1px solid var(--border-color)';
                titleBar.style.display = 'flex';
                titleBar.style.alignItems = 'center';
                titleBar.style.padding = '0 12px';

                // Add title
                const titleText = document.createElement('span');
                titleText.textContent = this.stripHtml(glyph.title);
                titleText.style.flex = '1';
                titleBar.appendChild(titleText);

                // Add minimize button
                const minimizeBtn = document.createElement('button');
                minimizeBtn.textContent = '−';
                minimizeBtn.style.width = '24px';
                minimizeBtn.style.height = '24px';
                minimizeBtn.style.border = 'none';
                minimizeBtn.style.background = 'transparent';
                minimizeBtn.style.cursor = 'pointer';
                minimizeBtn.onclick = () => this.morphToGlyph(glyphElement, glyph);
                titleBar.appendChild(minimizeBtn);

                // Add close button if glyph has onClose
                if (glyph.onClose) {
                    const closeBtn = document.createElement('button');
                    closeBtn.textContent = '×';
                    closeBtn.style.width = '24px';
                    closeBtn.style.height = '24px';
                    closeBtn.style.border = 'none';
                    closeBtn.style.background = 'transparent';
                    closeBtn.style.cursor = 'pointer';
                    closeBtn.onclick = () => {
                        // Remove from tray data AND remove element
                        this.remove(glyph.id);
                        glyphElement.remove();
                        glyph.onClose!();
                    };
                    titleBar.appendChild(closeBtn);
                }

                glyphElement.appendChild(titleBar);

                // Add content area
                const content = glyph.renderContent();
                content.style.padding = '16px';
                content.style.height = 'calc(100% - 32px)';
                content.style.overflow = 'auto';
                glyphElement.appendChild(content);

                // Make window draggable
                this.makeWindowDraggable(glyphElement, titleBar);

                // Re-enable proximity morphing
                this.isRestoring = false;
            }, 600); // Match animation duration
        });
    }

    /**
     * Morph a window back into a glyph (dot)
     * THE SAME ELEMENT morphs back - no new elements created
     */
    private morphToGlyph(windowElement: HTMLElement, glyph: Glyph): void {
        // Ensure glyph is still in our items (may have been removed via close)
        if (!this.items.has(glyph.id)) {
            // If it's not in items, the user closed it, so just remove the element
            windowElement.remove();
            return;
        }

        // Get target position in the indicator container
        if (!this.indicatorContainer) return;

        // Find the position where this glyph should go back to
        const glyphIndex = Array.from(this.items.keys()).indexOf(glyph.id);
        const targetRect = this.calculateGlyphTargetPosition(glyphIndex);

        // Clear window state flag
        delete windowElement.dataset.windowState;

        // Add morphing class
        windowElement.className = 'window-morphing-to-glyph';

        // Clear window content (title bar, content area)
        windowElement.innerHTML = '';

        // Start morphing back to dot at the target position
        windowElement.style.left = `${targetRect.x}px`;
        windowElement.style.top = `${targetRect.y}px`;
        windowElement.style.width = '8px';
        windowElement.style.height = '8px';
        windowElement.style.borderRadius = '2px';
        windowElement.style.backgroundColor = 'var(--bg-gray)';
        windowElement.style.boxShadow = 'none';
        windowElement.style.padding = '0';
        windowElement.style.border = '1px solid var(--border-on-dark)';

        // After animation completes, move element back to indicator container
        setTimeout(() => {
            // Remove from body
            windowElement.remove();

            // Reset to glyph class
            windowElement.className = 'glyph-run-glyph';

            // Reset inline styles (let CSS take over)
            windowElement.style.position = '';
            windowElement.style.left = '';
            windowElement.style.top = '';
            windowElement.style.width = '';
            windowElement.style.height = '';
            windowElement.style.borderRadius = '';
            windowElement.style.backgroundColor = '';
            windowElement.style.boxShadow = '';
            windowElement.style.padding = '';
            windowElement.style.border = '';
            windowElement.style.zIndex = '';
            windowElement.style.opacity = '';

            // Keep the glyph ID
            windowElement.setAttribute('data-glyph-id', glyph.id);

            // Re-attach click handler
            windowElement.addEventListener('click', (e) => {
                e.stopPropagation();
                this.isRestoring = true;
                this.morphToWindow(windowElement, glyph);
            });

            // Insert at the correct position in the indicator container
            const glyphs = Array.from(this.indicatorContainer!.children);
            if (glyphIndex < glyphs.length) {
                this.indicatorContainer!.insertBefore(windowElement, glyphs[glyphIndex]);
            } else {
                this.indicatorContainer!.appendChild(windowElement);
            }

            // Re-enable proximity morphing
            this.isRestoring = false;
        }, 600);
    }

    /**
     * Calculate where a glyph should be positioned in the tray
     */
    private calculateGlyphTargetPosition(index: number): { x: number, y: number } {
        if (!this.element) return { x: 0, y: 0 };

        const trayRect = this.element.getBoundingClientRect();
        const glyphSize = 8;
        const gap = 2;

        // Stack vertically
        return {
            x: trayRect.right - glyphSize - 4, // 4px from right edge
            y: trayRect.top + (index * (glyphSize + gap))
        };
    }

    /**
     * Make a window draggable by its title bar
     */
    private makeWindowDraggable(windowElement: HTMLElement, handle: HTMLElement): void {
        let isDragging = false;
        let startX = 0;
        let startY = 0;
        let initialX = 0;
        let initialY = 0;

        const startDrag = (e: MouseEvent) => {
            isDragging = true;
            startX = e.clientX;
            startY = e.clientY;
            const rect = windowElement.getBoundingClientRect();
            initialX = rect.left;
            initialY = rect.top;
            e.preventDefault();
        };

        const drag = (e: MouseEvent) => {
            if (!isDragging) return;
            const deltaX = e.clientX - startX;
            const deltaY = e.clientY - startY;
            windowElement.style.left = `${initialX + deltaX}px`;
            windowElement.style.top = `${initialY + deltaY}px`;
        };

        const stopDrag = () => {
            isDragging = false;
        };

        handle.addEventListener('mousedown', startDrag);
        document.addEventListener('mousemove', drag);
        document.addEventListener('mouseup', stopDrag);
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

        console.log(`✓ Invariant verified: ${this.glyphElements.size} glyphs maintain single-element axiom`);
    }
}

// Singleton instance
export const glyphRun = new GlyphRunImpl();
