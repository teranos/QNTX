/**
 * Window Manifestation - Traditional window with chrome
 *
 * The window manifestation morphs a glyph into a draggable window with:
 * - Title bar
 * - Minimize/close buttons
 * - Resizable content area
 * - Window chrome (borders, shadow, padding)
 */

import { getLogger, getLogSegment } from '../config';
import { type Glyph, DEFAULT_GLYPH_COLOR, DEFAULT_GLYPH_TEXT_COLOR } from '../glyph';
import { addWindowControls } from './title-bar-controls';
import { stashContent } from './stash';
import { renderGlyphContent } from './render-content';
import { setupWindowDrag, teardownWindowDrag } from '../window-drag';
import {
    getLastPosition,
    setLastPosition,
} from '../dataset';
import { prepareMorphTo, calculateTrayTarget, resetGlyphElement } from './morphology';
import { beginMaximizeMorph, beginMinimizeMorph } from '../morph-transaction';
import {
    getMaximizeDuration,
    getMinimizeDuration,
    WINDOW_BORDER_RADIUS,
    WINDOW_BOX_SHADOW,
    TITLE_BAR_HEIGHT,
    CANVAS_GLYPH_CONTENT_PADDING,
} from '../glyph';

/**
 * Morph a glyph to window with chrome (title bar, buttons)
 */
export function morphToWindow(
    glyphElement: HTMLElement,
    glyph: Glyph,
    verifyElement: (id: string, element: HTMLElement) => void,
    onRemove: (id: string) => void,
    onMinimize: (element: HTMLElement, glyph: Glyph) => void
): void {
    const log = getLogger();
    const seg = getLogSegment();
    const glyphRect = prepareMorphTo(glyphElement, glyph, verifyElement, 'glyph-morphing-to-window', '1000');

    // Size ownership per axis:
    //   initialWidth set  → window owns width  (explicit px, content clips/scrolls)
    //   initialWidth unset → content owns width (`fit-content`, window wraps)
    // Same for height. Pre-render + measure the content when either axis is
    // content-owned so the morph animation targets the final box directly
    // (no post-animation resize flash).
    const widthOwnedByWindow = glyph.initialWidth != null;
    const heightOwnedByWindow = glyph.initialHeight != null;

    let preRenderedContent: HTMLElement | null = null;
    let measuredWidth = 0;
    let measuredHeight = 0;
    if (!widthOwnedByWindow || !heightOwnedByWindow) {
        preRenderedContent = glyph.renderContent();
        const measurer = document.createElement('div');
        measurer.style.position = 'fixed';
        measurer.style.left = '-99999px';
        measurer.style.top = '0';
        measurer.style.visibility = 'hidden';
        measurer.style.padding = `${CANVAS_GLYPH_CONTENT_PADDING}px`;
        measurer.appendChild(preRenderedContent);
        document.body.appendChild(measurer);
        measuredWidth = measurer.scrollWidth;
        measuredHeight = measurer.scrollHeight;
        // Detach content from measurer so it can be reparented into the window
        // without being torn down (subscriptions/timers keep firing).
        measurer.removeChild(preRenderedContent);
        document.body.removeChild(measurer);
    }

    const titleBarHeight = parseInt(TITLE_BAR_HEIGHT);
    const windowWidth = widthOwnedByWindow
        ? parseInt(glyph.initialWidth!)
        : measuredWidth;
    const windowHeight = heightOwnedByWindow
        ? parseInt(glyph.initialHeight!)
        : measuredHeight + titleBarHeight;

    // Check if we have a remembered position on the element
    const rememberedPos = getLastPosition(glyphElement);

    // Use remembered position, or default position, or center
    const targetX = rememberedPos?.x ??
                   (glyph.defaultX ?? (window.innerWidth - windowWidth) / 2);
    const targetY = rememberedPos?.y ??
                   (glyph.defaultY ?? (window.innerHeight - windowHeight) / 2);

    // BEGIN TRANSACTION: Start the morph animation
    beginMaximizeMorph(
        glyphElement,
        glyphRect,
        { x: targetX, y: targetY, width: windowWidth, height: windowHeight },
        getMaximizeDuration()
    ).then(() => {
        // COMMIT PHASE: Animation completed successfully
        log.debug(seg, `[Window] Animation committed for ${glyph.id}`);

        // Apply final window state — per-axis size ownership committed here.
        glyphElement.style.position = 'fixed';
        glyphElement.style.left = `${targetX}px`;
        glyphElement.style.top = `${targetY}px`;
        glyphElement.style.width = widthOwnedByWindow ? `${windowWidth}px` : 'fit-content';
        glyphElement.style.height = heightOwnedByWindow ? `${windowHeight}px` : 'fit-content';
        glyphElement.style.borderRadius = WINDOW_BORDER_RADIUS;
        glyphElement.style.backgroundColor = glyph.color ?? DEFAULT_GLYPH_COLOR;
        glyphElement.style.backdropFilter = 'blur(2px)';
        glyphElement.style.boxShadow = WINDOW_BOX_SHADOW;
        glyphElement.style.padding = '0';
        glyphElement.style.opacity = '1';
        glyphElement.style.color = glyph.textColor ?? DEFAULT_GLYPH_TEXT_COLOR;

        // Set up window as flex container
        glyphElement.style.display = 'flex';
        glyphElement.style.flexDirection = 'column';
        glyphElement.style.overflow = 'hidden';

        // Restore stashed content or render fresh (shared with panel.ts).
        // preRenderedContent is populated when we measured for fit-content
        // sizing above; passing it in avoids a second renderContent() call.
        const { titleBar } = renderGlyphContent(
            glyphElement,
            glyph,
            'Window',
            preRenderedContent ?? undefined,
        );

        // Add window controls (minimize/close) to the title bar
        addWindowControls(titleBar, {
            onMinimize: () => morphFromWindow(glyphElement, glyph, verifyElement, onMinimize),
            onClose: glyph.onClose ? () => {
                teardownWindowDrag(glyphElement);
                onRemove(glyph.id);
                glyphElement.remove();
                try {
                    glyph.onClose!();
                } catch (error) {
                    log.error(seg, `[Window ${glyph.id}] Error in onClose callback: ${error instanceof Error ? error.message : String(error)}`);
                }
            } : undefined,
        });

        // Width/height are owned per-axis (see morphToWindow prologue).
        // No ResizeObserver — `fit-content` handles growth/shrink naturally
        // when content owns the axis; explicit px handles the window-owned axis.

        // Make window draggable
        setupWindowDrag(glyphElement, titleBar);
    }).catch(error => {
        // ROLLBACK: Animation was cancelled or failed
        log.warn(seg, `[Window] Animation failed for ${glyph.id}: ${error instanceof Error ? error.message : String(error)}`);
        // Element stays in glyph state, can retry
    });
}

/**
 * Morph a window back into a glyph (dot)
 * THE SAME ELEMENT morphs back - no new elements created
 */
export function morphFromWindow(
    windowElement: HTMLElement,
    glyph: Glyph,
    verifyElement: (id: string, element: HTMLElement) => void,
    onMorphComplete: (element: HTMLElement, glyph: Glyph) => void
): void {
    const log = getLogger();
    const seg = getLogSegment();
    verifyElement(glyph.id, windowElement);
    log.debug(seg, `[Window] Minimizing ${glyph.id}`);

    // Get current window state before clearing anything
    const currentRect = windowElement.getBoundingClientRect();

    // Remember window position for next time it opens
    setLastPosition(windowElement, currentRect.left, currentRect.top);

    // Tear down window drag handlers before stashing (prevents handler accumulation)
    teardownWindowDrag(windowElement);

    // Stash content (strips window controls, preserves glyph identity off-DOM)
    stashContent(windowElement);

    const trayTarget = calculateTrayTarget(glyph.id);

    beginMinimizeMorph(windowElement, currentRect, trayTarget, getMinimizeDuration())
        .then(() => {
            resetGlyphElement(windowElement, glyph, 'Window', onMorphComplete);
        })
        .catch(error => {
            // Animation was cancelled or failed
            log.warn(seg, `[Window] Animation failed for ${glyph.id}: ${error instanceof Error ? error.message : String(error)}`);
            // Element stays in window state, can retry
        });
}

