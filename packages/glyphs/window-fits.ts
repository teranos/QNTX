/**
 * Whether a glyph has the room to be a window.
 *
 * A window is a box with screen around it: something you place, drag and put
 * beside another one. Where the content wants more room than a window may take,
 * the glyph is a panel and takes the whole screen.
 *
 * On a phone that is almost every glyph, which is why one rarely manifests to
 * a window there.
 */

import { MAX_VIEWPORT_WIDTH_RATIO } from './glyph';

/**
 * `contentWidth` is what the glyph's content measured. Zero is a glyph that
 * was never measured, and nothing about it asks for the screen.
 */
export function fitsAsWindow(contentWidth: number, viewport: number): boolean {
    if (contentWidth <= 0) return true;
    return contentWidth <= viewport * MAX_VIEWPORT_WIDTH_RATIO;
}
