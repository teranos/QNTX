/**
 * What a window is, once its morph has committed.
 *
 * `window` is one row in MANIFESTATIONS and had two constructors: a tray dot
 * opening (window.ts) and a canvas-placed glyph being lifted (canvas-window.ts).
 * Both wrote `data-manifestation="window"` and neither agreed with the other on
 * what that meant — a content-sized box against a fixed 520x420, a cap on one
 * and none on the other, a z-index that rose on a press and one that sat at the
 * base and never moved. The table names the state. Nothing said what the state
 * is, so the two drifted where nothing was watching.
 *
 * This is what says it. The box is the caller's — it is the morph's own target,
 * and how each path arrives at one is its own business. Everything a window is
 * regardless of where it came from is here, written once.
 *
 * What is deliberately NOT here: what the glyph wears. Its colour, its border,
 * its padding are data on the glyph and never a property of a manifestation
 * (VISION.md) — the tray path reads them off the Glyph, the canvas path leaves
 * the element wearing what it already wore, and both are the same intent
 * reached by different routes. Nor the content: one path rebuilds from
 * renderContent(), the other wraps the children it already has so a scroll
 * position and a half-typed textarea survive the lift.
 */

import { getWindowBorderRadius } from '../config';
import {
    WINDOW_BOX_SHADOW,
    MAX_VIEWPORT_WIDTH_RATIO,
    MAX_VIEWPORT_HEIGHT_RATIO,
} from '../glyph';
import { setNaturalWidth } from '../window-drag';
import { raise, raiseOnInteract } from '../z-order';

/**
 * The box a morph animated to.
 *
 * `width` and `height` are the numbers the animation ended on, and they stay
 * numbers even when an axis is written as `fit-content`: a drag against an edge
 * reflows from what the window measures, and `fit-content` is not a number.
 */
export interface WindowBox {
    x: number;
    y: number;
    width: number;
    height: number;
    /** Written instead of `${width}px` when the content owns the axis. */
    widthStyle?: string;
    /** Written instead of `${height}px` when the content owns the axis. */
    heightStyle?: string;
}

/**
 * Settle an element into the window manifestation.
 *
 * Call at morph commit, from either constructor. Idempotent: a glyph reopened
 * is one element (Element Axioma), and settling it again neither stacks its
 * press listener nor loses its place in the stack.
 */
export function settleWindow(element: HTMLElement, box: WindowBox): void {
    element.style.position = 'fixed';
    element.style.left = `${box.x}px`;
    element.style.top = `${box.y}px`;
    element.style.width = box.widthStyle ?? `${box.width}px`;
    element.style.height = box.heightStyle ?? `${box.height}px`;

    // No declared, remembered or measured box outranks the screen it landed on
    // — a phone may be the primary screen (placement.ts, clampToViewport).
    element.style.maxWidth = `${Math.floor(window.innerWidth * MAX_VIEWPORT_WIDTH_RATIO)}px`;
    element.style.maxHeight = `${Math.floor(window.innerHeight * MAX_VIEWPORT_HEIGHT_RATIO)}px`;

    element.style.borderRadius = getWindowBorderRadius();
    element.style.boxShadow = WINDOW_BOX_SHADOW;

    // A window holds a title bar above a body that clips.
    element.style.display = 'flex';
    element.style.flexDirection = 'column';
    element.style.overflow = 'hidden';

    // How wide this window is when nothing is squeezing it, so a drag against
    // an edge knows what it is giving way from.
    setNaturalWidth(element, box.width);

    // In front on arrival, and in front again whenever it is touched.
    raise(element);
    raiseOnInteract(element);
}
