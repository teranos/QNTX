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

// Animation durations in milliseconds - adjust these to slow down/speed up morphing
export const MAXIMIZE_DURATION_MS = 200;  // Duration for dot → window (e.g., 1000 for 1 second)
export const MINIMIZE_DURATION_MS = 200;  // Duration for window → dot (e.g., 400 for faster minimize)

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

// Dot dimensions
const DOT_SIZE = '8px';
const DOT_BORDER_RADIUS = '2px';
const DOT_BORDER = '1px solid var(--border-on-dark)';

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
        const windowWidth = parseInt(glyph.initialWidth || DEFAULT_WINDOW_WIDTH);
        const windowHeight = parseInt(glyph.initialHeight || DEFAULT_WINDOW_HEIGHT);

        // Check if we have a remembered position on the element
        const rememberedX = glyphElement.dataset.lastX;
        const rememberedY = glyphElement.dataset.lastY;

        // Use remembered position, or default position, or center
        const targetX = rememberedX ? parseFloat(rememberedX) :
                       (glyph.defaultX ?? (window.innerWidth - windowWidth) / 2);
        const targetY = rememberedY ? parseFloat(rememberedY) :
                       (glyph.defaultY ?? (window.innerHeight - windowHeight) / 2);

        // THE GLYPH ITSELF BECOMES THE WINDOW - NO CLONING
        // Remove from indicator container and reparent to body
        glyphElement.remove(); // Detach from current parent (keeps element alive)

        // Clear any proximity text that might be present AFTER detaching
        if (glyphElement.dataset.hasText) {
            glyphElement.textContent = '';
            delete glyphElement.dataset.hasText;
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

        // NOW set transition after element is positioned
        glyphElement.style.transition = `all ${MAXIMIZE_DURATION_MS}ms cubic-bezier(0.4, 0, 0.2, 1)`;

        // Mark element as in-window-state (but keep glyph ID)
        glyphElement.dataset.windowState = 'true';

        // Trigger morph animation after a frame to ensure initial styles are applied
        requestAnimationFrame(() => {
            // Apply window dimensions and position
            glyphElement.style.left = `${targetX}px`;
            glyphElement.style.top = `${targetY}px`;
            glyphElement.style.width = `${windowWidth}px`;
            glyphElement.style.height = `${windowHeight}px`;
            glyphElement.style.borderRadius = WINDOW_BORDER_RADIUS;
            glyphElement.style.backgroundColor = 'var(--bg-primary)';
            glyphElement.style.boxShadow = WINDOW_BOX_SHADOW;
            glyphElement.style.padding = '0'; // Reset padding to allow content to fill
            glyphElement.style.opacity = '1'; // Ensure it's visible

            // After animation completes, add window content
            setTimeout(() => {
                // CRITICAL: Remove transition after morphing completes to allow smooth dragging
                glyphElement.style.transition = '';

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
                titleText.textContent = this.stripHtml(glyph.title);
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
                            console.error(`[Glyph ${glyph.id}] Error in onClose callback:`, error);
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
                    console.error(`[Glyph ${glyph.id}] Error rendering content:`, error);
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
            }, MAXIMIZE_DURATION_MS); // Match maximize animation duration
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
        console.log(`[AXIOM CHECK] Minimizing same element for ${glyph.id}:`, windowElement);
        // Find the position where this glyph should go back to
        const targetRect = this.calculateGlyphTargetPosition();

        console.log(`[Minimize] Starting minimize for ${glyph.id}`);
        console.log(`[Minimize] Target position: x=${targetRect.x}, y=${targetRect.y}`);

        // Get current window state before clearing anything
        const currentRect = windowElement.getBoundingClientRect();
        console.log(`[Minimize] Current window position: x=${currentRect.left}, y=${currentRect.top}, w=${currentRect.width}, h=${currentRect.height}`);

        // Remember window position for next time it opens
        windowElement.dataset.lastX = String(currentRect.left);
        windowElement.dataset.lastY = String(currentRect.top);

        // Clear window state flag
        delete windowElement.dataset.windowState;

        // Clear window content but keep a visible background
        windowElement.innerHTML = '';
        windowElement.textContent = ''; // Ensure text is also cleared

        // Clear any proximity data attributes that might cause text to appear
        delete windowElement.dataset.hasText;

        // FORCE class change - remove old class first
        windowElement.classList.remove('glyph-morphing-to-window');
        windowElement.className = 'window-morphing-to-glyph';
        console.log(`[Minimize] Class changed to: ${windowElement.className}`);

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

        // Force the browser to commit these styles BEFORE adding transition
        windowElement.offsetHeight;

        // Use setTimeout(0) to ensure browser has painted the initial state
        setTimeout(() => {
            // NOW add the transition after initial state is rendered
            windowElement.style.transition = `all ${MINIMIZE_DURATION_MS}ms cubic-bezier(0.4, 0, 0.2, 1)`;

            console.log(`[Minimize] Transition applied`);

            // Use another frame to trigger animation AFTER transition is registered
            requestAnimationFrame(() => {
                console.log(`[Minimize] Triggering animation to target position`);

                // Animate to dot appearance and position
                windowElement.style.left = `${targetRect.x}px`;
                windowElement.style.top = `${targetRect.y}px`;
                windowElement.style.width = DOT_SIZE;
                windowElement.style.height = DOT_SIZE;
                windowElement.style.borderRadius = DOT_BORDER_RADIUS;
                windowElement.style.backgroundColor = 'var(--bg-gray)';
                windowElement.style.boxShadow = 'none';
                windowElement.style.border = DOT_BORDER;

                // Fallback timeout in case transitionend doesn't fire
                let timeoutId: ReturnType<typeof setTimeout> | null = null;

                // Listen for the transition to actually complete
                const onTransitionEnd = (e: TransitionEvent | { target: HTMLElement }) => {
                if (e.target !== windowElement) return;

                // Clear fallback timeout if transition completed normally
                if (timeoutId) {
                    clearTimeout(timeoutId);
                    timeoutId = null;
                }

                console.log(`[Minimize] Animation completed for ${glyph.id}`);
                windowElement.removeEventListener('transitionend', onTransitionEnd as EventListener);

                // Verify element identity is preserved
                console.log(`[AXIOM CHECK] Same element after animation:`, windowElement);

                // Keep element visible but reset to glyph class
                windowElement.className = 'glyph-run-glyph';

                // Remove transition so proximity morphing can take over smoothly
                windowElement.style.transition = '';

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
                windowElement.setAttribute('data-glyph-id', glyph.id);

                // CRITICAL: Ensure windowState is cleared after animation
                delete windowElement.dataset.windowState;

                // CRITICAL: Clear hasText flag to prevent proximity text appearing
                delete windowElement.dataset.hasText;

                // Now remove from body and re-attach to indicator container (SAME ELEMENT)
                windowElement.remove(); // Detach but element stays alive

                // Clear position styles now that we're re-attaching
                windowElement.style.position = '';
                windowElement.style.left = '';
                windowElement.style.top = '';
                windowElement.style.zIndex = '';

                // Re-attach THE SAME ELEMENT to indicator container
                console.log(`[AXIOM CHECK] Re-attaching same element to tray:`, windowElement);
                onMorphComplete(windowElement, glyph);
            };

                // Set up fallback timeout in case transitionend doesn't fire
                // (e.g., due to reduced motion preference, CSS interruption, rapid user action)
                timeoutId = setTimeout(() => {
                    console.warn(`[Minimize] Timeout reached for ${glyph.id}, forcing completion`);
                    windowElement.removeEventListener('transitionend', onTransitionEnd as EventListener);
                    // Force completion by calling the handler
                    onTransitionEnd({ target: windowElement });
                }, MINIMIZE_DURATION_MS + 100); // 100ms grace period

                // Add the event listener
                windowElement.addEventListener('transitionend', onTransitionEnd as EventListener);
            });
        }, 0);
    }

    /**
     * Calculate where a glyph should be positioned in the tray
     */
    private calculateGlyphTargetPosition(): { x: number, y: number } {
        const tray = document.querySelector('.glyph-run');
        if (!tray) {
            console.log(`[Minimize] No tray found, using default position`);
            return { x: window.innerWidth - 12, y: window.innerHeight / 2 };
        }

        const indicatorContainer = tray.querySelector('.glyph-run-indicators');
        if (!indicatorContainer) {
            console.log(`[Minimize] No indicator container found`);
            return { x: window.innerWidth - 12, y: window.innerHeight / 2 };
        }

        const trayRect = indicatorContainer.getBoundingClientRect();
        const glyphSize = 8;
        const gap = 2;

        // Count existing glyphs in tray (not including the one being minimized which is on body)
        const glyphsInTray = Array.from(indicatorContainer.querySelectorAll('.glyph-run-glyph'));
        console.log(`[Minimize] Found ${glyphsInTray.length} glyphs already in tray`);

        // The minimizing glyph will be added at the end
        const index = glyphsInTray.length;

        // Stack vertically in the indicator container
        const x = trayRect.right - glyphSize;
        const y = trayRect.top + (index * (glyphSize + gap));

        console.log(`[Minimize] Calculated position for index ${index}: x=${x}, y=${y}`);
        console.log(`[Minimize] Tray rect:`, trayRect);

        return { x, y };
    }

    /**
     * Make a window draggable by its title bar
     */
    private makeWindowDraggable(windowElement: HTMLElement, handle: HTMLElement): void {
        let isDragging = false;
        let offsetX = 0;
        let offsetY = 0;

        const startDrag = (e: MouseEvent) => {
            isDragging = true;

            // Calculate offset from mouse to window top-left
            const rect = windowElement.getBoundingClientRect();
            offsetX = e.clientX - rect.left;
            offsetY = e.clientY - rect.top;

            // Prevent text selection while dragging
            e.preventDefault();

            // Add cursor style
            document.body.style.cursor = 'move';

            // Move handlers to window for better capture
            window.addEventListener('mousemove', drag);
            window.addEventListener('mouseup', stopDrag);
        };

        const drag = (e: MouseEvent) => {
            if (!isDragging) return;

            // Direct position update - no transforms, no RAF
            const newX = e.clientX - offsetX;
            const newY = e.clientY - offsetY;

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
            windowElement.dataset.lastX = String(finalRect.left);
            windowElement.dataset.lastY = String(finalRect.top);

            // Remove window handlers
            window.removeEventListener('mousemove', drag);
            window.removeEventListener('mouseup', stopDrag);
        };

        handle.addEventListener('mousedown', startDrag);
    }

    /**
     * Strip HTML tags from title for plain text display
     */
    private stripHtml(html: string): string {
        const doc = new DOMParser().parseFromString(html, 'text/html');
        return doc.body.textContent || '';
    }
}
