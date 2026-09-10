/**
 * What a glyph is painted, carried across a change of manifestation.
 *
 * A glyph is exactly one DOM element for its entire lifetime, and everything
 * about it survives every transition (Element Axioma). Inline styles are wiped
 * on a morph because geometry and layout belong to the manifestation — a tray
 * dot is not laid out like a canvas frame. Paint does not belong to the
 * manifestation. A glyph is the colour it is in the tray, in a window and on
 * the canvas.
 *
 * So it is read off the element before the wipe and worn again after. The Glyph
 * datum is not where the colour lives: a datum that names one is asking to
 * change it, and one that names none is not asking for the default.
 */

import { DEFAULT_GLYPH_COLOR } from './glyph';

/** What an element is wearing now. */
export interface Paint {
    background: string;
    border: string;
}

/** Read the paint off an element, before anything wipes it. */
export function readPaint(element: HTMLElement): Paint {
    return {
        background: element.style.backgroundColor,
        border: element.style.border,
    };
}

/**
 * Put the paint back on, after the wipe.
 *
 * asked is what the caller wants changed, and wins where it says anything. What
 * the element wore is next, and the default is for an element that has never
 * been painted at all — one born straight into the tray.
 */
export function wearPaint(
    element: HTMLElement,
    was: Paint,
    asked?: { color?: string; border?: string },
): void {
    element.style.backgroundColor = asked?.color || was.background || DEFAULT_GLYPH_COLOR;

    const border = asked?.border || was.border;
    if (border) element.style.border = border;
}
