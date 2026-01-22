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

        // Calculate window target position (center of screen by default)
        const windowWidth = parseInt(glyph.initialWidth || '800px');
        const windowHeight = parseInt(glyph.initialHeight || '600px');
        const targetX = glyph.defaultX ?? (window.innerWidth - windowWidth) / 2;
        const targetY = glyph.defaultY ?? (window.innerHeight - windowHeight) / 2;

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
            glyphElement.style.borderRadius = '8px';
            glyphElement.style.backgroundColor = 'var(--bg-primary)';
            glyphElement.style.boxShadow = '0 8px 32px rgba(0, 0, 0, 0.3)';
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
                titleBar.style.height = '32px';
                titleBar.style.backgroundColor = 'var(--bg-secondary)';
                titleBar.style.borderBottom = '1px solid var(--border-color)';
                titleBar.style.display = 'flex';
                titleBar.style.alignItems = 'center';
                titleBar.style.padding = '0 12px';
                titleBar.style.flexShrink = '0'; // Prevent title bar from shrinking

                // Add title
                const titleText = document.createElement('span');
                titleText.textContent = this.stripHtml(glyph.title);
                titleText.style.flex = '1';
                titleBar.appendChild(titleText);

                // Add minimize button
                const minimizeBtn = document.createElement('button');
                minimizeBtn.textContent = '−';
                minimizeBtn.style.width = '24px';
                minimizeBtn.style.height = '24px';
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
                    closeBtn.style.width = '24px';
                    closeBtn.style.height = '24px';
                    closeBtn.style.border = 'none';
                    closeBtn.style.background = 'transparent';
                    closeBtn.style.cursor = 'pointer';
                    closeBtn.onclick = () => {
                        // Remove from tray data AND remove element
                        onRemove(glyph.id);
                        glyphElement.remove();
                        glyph.onClose!();
                    };
                    titleBar.appendChild(closeBtn);
                }

                glyphElement.appendChild(titleBar);

                // Add content area
                const content = glyph.renderContent();
                content.style.padding = '16px';
                content.style.flex = '1'; // Take remaining space in flex container
                content.style.overflow = 'auto';
                glyphElement.appendChild(content);

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
        const targetRect = this.calculateGlyphTargetPosition(glyph.id);

        console.log(`[Minimize] Starting minimize for ${glyph.id}`);
        console.log(`[Minimize] Target position: x=${targetRect.x}, y=${targetRect.y}`);

        // Get current window state before clearing anything
        const currentRect = windowElement.getBoundingClientRect();
        console.log(`[Minimize] Current window position: x=${currentRect.left}, y=${currentRect.top}, w=${currentRect.width}, h=${currentRect.height}`);

        // Clear window state flag
        delete windowElement.dataset.windowState;

        // Clear window content but keep a visible background
        windowElement.innerHTML = '';

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
        windowElement.style.borderRadius = '8px';
        windowElement.style.boxShadow = '0 8px 32px rgba(0, 0, 0, 0.3)';
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
                windowElement.style.width = '8px';
                windowElement.style.height = '8px';
                windowElement.style.borderRadius = '2px';
                windowElement.style.backgroundColor = 'var(--bg-gray)';
                windowElement.style.boxShadow = 'none';
                windowElement.style.border = '1px solid var(--border-on-dark)';

                // Listen for the transition to actually complete
                const onTransitionEnd = (e: TransitionEvent) => {
                if (e.target !== windowElement) return;

                console.log(`[Minimize] Animation completed via transitionend for ${glyph.id}`);
                windowElement.removeEventListener('transitionend', onTransitionEnd);

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

                // Keep the glyph ID
                windowElement.setAttribute('data-glyph-id', glyph.id);

                // CRITICAL: Ensure windowState is cleared after animation
                delete windowElement.dataset.windowState;

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

                // Add the event listener
                windowElement.addEventListener('transitionend', onTransitionEnd);
            });
        }, 0);
    }

    /**
     * Calculate where a glyph should be positioned in the tray
     */
    private calculateGlyphTargetPosition(glyphId: string): { x: number, y: number } {
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
