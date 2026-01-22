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

        // Get current glyph position and size
        const glyphRect = glyphElement.getBoundingClientRect();

        // Calculate window target position (center of screen by default)
        const windowWidth = parseInt(glyph.initialWidth || '800px');
        const windowHeight = parseInt(glyph.initialHeight || '600px');
        const targetX = glyph.defaultX ?? (window.innerWidth - windowWidth) / 2;
        const targetY = glyph.defaultY ?? (window.innerHeight - windowHeight) / 2;

        // THE GLYPH ITSELF BECOMES THE WINDOW - NO CLONING
        // Remove from indicator container and reparent to body
        glyphElement.remove(); // Detach from current parent (keeps element alive)

        // Apply initial fixed positioning at current location
        glyphElement.className = 'glyph-morphing-to-window';
        glyphElement.style.position = 'fixed';
        glyphElement.style.left = `${glyphRect.left}px`;
        glyphElement.style.top = `${glyphRect.top}px`;
        glyphElement.style.width = `${glyphRect.width}px`;
        glyphElement.style.height = `${glyphRect.height}px`;
        glyphElement.style.zIndex = '1000';

        // Clear any proximity text that might be present
        if (glyphElement.dataset.hasText) {
            glyphElement.textContent = '';
            delete glyphElement.dataset.hasText;
        }

        // Reparent to document body for morphing
        document.body.appendChild(glyphElement);

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
            glyphElement.style.opacity = '1'; // Ensure it's visible

            // After animation completes, add window content
            setTimeout(() => {
                // Add window chrome (title bar, controls)
                const titleBar = document.createElement('div');
                titleBar.className = 'window-title-bar';
                titleBar.style.height = '32px';
                titleBar.style.backgroundColor = 'var(--bg-secondary)';
                titleBar.style.borderBottom = '1px solid var(--border-color)';
                titleBar.style.display = 'flex';
                titleBar.style.alignItems = 'center';
                titleBar.style.padding = '0 12px';

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
                content.style.height = 'calc(100% - 32px)';
                content.style.overflow = 'auto';
                glyphElement.appendChild(content);

                // Make window draggable
                this.makeWindowDraggable(glyphElement, titleBar);
            }, 600); // Match animation duration
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
        // Find the position where this glyph should go back to
        const targetRect = this.calculateGlyphTargetPosition(glyph.id);

        // Clear window state flag
        delete windowElement.dataset.windowState;

        // Add morphing class
        windowElement.className = 'window-morphing-to-glyph';

        // Clear window content (title bar, content area)
        windowElement.innerHTML = '';

        // Start morphing back to dot at the target position
        windowElement.style.left = `${targetRect.x}px`;
        windowElement.style.top = `${targetRect.y}px`;
        windowElement.style.width = '8px';
        windowElement.style.height = '8px';
        windowElement.style.borderRadius = '2px';
        windowElement.style.backgroundColor = 'var(--bg-gray)';
        windowElement.style.boxShadow = 'none';
        windowElement.style.padding = '0';
        windowElement.style.border = '1px solid var(--border-on-dark)';

        // After animation completes, move element back to indicator container
        setTimeout(() => {
            // Remove from body
            windowElement.remove();

            // Reset to glyph class
            windowElement.className = 'glyph-run-glyph';

            // Reset inline styles (let CSS take over)
            windowElement.style.position = '';
            windowElement.style.left = '';
            windowElement.style.top = '';
            windowElement.style.width = '';
            windowElement.style.height = '';
            windowElement.style.borderRadius = '';
            windowElement.style.backgroundColor = '';
            windowElement.style.boxShadow = '';
            windowElement.style.padding = '';
            windowElement.style.border = '';
            windowElement.style.zIndex = '';
            windowElement.style.opacity = '';

            // Keep the glyph ID
            windowElement.setAttribute('data-glyph-id', glyph.id);

            // CRITICAL: Ensure windowState is cleared after animation
            delete windowElement.dataset.windowState;

            // Re-attach to indicator container (done by caller)
            onMorphComplete(windowElement, glyph);
        }, 600);
    }

    /**
     * Calculate where a glyph should be positioned in the tray
     */
    private calculateGlyphTargetPosition(glyphId: string): { x: number, y: number } {
        const tray = document.querySelector('.glyph-run');
        if (!tray) return { x: 0, y: 0 };

        const trayRect = tray.getBoundingClientRect();
        const glyphSize = 8;
        const gap = 2;

        // Get index from existing glyphs in tray
        const glyphsInTray = Array.from(tray.querySelectorAll('.glyph-run-glyph'));
        const index = glyphsInTray.findIndex(el => el.getAttribute('data-glyph-id') === glyphId);

        // Stack vertically
        return {
            x: trayRect.right - glyphSize - 4, // 4px from right edge
            y: trayRect.top + (Math.max(0, index) * (glyphSize + gap))
        };
    }

    /**
     * Make a window draggable by its title bar
     */
    private makeWindowDraggable(windowElement: HTMLElement, handle: HTMLElement): void {
        let isDragging = false;
        let startX = 0;
        let startY = 0;
        let initialX = 0;
        let initialY = 0;

        const startDrag = (e: MouseEvent) => {
            isDragging = true;
            startX = e.clientX;
            startY = e.clientY;
            const rect = windowElement.getBoundingClientRect();
            initialX = rect.left;
            initialY = rect.top;
            e.preventDefault();
        };

        const drag = (e: MouseEvent) => {
            if (!isDragging) return;
            const deltaX = e.clientX - startX;
            const deltaY = e.clientY - startY;
            windowElement.style.left = `${initialX + deltaX}px`;
            windowElement.style.top = `${initialY + deltaY}px`;
        };

        const stopDrag = () => {
            isDragging = false;
        };

        handle.addEventListener('mousedown', startDrag);
        document.addEventListener('mousemove', drag);
        document.addEventListener('mouseup', stopDrag);
    }

    /**
     * Strip HTML tags from title for plain text display
     */
    private stripHtml(html: string): string {
        const doc = new DOMParser().parseFromString(html, 'text/html');
        return doc.body.textContent || '';
    }
}