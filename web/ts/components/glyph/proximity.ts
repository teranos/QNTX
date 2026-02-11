/**
 * Proximity morphing for glyphs
 *
 * Handles the smooth transformation of glyphs from 8px dots to 220px expanded state
 * based on pointer proximity (mouse cursor or touch position).
 * This modifies the SAME DOM element in place.
 *
 * Desktop: mousemove drives proximity continuously.
 * Mobile:  touchstart near tray enters browse mode, touchmove drives proximity,
 *          touchend opens the peaked glyph. Between touches there is no pointer.
 *
 * CRITICAL: We ONLY change styles, never recreate or replace the element.
 * The element persists through: dot → proximity → window → dot
 */

import type { Glyph } from './glyph';
import { hasProximityText, setProximityText } from './dataset';

export class GlyphProximity {
    // Proximity morphing configuration
    private readonly PROXIMITY_THRESHOLD_HORIZONTAL = 30; // Max distance for horizontal approach (px)
    private readonly PROXIMITY_THRESHOLD_VERTICAL = 110; // Max distance for vertical approach (px)
    private readonly SNAP_THRESHOLD = 0.9; // Snap to 100% at this proximity to prevent flickering
    private readonly BASELINE_BOOST_TRIGGER = 0.80; // Trigger baseline boost when any item this close
    private readonly BASELINE_BOOST_AMOUNT = 0.3; // Amount to boost all items (0.0-1.0)
    private readonly TEXT_FADE_THRESHOLD = 0.5; // Show text when proximity exceeds this

    // Horizontal easing: gradual approach, dramatic finish
    private readonly HORIZONTAL_EASE_BREAKPOINT = 0.8; // 80% proximity
    private readonly HORIZONTAL_EASE_EARLY = 0.4; // Transform 40% by breakpoint
    private readonly HORIZONTAL_EASE_LATE = 0.6; // Remaining 60% in final stretch

    // Vertical easing: fast bloom, slow refinement (inverted)
    private readonly VERTICAL_EASE_BREAKPOINT = 0.55; // 55% proximity
    private readonly VERTICAL_EASE_EARLY = 0.8; // Transform 80% by breakpoint
    private readonly VERTICAL_EASE_LATE = 0.2; // Remaining 20% in final stretch

    // Morphing dimensions
    private readonly DOT_MIN_WIDTH = 8;
    private readonly DOT_MIN_HEIGHT = 8;
    private readonly DOT_MAX_WIDTH = 220;
    private readonly DOT_MAX_HEIGHT = 32;
    private readonly DOT_BORDER_RADIUS_MAX = 2; // Initial border radius for dots

    private mouseX: number = 0;
    private mouseY: number = 0;
    private proximityRAF: number | null = null;

    // Touch browse state — active while finger is down in the tray zone
    private _isTouchBrowsing: boolean = false;

    constructor() {
        this.setupPointerTracking();
    }

    /** True while the user's finger is down and sliding through the tray */
    public get isTouchBrowsing(): boolean {
        return this._isTouchBrowsing;
    }

    public set isTouchBrowsing(v: boolean) {
        this._isTouchBrowsing = v;
    }

    /** Feed pointer coordinates from any input source */
    public setPointerPosition(x: number, y: number): void {
        this.mouseX = x;
        this.mouseY = y;
    }

    private setupPointerTracking(): void {
        // Desktop: track mouse position globally for proximity effect
        document.addEventListener('mousemove', (e) => {
            this.mouseX = e.clientX;
            this.mouseY = e.clientY;
        });

        // Mobile: touchmove feeds the same coordinates during browse mode.
        // The actual touchstart/touchend lifecycle is managed by GlyphRun
        // which calls setPointerPosition and sets isTouchBrowsing.
    }

    /**
     * Calculate proximity metrics for a glyph element
     */
    public calculateProximity(glyph: HTMLElement): {
        distance: number;
        distanceX: number;
        distanceY: number;
        proximityRaw: number;
        isVerticalApproach: boolean;
    } {
        const rect = glyph.getBoundingClientRect();
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

        // Euclidean distance to nearest edge (0 if inside)
        const distance = Math.sqrt(distanceX * distanceX + distanceY * distanceY);

        // Determine approach direction
        const isVerticalApproach = distanceY > distanceX;

        // Use appropriate threshold based on approach direction
        const threshold = isVerticalApproach
            ? this.PROXIMITY_THRESHOLD_VERTICAL
            : this.PROXIMITY_THRESHOLD_HORIZONTAL;

        // Calculate proximity factor (1.0 = at dot, 0.0 = at threshold or beyond)
        const proximityRaw = Math.max(0, 1 - (distance / threshold));

        return { distance, distanceX, distanceY, proximityRaw, isVerticalApproach };
    }

    /**
     * Update proximity-based morphing for glyphs in the indicator container
     * Uses requestAnimationFrame for smooth 60fps updates
     */
    public updateProximity(
        indicatorContainer: HTMLElement | null,
        items: Map<string, Glyph>,
        stripHtml: (html: string) => string,
        isRestoring: boolean
    ): void {
        if (this.proximityRAF) {
            cancelAnimationFrame(this.proximityRAF);
        }

        this.proximityRAF = requestAnimationFrame(() => {
            if (!indicatorContainer || isRestoring) return;

            const glyphs = Array.from(indicatorContainer.querySelectorAll('.glyph-run-glyph')) as HTMLElement[];
            const itemsArray = Array.from(items.values());

            // First pass: check if any glyph is highly proximate (gives baseline boost to all)
            let maxProximityRaw = 0;
            glyphs.forEach((glyph) => {
                const { proximityRaw } = this.calculateProximity(glyph);
                maxProximityRaw = Math.max(maxProximityRaw, proximityRaw);
            });

            // Calculate baseline boost when any glyph is nearly fully expanded
            const baselineBoost = maxProximityRaw > this.BASELINE_BOOST_TRIGGER ? this.BASELINE_BOOST_AMOUNT : 0;

            glyphs.forEach((glyph, index) => {
                const { proximityRaw, isVerticalApproach } = this.calculateProximity(glyph);

                // Apply different easing based on approach direction
                let proximity: number;

                // Snap to 100% when very close to prevent flickering
                if (proximityRaw >= this.SNAP_THRESHOLD) {
                    proximity = 1.0;
                } else {

                    if (isVerticalApproach) {
                        // VERTICAL: Inverted easing - fast early growth, slow refinement
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

                // Apply baseline boost when any item is being hovered
                proximity = Math.min(1.0, proximity + baselineBoost);

                // Interpolate dimensions to match actual tray item size
                const width = this.DOT_MIN_WIDTH + (this.DOT_MAX_WIDTH - this.DOT_MIN_WIDTH) * proximity;
                const height = this.DOT_MIN_HEIGHT + (this.DOT_MAX_HEIGHT - this.DOT_MIN_HEIGHT) * proximity;

                // Interpolate border radius (starts at max, goes to 0 for full item)
                const borderRadius = this.DOT_BORDER_RADIUS_MAX * (1 - proximity);

                // Interpolate colors using RGB interpolation
                // Start: --bg-gray (#999 = rgb(153,153,153))
                // End: --bg-almost-black (#1a1a1a = rgb(26,26,26))
                const startR = 153, startG = 153, startB = 153;
                const endR = 26, endG = 26, endB = 26;
                let r = Math.round(startR + (endR - startR) * proximity);
                let g = Math.round(startG + (endG - startG) * proximity);
                let b = Math.round(startB + (endB - startB) * proximity);

                // Brighten on hover (10% lighter)
                const isHovered = glyph.matches(':hover');
                if (isHovered) {
                    r = Math.min(255, Math.round(r + (255 - r) * 0.1));
                    g = Math.min(255, Math.round(g + (255 - g) * 0.1));
                    b = Math.min(255, Math.round(b + (255 - b) * 0.1));
                }

                // Apply morphing styles
                glyph.style.width = `${width}px`;
                glyph.style.height = `${height}px`;
                glyph.style.borderRadius = `${borderRadius}px`;
                glyph.style.backgroundColor = `rgb(${r}, ${g}, ${b})`;

                // Show title text when proximity exceeds threshold
                if (proximity > this.TEXT_FADE_THRESHOLD && index < itemsArray.length) {
                    const item = itemsArray[index];
                    const title = stripHtml(item.title);

                    // Add text content if not already present
                    if (!hasProximityText(glyph)) {
                        glyph.style.display = 'flex';
                        glyph.style.alignItems = 'center';
                        glyph.style.justifyContent = 'flex-start'; // Left-align text (normal)
                        glyph.style.padding = '6px 10px';
                        glyph.style.whiteSpace = 'nowrap';
                        glyph.textContent = title;
                        setProximityText(glyph, true);
                    }
                    // Fade in text based on proximity (above threshold)
                    glyph.style.opacity = String(this.TEXT_FADE_THRESHOLD + (proximity - this.TEXT_FADE_THRESHOLD));
                } else {
                    // Hide text when far away
                    if (hasProximityText(glyph)) {
                        glyph.textContent = '';
                        glyph.style.display = '';
                        glyph.style.alignItems = '';
                        glyph.style.justifyContent = '';
                        glyph.style.padding = '';
                        glyph.style.whiteSpace = '';
                        glyph.style.textAlign = '';
                        setProximityText(glyph, false);
                    }
                    glyph.style.opacity = '1';
                }
            });

            this.proximityRAF = null;
        });
    }

    /**
     * Get current mouse position
     */
    public getMousePosition(): { x: number, y: number } {
        return { x: this.mouseX, y: this.mouseY };
    }
}