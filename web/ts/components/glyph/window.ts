/**
 * Glyph morphing transformations
 *
 * Handles the transformation of glyphs between states:
 * - Collapsed/Proximity → Window
 * - Window → Collapsed
 *
 * CRITICAL: The same DOM element morphs through all states.
 * We reparent and transform, but NEVER recreate the element.
 */

import { log, SEG } from '../../logger';
import { stripHtml } from '../../html-utils';
import {
    setWindowState,
    getLastPosition,
    setLastPosition,
    hasProximityText,
    setProximityText,
    setGlyphId
} from './dataset';
import {
    animateToWindow,
    animateToGlyph,
    cancelAnimation,
    waitForAnimation
} from './animation';

export interface Glyph {
    id: string;
    title: string;
    renderContent: () => HTMLElement;    // Function to render window content
    initialWidth?: string;                // Window width (e.g., "800px")
    initialHeight?: string;               // Window height (e.g., "600px")
    defaultX?: number;                    // Window default X position
    defaultY?: number;                    // Window default Y position
    onClose?: () => void;
}

// Animation durations and reduced motion handling moved to animation.ts

// Window dimensions
const DEFAULT_WINDOW_WIDTH = '800px';
const DEFAULT_WINDOW_HEIGHT = '600px';
const WINDOW_BORDER_RADIUS = '8px';
const WINDOW_BOX_SHADOW = '0 8px 32px rgba(0, 0, 0, 0.3)';

// Window chrome dimensions
const TITLE_BAR_HEIGHT = '32px';
const TITLE_BAR_PADDING = '0 12px';
const WINDOW_BUTTON_SIZE = '24px';
const CONTENT_PADDING = '16px';

// Dot dimensions - moved to animation.ts for Web Animations API

export class GlyphMorph {

    /**
     * Morph a glyph (dot) into a full window
     * The glyph DOM element itself transforms through animation
     */
    public morphToWindow(
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

        // Capture current computed styles to preserve proximity-expanded state
        const computedStyle = window.getComputedStyle(glyphElement);
        const currentWidth = glyphRect.width;
        const currentHeight = glyphRect.height;
        const currentBorderRadius = computedStyle.borderRadius;
        const currentBackgroundColor = computedStyle.backgroundColor;
        const currentOpacity = computedStyle.opacity;
        const currentPadding = computedStyle.padding;

        // Calculate window target position
        const windowWidth = glyph.initialWidth || DEFAULT_WINDOW_WIDTH;
        const windowHeight = glyph.initialHeight || DEFAULT_WINDOW_HEIGHT;
        const windowWidthPx = parseInt(windowWidth);
        const windowHeightPx = parseInt(windowHeight);

        // Check if we have a remembered position on the element
        const rememberedPos = getLastPosition(glyphElement);

        // Use remembered position, or default position, or center
        const targetX = rememberedPos?.x ??
                       (glyph.defaultX ?? (window.innerWidth - windowWidthPx) / 2);
        const targetY = rememberedPos?.y ??
                       (glyph.defaultY ?? (window.innerHeight - windowHeightPx) / 2);

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
        glyphElement.style.left = `${glyphRect.left}px`;
        glyphElement.style.top = `${glyphRect.top}px`;
        glyphElement.style.width = `${currentWidth}px`;
        glyphElement.style.height = `${currentHeight}px`;
        glyphElement.style.borderRadius = currentBorderRadius;
        glyphElement.style.backgroundColor = currentBackgroundColor;
        glyphElement.style.opacity = currentOpacity;
        glyphElement.style.padding = currentPadding;
        glyphElement.style.zIndex = '1000';

        // Reparent to document body for morphing
        document.body.appendChild(glyphElement);

        // Force a reflow to ensure initial styles are applied
        glyphElement.offsetHeight;

        // Mark element as in-window-state (but keep glyph ID)
        setWindowState(glyphElement, true);

        // Cancel any existing animations
        cancelAnimation(glyphElement);

        // Use Web Animations API for morphing
        const currentRect = glyphElement.getBoundingClientRect();
        const animation = animateToWindow(
            glyphElement,
            currentRect,
            {
                x: targetX,
                y: targetY,
                width: windowWidth,
                height: windowHeight
            }
        );

        // After animation completes, add window content
        waitForAnimation(animation).then(() => {
                // Commit the final animation state to the element
                // This is critical for dragging to work properly
                glyphElement.style.position = 'fixed';
                glyphElement.style.left = `${targetX}px`;
                glyphElement.style.top = `${targetY}px`;
                glyphElement.style.width = windowWidth;
                glyphElement.style.height = windowHeight;
                glyphElement.style.borderRadius = '8px';
                glyphElement.style.backgroundColor = 'var(--bg-black)';
                glyphElement.style.boxShadow = '0 8px 32px rgba(0,0,0,0.6)';
                glyphElement.style.border = '1px solid var(--border)';

                // Cancel the animation to remove its effects
                animation.cancel();

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
                minimizeBtn.onclick = () => this.morphToGlyph(
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
                            log.error(SEG.UI, `[Glyph ${glyph.id}] Error in onClose callback:`, error);
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
                    log.error(SEG.UI, `[Glyph ${glyph.id}] Error rendering content:`, error);
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
                this.makeWindowDraggable(glyphElement, titleBar);
        });
    }

    /**
     * Morph a window back into a glyph (dot)
     * THE SAME ELEMENT morphs back - no new elements created
     */
    public morphToGlyph(
        windowElement: HTMLElement,
        glyph: Glyph,
        verifyElement: (id: string, element: HTMLElement) => void,
        onMorphComplete: (element: HTMLElement, glyph: Glyph) => void
    ): void {
        // AXIOM CHECK: Verify this is the correct element
        verifyElement(glyph.id, windowElement);
        log.debug(SEG.UI, `[AXIOM CHECK] Minimizing same element for ${glyph.id}:`, windowElement);
        // Find the position where this glyph should go back to
        const targetRect = this.calculateGlyphTargetPosition();

        log.debug(SEG.UI, `[Minimize] Starting minimize for ${glyph.id}`);
        log.debug(SEG.UI, `[Minimize] Target position: x=${targetRect.x}, y=${targetRect.y}`);

        // Get current window state before clearing anything
        const currentRect = windowElement.getBoundingClientRect();
        log.debug(SEG.UI, `[Minimize] Current window position: x=${currentRect.left}, y=${currentRect.top}, w=${currentRect.width}, h=${currentRect.height}`);

        // Remember window position for next time it opens
        setLastPosition(windowElement, currentRect.left, currentRect.top);

        // Clear window state flag
        setWindowState(windowElement, false);

        // Clear window content but keep a visible background
        windowElement.innerHTML = '';
        windowElement.textContent = ''; // Ensure text is also cleared

        // Clear any proximity data attributes that might cause text to appear
        setProximityText(windowElement, false);

        // FORCE class change - remove old class first
        windowElement.classList.remove('glyph-morphing-to-window');
        windowElement.className = 'window-morphing-to-glyph';
        log.debug(SEG.UI, `[Minimize] Class changed to: ${windowElement.className}`);

        windowElement.style.position = 'fixed';
        windowElement.style.left = `${currentRect.left}px`;
        windowElement.style.top = `${currentRect.top}px`;
        windowElement.style.width = `${currentRect.width}px`;
        windowElement.style.height = `${currentRect.height}px`;
        windowElement.style.backgroundColor = 'var(--bg-primary)';  // Keep window background
        windowElement.style.borderRadius = WINDOW_BORDER_RADIUS;
        windowElement.style.boxShadow = WINDOW_BOX_SHADOW;
        windowElement.style.border = 'none';
        windowElement.style.padding = '0';
        windowElement.style.opacity = '1';
        windowElement.style.zIndex = '10000';
        // NO transition yet - we need initial state to be committed first

        // Force the browser to commit these styles BEFORE animating
        windowElement.offsetHeight;

        // Cancel any existing animations
        cancelAnimation(windowElement);

        // Use Web Animations API for minimize animation
        const animation = animateToGlyph(
            windowElement,
            currentRect,
            { x: targetRect.x, y: targetRect.y }
        );

        log.debug(SEG.UI, `[Minimize] Triggering animation to target position`);

        // Wait for animation to complete
        waitForAnimation(animation).then(() => {
                log.debug(SEG.UI, `[Minimize] Animation completed for ${glyph.id}`);

                // Commit the final dot state to the element
                windowElement.style.position = 'fixed';
                windowElement.style.left = `${targetRect.x}px`;
                windowElement.style.top = `${targetRect.y}px`;
                windowElement.style.width = '8px';
                windowElement.style.height = '8px';
                windowElement.style.borderRadius = '2px';
                windowElement.style.backgroundColor = 'rgb(153, 153, 153)';
                windowElement.style.boxShadow = 'none';
                windowElement.style.border = '1px solid rgba(255,255,255,0.1)';

                // Cancel the animation to remove its effects
                animation.cancel();

                // Verify element identity is preserved
                log.debug(SEG.UI, `[AXIOM CHECK] Same element after animation:`, windowElement);

                // Keep element visible but reset to glyph class
                windowElement.className = 'glyph-run-glyph';

                // Reset inline styles BUT keep position for now
                windowElement.style.width = '';
                windowElement.style.height = '';
                windowElement.style.borderRadius = '';
                windowElement.style.backgroundColor = '';
                windowElement.style.boxShadow = '';
                windowElement.style.padding = '';
                windowElement.style.border = '';
                windowElement.style.opacity = '';

                // Clear any text-related styles from proximity morphing
                windowElement.style.display = '';
                windowElement.style.alignItems = '';
                windowElement.style.justifyContent = '';
                windowElement.style.whiteSpace = '';
                windowElement.style.flexDirection = '';
                windowElement.textContent = ''; // Ensure no text remains

                // Keep the glyph ID
                setGlyphId(windowElement, glyph.id);

                // CRITICAL: Ensure windowState is cleared after animation
                setWindowState(windowElement, false);

                // CRITICAL: Clear hasText flag to prevent proximity text appearing
                setProximityText(windowElement, false);

                // Now remove from body and re-attach to indicator container (SAME ELEMENT)
                windowElement.remove(); // Detach but element stays alive

                // Clear position styles now that we're re-attaching
                windowElement.style.position = '';
                windowElement.style.left = '';
                windowElement.style.top = '';
                windowElement.style.zIndex = '';

                // Re-attach THE SAME ELEMENT to indicator container
                log.debug(SEG.UI, `[AXIOM CHECK] Re-attaching same element to tray:`, windowElement);
                onMorphComplete(windowElement, glyph);
        });
    }

    /**
     * Calculate where a glyph should be positioned in the tray
     */
    private calculateGlyphTargetPosition(): { x: number, y: number } {
        const tray = document.querySelector('.glyph-run');
        if (!tray) {
            log.debug(SEG.UI, `[Minimize] No tray found, using default position`);
            return { x: window.innerWidth - 12, y: window.innerHeight / 2 };
        }

        const indicatorContainer = tray.querySelector('.glyph-run-indicators');
        if (!indicatorContainer) {
            log.debug(SEG.UI, `[Minimize] No indicator container found`);
            return { x: window.innerWidth - 12, y: window.innerHeight / 2 };
        }

        const trayRect = indicatorContainer.getBoundingClientRect();
        const glyphSize = 8;
        const gap = 2;

        // Count existing glyphs in tray (not including the one being minimized which is on body)
        const glyphsInTray = Array.from(indicatorContainer.querySelectorAll('.glyph-run-glyph'));
        log.debug(SEG.UI, `[Minimize] Found ${glyphsInTray.length} glyphs already in tray`);

        // The minimizing glyph will be added at the end
        const index = glyphsInTray.length;

        // Stack vertically in the indicator container
        const x = trayRect.right - glyphSize;
        const y = trayRect.top + (index * (glyphSize + gap));

        log.debug(SEG.UI, `[Minimize] Calculated position for index ${index}: x=${x}, y=${y}`);
        log.debug(SEG.UI, `[Minimize] Tray rect:`, trayRect);

        return { x, y };
    }

    /**
     * Make a window draggable by its title bar
     */
    private makeWindowDraggable(windowElement: HTMLElement, handle: HTMLElement): void {
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

}
