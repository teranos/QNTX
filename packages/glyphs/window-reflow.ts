/**
 * Where a dragged glyph sits and how wide it is allowed to be.
 *
 * A fixed box positioned from the left takes its available width from the left
 * alone, so `fit-content` only ever felt the right edge of the screen. Asking
 * both edges is what makes a glyph give way at either one.
 */

/** The smallest a window goes: less than this is not something to take hold of. */
export const MIN_WIDTH = 120;

export interface Box {
    left: number;
    width: number;
}

/**
 * Fit a window of its natural width at the place it was dragged to.
 *
 * The natural width is asked with every time rather than the current one, so a
 * window that gave way at an edge is its whole self again once it leaves.
 */
export function reflowBox(desiredLeft: number, naturalWidth: number, viewport: number): Box {
    const wanted = Math.min(naturalWidth, viewport);

    let left = desiredLeft;
    let right = desiredLeft + wanted;

    if (left < 0) left = 0;
    if (right > viewport) right = viewport;

    let width = right - left;
    if (width < MIN_WIDTH) {
        width = Math.min(MIN_WIDTH, viewport);
        // Pushed so far that the minimum no longer fits where it was asked for:
        // it keeps the width and gives up the position instead.
        if (left + width > viewport) left = viewport - width;
        if (left < 0) left = 0;
    }
    return { left, width };
}
