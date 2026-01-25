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
import { stripHtml } from '../../../html-utils';
import type { Glyph } from '../glyph';
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
    TITLE_BAR_PADDING,
    WINDOW_BUTTON_SIZE,
    CONTENT_PADDING
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
        log.debug(SEG.UI, `[Window] Animation committed for ${glyph.id}`);

        // Apply final window state
        glyphElement.style.position = 'fixed';
        glyphElement.style.left = `${targetX}px`;
        glyphElement.style.top = `${targetY}px`;
        glyphElement.style.width = `${windowWidth}px`;
        glyphElement.style.height = `${windowHeight}px`;
        glyphElement.style.borderRadius = WINDOW_BORDER_RADIUS;
        glyphElement.style.backgroundColor = 'var(--bg-primary)';
        glyphElement.style.boxShadow = WINDOW_BOX_SHADOW;
        glyphElement.style.padding = '0';
        glyphElement.style.opacity = '1';

        // Set up window as flex container
        glyphElement.style.display = 'flex';
        glyphElement.style.flexDirection = 'column';

        // Add window chrome (title bar, controls)
        const titleBar = document.createElement('div');
        titleBar.className = 'window-title-bar';
        titleBar.style.height = TITLE_BAR_HEIGHT;
        titleBar.style.backgroundColor = 'var(--bg-secondary)';
        titleBar.style.borderBottom = '1px solid var(--border-color)';
        titleBar.style.display = 'flex';
        titleBar.style.alignItems = 'center';
        titleBar.style.padding = TITLE_BAR_PADDING;
        titleBar.style.flexShrink = '0'; // Prevent title bar from shrinking

        // Add title
        const titleText = document.createElement('span');
        titleText.textContent = stripHtml(glyph.title);
        titleText.style.flex = '1';
        titleBar.appendChild(titleText);

        // Add minimize button
        const minimizeBtn = document.createElement('button');
        minimizeBtn.textContent = '−';
        minimizeBtn.style.width = WINDOW_BUTTON_SIZE;
        minimizeBtn.style.height = WINDOW_BUTTON_SIZE;
        minimizeBtn.style.border = 'none';
        minimizeBtn.style.background = 'transparent';
        minimizeBtn.style.cursor = 'pointer';
        minimizeBtn.onclick = () => morphFromWindow(
            glyphElement,
            glyph,
            verifyElement,
            onMinimize
        );
        titleBar.appendChild(minimizeBtn);

        // Add close button if glyph has onClose
        if (glyph.onClose) {
            const closeBtn = document.createElement('button');
            closeBtn.textContent = '×';
            closeBtn.style.width = WINDOW_BUTTON_SIZE;
            closeBtn.style.height = WINDOW_BUTTON_SIZE;
            closeBtn.style.border = 'none';
            closeBtn.style.background = 'transparent';
            closeBtn.style.cursor = 'pointer';
            closeBtn.onclick = () => {
                // Remove from tray data AND remove element
                onRemove(glyph.id);
                glyphElement.remove();
                // Call onClose in try-catch (cleanup already done, so safe if it fails)
                try {
                    glyph.onClose!();
                } catch (error) {
                    log.error(SEG.UI, `[Window ${glyph.id}] Error in onClose callback:`, error);
                }
            };
            titleBar.appendChild(closeBtn);
        }

        glyphElement.appendChild(titleBar);

        // Add content area with error boundary
        try {
            const content = glyph.renderContent();
            content.style.padding = CONTENT_PADDING;
            content.style.flex = '1'; // Take remaining space in flex container
            content.style.overflow = 'auto';
            glyphElement.appendChild(content);
        } catch (error) {
            // Show error UI if renderContent fails
            log.error(SEG.UI, `[Window ${glyph.id}] Error rendering content:`, error);
            const errorContent = document.createElement('div');
            errorContent.style.padding = CONTENT_PADDING;
            errorContent.style.flex = '1';
            errorContent.style.overflow = 'auto';
            errorContent.style.color = '#ef4444'; // Red error text
            errorContent.style.fontFamily = 'var(--font-mono)';
            errorContent.innerHTML = `
                    <div style="margin-bottom: 8px; font-weight: bold;">Error rendering content</div>
                    <div style="opacity: 0.8; font-size: 12px;">${error instanceof Error ? error.message : String(error)}</div>
                `;
            glyphElement.appendChild(errorContent);
        }

        // Make window draggable
        makeWindowDraggable(glyphElement, titleBar);
    }).catch(error => {
        // ROLLBACK: Animation was cancelled or failed
        log.warn(SEG.UI, `[Window] Animation failed for ${glyph.id}:`, error);
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
    log.debug(SEG.UI, `[Window] Minimizing ${glyph.id}`);

    // Get current window state before clearing anything
    const currentRect = windowElement.getBoundingClientRect();

    // Remember window position for next time it opens
    setLastPosition(windowElement, currentRect.left, currentRect.top);

    // Clear window content immediately for visual feedback
    windowElement.innerHTML = '';
    windowElement.textContent = '';

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
            log.debug(SEG.UI, `[Window] Animation complete for ${glyph.id}`);

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
            log.debug(SEG.UI, `[Window] Re-attaching to indicator container`);
            onMorphComplete(windowElement, glyph);
        })
        .catch(error => {
            // Animation was cancelled or failed
            log.warn(SEG.UI, `[Window] Animation failed for ${glyph.id}:`, error);
            // Element stays in window state, can retry
        });
}

/**
 * Make a window draggable by its title bar
 */
function makeWindowDraggable(windowElement: HTMLElement, handle: HTMLElement): void {
    let isDragging = false;
    let offsetX = 0;
    let offsetY = 0;

    const startDrag = (e: MouseEvent | TouchEvent) => {
        // Handle both mouse and touch/pen input
        const clientX = e instanceof MouseEvent ? e.clientX : e.touches[0]?.clientX;
        const clientY = e instanceof MouseEvent ? e.clientY : e.touches[0]?.clientY;

        if (!clientX || !clientY) return;

        isDragging = true;

        // Calculate offset from pointer to window top-left
        const rect = windowElement.getBoundingClientRect();
        offsetX = clientX - rect.left;
        offsetY = clientY - rect.top;

        // Prevent text selection while dragging
        e.preventDefault();

        // Add cursor style
        document.body.style.cursor = 'move';

        // Move handlers to window for better capture
        window.addEventListener('mousemove', drag);
        window.addEventListener('mouseup', stopDrag);
        window.addEventListener('touchmove', drag, { passive: false });
        window.addEventListener('touchend', stopDrag);
        window.addEventListener('keydown', cancelOnEscape);
    };

    const drag = (e: MouseEvent | TouchEvent) => {
        if (!isDragging) return;

        // Handle both mouse and touch input
        const clientX = e instanceof MouseEvent ? e.clientX : e.touches[0]?.clientX;
        const clientY = e instanceof MouseEvent ? e.clientY : e.touches[0]?.clientY;

        if (!clientX || !clientY) return;

        // Calculate new position
        let newX = clientX - offsetX;
        let newY = clientY - offsetY;

        // Clamp to viewport bounds (keep at least 50px visible)
        const rect = windowElement.getBoundingClientRect();
        const minVisibleArea = 50;

        // Clamp X position
        newX = Math.max(-rect.width + minVisibleArea, newX);
        newX = Math.min(window.innerWidth - minVisibleArea, newX);

        // Clamp Y position (keep title bar visible)
        newY = Math.max(0, newY);
        newY = Math.min(window.innerHeight - minVisibleArea, newY);

        windowElement.style.left = `${newX}px`;
        windowElement.style.top = `${newY}px`;
    };

    const stopDrag = () => {
        if (!isDragging) return;
        isDragging = false;

        // Reset cursor
        document.body.style.cursor = '';

        // Save final position for next time window opens
        const finalRect = windowElement.getBoundingClientRect();
        setLastPosition(windowElement, finalRect.left, finalRect.top);

        // Remove all event handlers
        window.removeEventListener('mousemove', drag);
        window.removeEventListener('mouseup', stopDrag);
        window.removeEventListener('touchmove', drag);
        window.removeEventListener('touchend', stopDrag);
        window.removeEventListener('keydown', cancelOnEscape);
    };

    const cancelOnEscape = (e: KeyboardEvent) => {
        if (e.key === 'Escape' && isDragging) {
            stopDrag();
        }
    };

    // Add both mouse and touch/pen event handlers
    handle.addEventListener('mousedown', startDrag);
    handle.addEventListener('touchstart', startDrag, { passive: false });
}
