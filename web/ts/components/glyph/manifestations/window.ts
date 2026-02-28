/**
 * Window Manifestation - Traditional window with chrome
 *
 * The window manifestation morphs a glyph into a draggable window with:
 * - Title bar
 * - Minimize/close buttons
 * - Resizable content area
 * - Window chrome (borders, shadow, padding)
 */

import { log, SEG } from '../../../logger';
import type { Glyph } from '../glyph';
import { addWindowControls } from './title-bar-controls';
import { stashContent } from './stash';
import { renderGlyphContent } from './render-content';
import { setupWindowDrag, teardownWindowDrag } from './canvas-window';
import {
    setWindowState,
    getLastPosition,
    setLastPosition,
    hasProximityText,
    setProximityText,
    setGlyphId
} from '../dataset';
import { beginMaximizeMorph, beginMinimizeMorph } from '../morph-transaction';
import {
    getMaximizeDuration,
    getMinimizeDuration,
    DEFAULT_WINDOW_WIDTH,
    DEFAULT_WINDOW_HEIGHT,
    WINDOW_BORDER_RADIUS,
    WINDOW_BOX_SHADOW,
    TITLE_BAR_HEIGHT,
    CANVAS_GLYPH_CONTENT_PADDING,
    GLYPH_CONTENT_INNER_PADDING,
    MAX_VIEWPORT_HEIGHT_RATIO,
    MAX_VIEWPORT_WIDTH_RATIO,
    MIN_WINDOW_HEIGHT,
    MIN_WINDOW_WIDTH
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
    // AXIOM CHECK: Verify this is the correct element
    verifyElement(glyph.id, glyphElement);

    // Verify no duplicates exist
    const elements = document.querySelectorAll(`[data-glyph-id="${glyph.id}"]`);
    if (elements.length !== 1) {
        throw new Error(
            `AXIOM VIOLATION: Expected exactly 1 element for ${glyph.id}, found ${elements.length}`
        );
    }

    // Get current glyph position and size (may be proximity-expanded)
    const glyphRect = glyphElement.getBoundingClientRect();

    // Calculate window target position
    const windowWidth = parseInt(glyph.initialWidth || DEFAULT_WINDOW_WIDTH);
    const windowHeight = parseInt(glyph.initialHeight || DEFAULT_WINDOW_HEIGHT);

    // Check if we have a remembered position on the element
    const rememberedPos = getLastPosition(glyphElement);

    // Use remembered position, or default position, or center
    const targetX = rememberedPos?.x ??
                   (glyph.defaultX ?? (window.innerWidth - windowWidth) / 2);
    const targetY = rememberedPos?.y ??
                   (glyph.defaultY ?? (window.innerHeight - windowHeight) / 2);

    // THE GLYPH ITSELF BECOMES THE WINDOW - NO CLONING
    // Remove from indicator container and reparent to body
    glyphElement.remove(); // Detach from current parent (keeps element alive)

    // Clear any proximity text that might be present AFTER detaching
    if (hasProximityText(glyphElement)) {
        glyphElement.textContent = '';
        setProximityText(glyphElement, false);
    }

    // Apply initial fixed positioning at EXACT current state (including proximity expansion)
    glyphElement.className = 'glyph-morphing-to-window';
    glyphElement.style.position = 'fixed';
    glyphElement.style.zIndex = '1000';

    // Reparent to document body for morphing
    document.body.appendChild(glyphElement);

    // Mark element as in-window-state (but keep glyph ID)
    setWindowState(glyphElement, true);

    // BEGIN TRANSACTION: Start the morph animation
    beginMaximizeMorph(
        glyphElement,
        glyphRect,
        { x: targetX, y: targetY, width: windowWidth, height: windowHeight },
        getMaximizeDuration()
    ).then(() => {
        // COMMIT PHASE: Animation completed successfully
        log.debug(SEG.GLYPH, `[Window] Animation committed for ${glyph.id}`);

        // Apply final window state
        glyphElement.style.position = 'fixed';
        glyphElement.style.left = `${targetX}px`;
        glyphElement.style.top = `${targetY}px`;
        glyphElement.style.width = `${windowWidth}px`;
        glyphElement.style.height = `${windowHeight}px`;
        glyphElement.style.borderRadius = WINDOW_BORDER_RADIUS;
        glyphElement.style.backgroundColor = 'var(--bg-almost-black)';
        glyphElement.style.boxShadow = WINDOW_BOX_SHADOW;
        glyphElement.style.padding = '0';
        glyphElement.style.opacity = '1';
        glyphElement.style.color = 'var(--text-on-dark)';

        // Set up window as flex container
        glyphElement.style.display = 'flex';
        glyphElement.style.flexDirection = 'column';
        glyphElement.style.overflow = 'hidden';

        // Restore stashed content or render fresh (shared with panel.ts)
        const { titleBar, contentElement } = renderGlyphContent(glyphElement, glyph, 'Window');

        // Add window controls (minimize/close) to the title bar
        addWindowControls(titleBar, {
            onMinimize: () => morphFromWindow(glyphElement, glyph, verifyElement, onMinimize),
            onClose: glyph.onClose ? () => {
                onRemove(glyph.id);
                glyphElement.remove();
                try {
                    glyph.onClose!();
                } catch (error) {
                    log.error(SEG.GLYPH, `[Window ${glyph.id}] Error in onClose callback:`, error);
                }
            } : undefined,
        });

        // Set up ResizeObserver for auto-sizing window to content
        if (contentElement) {
            setupWindowResizeObserver(glyphElement, contentElement, glyph.id);
        }

        // Make window draggable (uses same system as canvas-window.ts for compatibility)
        setupWindowDrag(glyphElement, titleBar);
    }).catch(error => {
        // ROLLBACK: Animation was cancelled or failed
        log.warn(SEG.GLYPH, `[Window] Animation failed for ${glyph.id}:`, error);
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
    // AXIOM CHECK: Verify this is the correct element
    verifyElement(glyph.id, windowElement);
    log.debug(SEG.GLYPH, `[Window] Minimizing ${glyph.id}`);

    // Get current window state before clearing anything
    const currentRect = windowElement.getBoundingClientRect();

    // Remember window position for next time it opens
    setLastPosition(windowElement, currentRect.left, currentRect.top);

    // Tear down window drag handlers before stashing (prevents handler accumulation)
    teardownWindowDrag(windowElement);

    // Stash content (strips window controls, preserves glyph identity off-DOM)
    stashContent(windowElement);

    // Calculate target position for the dot
    // The glyph will go to the right side of the tray
    const trayElement = document.querySelector('.glyph-run');
    let targetX = window.innerWidth - 50; // Default to right side if no tray
    let targetY = window.innerHeight / 2;

    if (trayElement) {
        const trayRect = trayElement.getBoundingClientRect();
        // Position at the right edge of the tray, centered vertically
        targetX = trayRect.right - 20; // A bit inset from the edge
        targetY = trayRect.top + trayRect.height / 2;
    }

    // Begin the minimize morph animation
    // Element stays fixed on body during animation
    beginMinimizeMorph(windowElement, currentRect, { x: targetX, y: targetY }, getMinimizeDuration())
        .then(() => {
            // Animation completed successfully
            log.debug(SEG.GLYPH, `[Window] Animation complete for ${glyph.id}`);

            // Now reparent the element to the indicator container
            // Clear state flags
            setWindowState(windowElement, false);
            setProximityText(windowElement, false);

            // Remove from body
            windowElement.remove();

            // Clear all inline styles
            windowElement.style.cssText = '';

            // Apply glyph class
            windowElement.className = 'glyph-run-glyph';

            // Keep the glyph ID
            setGlyphId(windowElement, glyph.id);

            // Re-attach to indicator container
            log.debug(SEG.GLYPH, `[Window] Re-attaching to indicator container`);
            onMorphComplete(windowElement, glyph);
        })
        .catch(error => {
            // Animation was cancelled or failed
            log.warn(SEG.GLYPH, `[Window] Animation failed for ${glyph.id}:`, error);
            // Element stays in window state, can retry
        });
}

/**
 * Set up ResizeObserver to auto-size window to match content height
 * Observes the inner .glyph-content element which has intrinsic size
 */
function setupWindowResizeObserver(
    windowElement: HTMLElement,
    contentElement: HTMLElement,
    glyphId: string
): void {
    const titleBarHeight = parseInt(TITLE_BAR_HEIGHT);
    const maxHeight = window.innerHeight * MAX_VIEWPORT_HEIGHT_RATIO;
    const minHeight = MIN_WINDOW_HEIGHT;

    // Find the inner .glyph-content element which has intrinsic size
    // The contentElement itself has flex: 1 and doesn't report natural height
    const innerContent = contentElement.querySelector('.glyph-content, .glyph-loading') as HTMLElement;

    if (!innerContent) {
        log.warn(SEG.GLYPH, `[Window ${glyphId}] No .glyph-content element found for ResizeObserver`);
        return;
    }

    const maxWidth = window.innerWidth * MAX_VIEWPORT_WIDTH_RATIO;
    const minWidth = MIN_WINDOW_WIDTH;

    const resizeObserver = new ResizeObserver(entries => {
        for (const entry of entries) {
            const contentHeight = entry.contentRect.height;
            const contentWidth = entry.contentRect.width;

            // Skip if content hasn't rendered yet (height is 0)
            if (contentHeight === 0) {
                log.debug(SEG.GLYPH, `[Window ${glyphId}] Skipping resize - content height is 0`);
                return;
            }

            // Add padding for both layers:
            // - contentElement padding: CANVAS_GLYPH_CONTENT_PADDING
            // - .glyph-content padding: GLYPH_CONTENT_INNER_PADDING (CSS)
            // Total: (8 + 4) * 2 = 24px per dimension
            const contentElementPadding = CANVAS_GLYPH_CONTENT_PADDING * 2; // top + bottom OR left + right
            const glyphContentPadding = GLYPH_CONTENT_INNER_PADDING * 2; // top + bottom OR left + right
            const totalPadding = contentElementPadding + glyphContentPadding;

            const totalHeight = Math.max(minHeight, Math.min(contentHeight + titleBarHeight + totalPadding, maxHeight));
            const totalWidth = Math.max(minWidth, Math.min(contentWidth + totalPadding, maxWidth));

            windowElement.style.height = `${totalHeight}px`;
            windowElement.style.width = `${totalWidth}px`;

            log.debug(SEG.GLYPH, `[Window ${glyphId}] Auto-resized to ${totalWidth}x${totalHeight}px (content: ${contentWidth}x${contentHeight}px + padding: ${totalPadding}px)`);
        }
    });

    resizeObserver.observe(innerContent);

    // Store observer for cleanup on minimize/close
    (windowElement as any).__resizeObserver = resizeObserver;
}
