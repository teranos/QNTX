// Zoom and pan behavior for graph visualization
// Handles zoom constraints, pan detection, and unfocus triggers

import { GRAPH_PHYSICS } from '../config.ts';
import { getG, getIsFocusAnimating } from './state.ts';
import { isFocused, unfocus } from './focus.ts';
import type { ZoomBehavior, ZoomEvent } from '../../types/d3-graph';

// Import D3 from vendor bundle
declare const d3: any;

// Callback to save session after zoom/pan
type SaveSessionCallback = () => void;

/**
 * Create D3 zoom behavior with unfocus detection
 * Tracks user zoom/pan interactions and triggers unfocus when user navigates away from focused tile
 *
 * @param saveSessionCallback - Function to call after zoom/pan ends to save state
 * @returns Configured D3 zoom behavior
 */
export function createZoomBehavior(saveSessionCallback: SaveSessionCallback): ZoomBehavior {
    // Track zoom state for unfocus detection
    let zoomStartTransform: { x: number; y: number; k: number } | null = null;

    const zoom = d3.zoom()
        .scaleExtent([GRAPH_PHYSICS.ZOOM_MIN, GRAPH_PHYSICS.ZOOM_MAX])
        .on("start", function(event: ZoomEvent) {
            // Capture starting transform for pan detection
            zoomStartTransform = {
                x: event.transform.x,
                y: event.transform.y,
                k: event.transform.k
            };
        })
        .on("zoom", function(event: ZoomEvent) {
            const g = getG();
            if (g) {
                g.attr("transform", event.transform.toString());
            }

            // Skip unfocus detection during programmatic focus/unfocus animations
            if (getIsFocusAnimating()) {
                return;
            }

            // Detect unfocus triggers while focused
            if (isFocused() && zoomStartTransform) {
                const currentScale = event.transform.k;
                const startScale = zoomStartTransform.k;

                // Unfocus if user zooms out significantly
                if (currentScale < startScale * 0.85) {
                    unfocus();
                    return;
                }

                // Unfocus if user pans significantly (more than 50px in either direction)
                const panDeltaX = Math.abs(event.transform.x - zoomStartTransform.x);
                const panDeltaY = Math.abs(event.transform.y - zoomStartTransform.y);
                if (panDeltaX > 50 || panDeltaY > 50) {
                    unfocus();
                    return;
                }
            }
        })
        .on("end", function() {
            // Save transform state after zoom/pan
            saveSessionCallback();
            zoomStartTransform = null;
        });

    return zoom;
}

/**
 * Setup click handler on SVG to unfocus when clicking empty space
 *
 * @param svg - D3 selection of the SVG element
 */
export function setupSVGClickHandler(svg: any): void {
    svg.on("click", function(event: MouseEvent) {
        // Only unfocus if clicking directly on the SVG (not on a node)
        if (event.target === svg.node() && isFocused()) {
            unfocus();
        }
    });
}
