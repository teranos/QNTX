/**
 * Touch browse — hold thumb near tray, slide to browse, release to open.
 *
 * Separated from run.ts because touch interaction is a self-contained
 * concern with its own state (suppressNextClick, activation zone).
 */

import { getLogger, getLogSegment } from './config';
import type { GlyphProximity } from './proximity';
import type { Glyph } from './glyph';

// How close to the tray's edge the touch must land (px)
const TOUCH_ACTIVATION_MARGIN = 44;

// Minimum proximity factor to count as "thumb was on this glyph"
const MIN_PROXIMITY_THRESHOLD = 0.3;

export interface TouchBrowseHost {
    readonly element: HTMLElement | null;
    readonly indicatorContainer: HTMLElement | null;
    readonly proximity: GlyphProximity;
    readonly items: Map<string, Glyph>;
    updateProximity(): void;
    morphGlyph(element: HTMLElement, item: Glyph): void;
}

/**
 * Find the glyph dot with the highest proximity factor.
 * Returns the element and its Glyph data, or null if nothing is close enough.
 */
export function findPeakedGlyph(host: TouchBrowseHost): { element: HTMLElement; item: Glyph } | null {
    if (!host.indicatorContainer) return null;

    const glyphs = Array.from(
        host.indicatorContainer.querySelectorAll('.glyph-run-glyph')
    ) as HTMLElement[];

    let bestProximity = 0;
    let bestElement: HTMLElement | null = null;

    glyphs.forEach((glyph) => {
        const { proximityRaw } = host.proximity.calculateProximity(glyph);
        if (proximityRaw > bestProximity) {
            bestProximity = proximityRaw;
            bestElement = glyph;
        }
    });

    if (!bestElement || bestProximity < MIN_PROXIMITY_THRESHOLD) {
        return null;
    }

    const glyphId = (bestElement as HTMLElement).dataset.glyphId ?? '';
    const item = host.items.get(glyphId);
    if (!item) return null;

    return { element: bestElement, item };
}

/**
 * Set up touch browse on the document.
 *
 * touchstart near the tray edge enters browse mode.
 * touchmove slides through glyphs — proximity morphing shows labels.
 * touchend opens the glyph with highest proximity.
 *
 * Suppresses the synthetic click that would otherwise fire on the 8px dot.
 */
export function setupTouchBrowse(host: TouchBrowseHost): void {
    const log = getLogger();
    const seg = getLogSegment();

    let suppressNextClick = false;

    document.addEventListener('touchstart', (e) => {
        if (!host.element || !host.indicatorContainer) return;
        if (host.items.size === 0) return;

        const touch = e.touches[0];
        if (!touch) return;

        const trayRect = host.element.getBoundingClientRect();
        const withinX = touch.clientX >= trayRect.left - TOUCH_ACTIVATION_MARGIN
                     && touch.clientX <= trayRect.right + TOUCH_ACTIVATION_MARGIN;
        const withinY = touch.clientY >= trayRect.top - TOUCH_ACTIVATION_MARGIN
                     && touch.clientY <= trayRect.bottom + TOUCH_ACTIVATION_MARGIN;

        if (!withinX || !withinY) return;

        e.preventDefault();
        host.proximity.isTouchBrowsing = true;
        host.proximity.setPointerPosition(touch.clientX, touch.clientY);
        host.updateProximity();

        log.debug(seg, `[GlyphRun] Touch browse started at ${touch.clientX},${touch.clientY} with ${host.items.size} glyphs`);
    }, { passive: false });

    document.addEventListener('touchmove', (e) => {
        if (!host.proximity.isTouchBrowsing) return;

        const touch = e.touches[0];
        if (!touch) return;

        e.preventDefault();
        host.proximity.setPointerPosition(touch.clientX, touch.clientY);
        host.updateProximity();
    }, { passive: false });

    document.addEventListener('touchend', () => {
        if (!host.proximity.isTouchBrowsing) return;

        host.proximity.isTouchBrowsing = false;

        const peaked = findPeakedGlyph(host);

        host.proximity.setPointerPosition(-9999, -9999);
        host.updateProximity();

        if (peaked) {
            suppressNextClick = true;
            log.debug(seg, `[GlyphRun] Touch browse selected ${peaked.item.id}`);
            host.morphGlyph(peaked.element, peaked.item);
        } else {
            log.debug(seg, '[GlyphRun] Touch browse ended with no selection');
        }
    });

    // Suppress synthetic click after touch browse.
    // Capture phase so we catch it before the glyph's own click handler.
    document.addEventListener('click', (e) => {
        if (!suppressNextClick) return;
        suppressNextClick = false;

        const target = e.target as HTMLElement;
        if (target.closest('.glyph-run-glyph')) {
            e.stopPropagation();
            e.preventDefault();
            log.debug(seg, '[GlyphRun] Suppressed post-browse synthetic click');
        }
    }, { capture: true });
}
