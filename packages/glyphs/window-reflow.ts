/**
 * Where a dragged glyph sits and how wide it is.
 *
 * A glyph gives way at the edge it is pushed against, either one. Both edges
 * are asked, because a fixed box measures its own room from the left alone.
 */

/** The smallest a window goes, and still something you can take hold of. */
export const MIN_WIDTH = 120;

export interface Box {
    left: number;
    width: number;
}

/**
 * Fit a window of its natural width at the place it was dragged to.
 *
 * `floor` is the width the content of this glyph needs. A window gives way at
 * an edge down to that and then stops giving, because narrower than what it
 * holds is a window that has stopped showing it.
 *
 * The natural width is what is asked with every time, so a window that gave way
 * at an edge is its whole self again once it leaves.
 */
export function reflowBox(
    desiredLeft: number,
    naturalWidth: number,
    viewport: number,
    floor: number = MIN_WIDTH,
): Box {
    const wanted = Math.min(naturalWidth, viewport);
    const least = Math.min(Math.max(floor, MIN_WIDTH), viewport);

    let left = desiredLeft;
    let right = desiredLeft + wanted;

    if (left < 0) left = 0;
    if (right > viewport) right = viewport;

    let width = right - left;
    if (width < least) {
        width = least;
        // Pushed past what the content needs: the width holds and the position
        // gives instead, so the window slides rather than crushing what is in it.
        if (left + width > viewport) left = viewport - width;
        if (left < 0) left = 0;
    }
    return { left, width };
}
