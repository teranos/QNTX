/**
 * Dock - Unified Container for All Dots
 *
 * Replaces both Symbol Palette and WindowTray with a single system.
 * All features, windows, and panels are represented as dots that can expand.
 *
 * Preserves WindowTray's carefully tuned proximity morphing:
 * - Distance-based expansion (8px → 220px)
 * - Direction-aware easing (horizontal vs vertical)
 * - Baseline boost when any dot is close
 * - Smooth 60fps RAF updates
 *
 * Architecture:
 * - Dots are registered with Dock
 * - Dock handles rendering and proximity morphing
 * - Dots handle their own expand/collapse logic
 */

import { Dot } from './dot';
import { log, SEG } from '../logger';

class DockImpl {
    // Proximity morphing configuration (preserved from WindowTray)
    private readonly PROXIMITY_THRESHOLD_HORIZONTAL = 30;
    private readonly PROXIMITY_THRESHOLD_VERTICAL = 110;
    private readonly SNAP_THRESHOLD = 0.9;
    private readonly BASELINE_BOOST_TRIGGER = 0.80;
    private readonly BASELINE_BOOST_AMOUNT = 0.3;
    private readonly TEXT_FADE_THRESHOLD = 0.5;

    // Horizontal easing: gradual approach, dramatic finish
    private readonly HORIZONTAL_EASE_BREAKPOINT = 0.8;
    private readonly HORIZONTAL_EASE_EARLY = 0.4;
    private readonly HORIZONTAL_EASE_LATE = 0.6;

    // Vertical easing: fast bloom, slow refinement
    private readonly VERTICAL_EASE_BREAKPOINT = 0.55;
    private readonly VERTICAL_EASE_EARLY = 0.8;
    private readonly VERTICAL_EASE_LATE = 0.2;

    // Morphing dimensions (preserved from WindowTray)
    private readonly DOT_MIN_WIDTH = 8;
    private readonly DOT_MIN_HEIGHT = 8;
    private readonly DOT_MAX_WIDTH = 220;
    private readonly DOT_MAX_HEIGHT = 32;
    private readonly DOT_BORDER_RADIUS_MAX = 2;

    // Component state
    private element: HTMLElement | null = null;
    private container: HTMLElement | null = null;
    private dots: Map<string, Dot> = new Map();
    private mouseX: number = 0;
    private mouseY: number = 0;
    private proximityRAF: number | null = null;
    private isAnimating: boolean = false; // Disable morphing during expand/collapse

    /**
     * Initialize dock and attach to DOM
     */
    public init(): void {
        if (this.element) return; // Already initialized

        const graphContainer = document.getElementById('graph-container');
        if (!graphContainer) {
            log.warn(SEG.UI, 'Dock: #graph-container not found, deferring init');
            return;
        }

        // Create dock element (replaces window-tray)
        this.element = document.createElement('div');
        this.element.className = 'dock';
        this.element.setAttribute('data-empty', 'true');

        // Dot container
        this.container = document.createElement('div');
        this.container.className = 'dock-container';
        this.element.appendChild(this.container);

        graphContainer.appendChild(this.element);

        this.setupEventListeners();
        log.debug(SEG.UI, 'Dock initialized');
    }

    /**
     * Register a dot with the dock
     */
    public register(dot: Dot): void {
        if (this.dots.has(dot.id)) {
            log.warn(SEG.UI, `Dot already registered: ${dot.id}`);
            return;
        }

        this.dots.set(dot.id, dot);
        this.render();
        log.debug(SEG.UI, `Dot registered: ${dot.id}`);
    }

    /**
     * Unregister a dot from the dock
     */
    public unregister(id: string): void {
        const dot = this.dots.get(id);
        if (!dot) return;

        this.dots.delete(id);
        this.render();
        log.debug(SEG.UI, `Dot unregistered: ${id}`);
    }

    /**
     * Get a dot by ID
     */
    public get(id: string): Dot | undefined {
        return this.dots.get(id);
    }

    /**
     * Setup global event listeners for proximity morphing
     */
    private setupEventListeners(): void {
        if (!this.element) return;

        // Track mouse position globally for proximity effect
        document.addEventListener('mousemove', (e) => {
            this.mouseX = e.clientX;
            this.mouseY = e.clientY;
            this.updateProximity();
        });
    }

    /**
     * Render all dots
     */
    private render(): void {
        if (!this.container) return;

        // Clear existing
        this.container.innerHTML = '';

        // Render dots in order
        this.dots.forEach((dot) => {
            const element = dot.createElement();
            this.container!.appendChild(element);
        });

        // Update empty state
        this.element?.setAttribute('data-empty', this.dots.size === 0 ? 'true' : 'false');
    }

    /**
     * Calculate proximity metrics for a dot element (preserved from WindowTray)
     */
    private calculateProximity(dot: HTMLElement): {
        distance: number;
        distanceX: number;
        distanceY: number;
        proximityRaw: number;
        isVerticalApproach: boolean;
    } {
        const rect = dot.getBoundingClientRect();
        let distanceX = 0, distanceY = 0;

        // Horizontal distance to nearest edge
        if (this.mouseX < rect.left) {
            distanceX = rect.left - this.mouseX;
        } else if (this.mouseX > rect.right) {
            distanceX = this.mouseX - rect.right;
        }

        // Vertical distance to nearest edge
        if (this.mouseY < rect.top) {
            distanceY = rect.top - this.mouseY;
        } else if (this.mouseY > rect.bottom) {
            distanceY = this.mouseY - rect.bottom;
        }

        // Euclidean distance to nearest edge
        const distance = Math.sqrt(distanceX * distanceX + distanceY * distanceY);

        // Determine approach direction
        const isVerticalApproach = distanceY > distanceX;

        // Use appropriate threshold
        const threshold = isVerticalApproach
            ? this.PROXIMITY_THRESHOLD_VERTICAL
            : this.PROXIMITY_THRESHOLD_HORIZONTAL;

        // Calculate proximity factor (1.0 = at dot, 0.0 = at threshold)
        const proximityRaw = Math.max(0, 1 - (distance / threshold));

        return { distance, distanceX, distanceY, proximityRaw, isVerticalApproach };
    }

    /**
     * Update proximity-based morphing for each dot (preserved from WindowTray)
     * Uses requestAnimationFrame for smooth 60fps updates
     */
    private updateProximity(): void {
        if (this.proximityRAF) {
            cancelAnimationFrame(this.proximityRAF);
        }

        this.proximityRAF = requestAnimationFrame(() => {
            if (!this.container || this.isAnimating) return;

            const dotElements = Array.from(this.container.querySelectorAll('.dock-dot')) as HTMLElement[];
            const dotArray = Array.from(this.dots.values());

            // First pass: check if any dot is highly proximate (baseline boost)
            let maxProximityRaw = 0;
            dotElements.forEach((dotEl) => {
                const { proximityRaw } = this.calculateProximity(dotEl);
                maxProximityRaw = Math.max(maxProximityRaw, proximityRaw);
            });

            // Calculate baseline boost
            const baselineBoost = maxProximityRaw > this.BASELINE_BOOST_TRIGGER
                ? this.BASELINE_BOOST_AMOUNT
                : 0;

            dotElements.forEach((dotEl, index) => {
                const { proximityRaw, isVerticalApproach } = this.calculateProximity(dotEl);

                // Apply direction-aware easing
                let proximity: number;

                if (proximityRaw >= this.SNAP_THRESHOLD) {
                    proximity = 1.0;
                } else {
                    if (isVerticalApproach) {
                        // VERTICAL: Fast early growth, slow refinement
                        if (proximityRaw < this.VERTICAL_EASE_BREAKPOINT) {
                            proximity = (proximityRaw / this.VERTICAL_EASE_BREAKPOINT) * this.VERTICAL_EASE_EARLY;
                        } else {
                            const remaining = 1.0 - this.VERTICAL_EASE_BREAKPOINT;
                            proximity = this.VERTICAL_EASE_EARLY +
                                      ((proximityRaw - this.VERTICAL_EASE_BREAKPOINT) / remaining) * this.VERTICAL_EASE_LATE;
                        }
                    } else {
                        // HORIZONTAL: Gradual growth, dramatic finish
                        if (proximityRaw < this.HORIZONTAL_EASE_BREAKPOINT) {
                            proximity = (proximityRaw / this.HORIZONTAL_EASE_BREAKPOINT) * this.HORIZONTAL_EASE_EARLY;
                        } else {
                            const remaining = this.SNAP_THRESHOLD - this.HORIZONTAL_EASE_BREAKPOINT;
                            proximity = this.HORIZONTAL_EASE_EARLY +
                                      ((proximityRaw - this.HORIZONTAL_EASE_BREAKPOINT) / remaining) * this.HORIZONTAL_EASE_LATE;
                        }
                    }
                }

                // Apply baseline boost
                proximity = Math.min(1.0, proximity + baselineBoost);

                // Interpolate dimensions
                const width = this.DOT_MIN_WIDTH + (this.DOT_MAX_WIDTH - this.DOT_MIN_WIDTH) * proximity;
                const height = this.DOT_MIN_HEIGHT + (this.DOT_MAX_HEIGHT - this.DOT_MIN_HEIGHT) * proximity;
                const borderRadius = this.DOT_BORDER_RADIUS_MAX * (1 - proximity);

                // Interpolate colors (gray → almost-black)
                const startR = 153, startG = 153, startB = 153;
                const endR = 26, endG = 26, endB = 26;
                let r = Math.round(startR + (endR - startR) * proximity);
                let g = Math.round(startG + (endG - startG) * proximity);
                let b = Math.round(startB + (endB - startB) * proximity);

                // Brighten on hover
                const isHovered = dotEl.matches(':hover');
                if (isHovered) {
                    r = Math.min(255, Math.round(r + (255 - r) * 0.1));
                    g = Math.min(255, Math.round(g + (255 - g) * 0.1));
                    b = Math.min(255, Math.round(b + (255 - b) * 0.1));
                }

                // Apply morphing styles
                dotEl.style.width = `${width}px`;
                dotEl.style.height = `${height}px`;
                dotEl.style.borderRadius = `${borderRadius}px`;
                dotEl.style.backgroundColor = `rgb(${r}, ${g}, ${b})`;

                // Show text when proximity exceeds threshold
                if (proximity > this.TEXT_FADE_THRESHOLD && index < dotArray.length) {
                    const dot = dotArray[index];
                    const title = this.stripHtml(dot.title);

                    if (!dotEl.dataset.hasText) {
                        dotEl.style.display = 'flex';
                        dotEl.style.alignItems = 'center';
                        dotEl.style.justifyContent = 'flex-start';
                        dotEl.style.padding = '6px 10px';
                        dotEl.style.whiteSpace = 'nowrap';
                        dotEl.textContent = title;
                        dotEl.dataset.hasText = 'true';
                    }
                    dotEl.style.opacity = String(this.TEXT_FADE_THRESHOLD + (proximity - this.TEXT_FADE_THRESHOLD));
                } else {
                    if (dotEl.dataset.hasText) {
                        dotEl.textContent = '';
                        dotEl.style.display = '';
                        dotEl.style.alignItems = '';
                        dotEl.style.justifyContent = '';
                        dotEl.style.padding = '';
                        dotEl.style.whiteSpace = '';
                        delete dotEl.dataset.hasText;
                    }
                    dotEl.style.opacity = '1';
                }
            });

            this.proximityRAF = null;
        });
    }

    /**
     * Strip HTML tags from title for text display
     */
    private stripHtml(html: string): string {
        const doc = new DOMParser().parseFromString(html, 'text/html');
        return doc.body.textContent || '';
    }

    /**
     * Get count of registered dots
     */
    public get count(): number {
        return this.dots.size;
    }
}

// Singleton instance
export const dock = new DockImpl();
