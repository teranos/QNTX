/**
 * Window Tray - Hidden dock for minimized windows
 *
 * Design: Nearly invisible until needed. Shows tiny dot indicators when
 * windows are minimized. Hovering near the zone reveals the window items.
 * The minimize animation teaches users where windows go.
 */

export interface TrayItem {
    id: string;
    title: string;
    onRestore: () => void;
    onClose?: () => void;
}

class WindowTrayImpl {
    private element: HTMLElement | null = null;
    private itemsContainer: HTMLElement | null = null;
    private indicatorContainer: HTMLElement | null = null;
    private items: Map<string, TrayItem> = new Map();
    private isRevealed: boolean = false;
    private hideTimeout: number | null = null;
    private mouseX: number = 0;
    private mouseY: number = 0;
    private proximityRAF: number | null = null;
    private proximityThreshold: number = 150; // Max distance for morphing effect (px)

    /**
     * Initialize the tray and attach to DOM
     * Call this once when the app starts
     */
    public init(): void {
        if (this.element) return; // Already initialized

        const graphContainer = document.getElementById('graph-container');
        if (!graphContainer) {
            console.warn('WindowTray: #graph-container not found, deferring init');
            return;
        }

        this.element = document.createElement('div');
        this.element.className = 'window-tray';
        this.element.setAttribute('data-empty', 'true');

        // Indicator dots (visible when not revealed)
        this.indicatorContainer = document.createElement('div');
        this.indicatorContainer.className = 'window-tray-indicators';
        this.element.appendChild(this.indicatorContainer);

        // Items container (visible when revealed)
        this.itemsContainer = document.createElement('div');
        this.itemsContainer.className = 'window-tray-items';
        this.element.appendChild(this.itemsContainer);

        graphContainer.appendChild(this.element);

        this.setupEventListeners();
    }

    private setupEventListeners(): void {
        if (!this.element) return;

        // Track mouse position globally for proximity effect
        document.addEventListener('mousemove', (e) => {
            this.mouseX = e.clientX;
            this.mouseY = e.clientY;
            this.updateProximity();
        });

        // Note: mouseenter/mouseleave removed - proximity morphing replaces reveal behavior
        // Container has pointer-events: none, only dots are interactive
    }

    private reveal(): void {
        if (this.hideTimeout) {
            clearTimeout(this.hideTimeout);
            this.hideTimeout = null;
        }
        if (!this.isRevealed && this.items.size > 0) {
            this.isRevealed = true;
            this.element?.setAttribute('data-revealed', 'true');
        }
    }

    private scheduleHide(): void {
        if (this.hideTimeout) {
            clearTimeout(this.hideTimeout);
        }
        this.hideTimeout = window.setTimeout(() => {
            this.isRevealed = false;
            this.element?.setAttribute('data-revealed', 'false');
            this.hideTimeout = null;
        }, 300);
    }

    /**
     * Update proximity-based morphing for each dot
     * Uses requestAnimationFrame for smooth 60fps updates
     */
    private updateProximity(): void {
        if (this.proximityRAF) {
            cancelAnimationFrame(this.proximityRAF);
        }

        this.proximityRAF = requestAnimationFrame(() => {
            if (!this.indicatorContainer || this.isRevealed) return;

            const dots = Array.from(this.indicatorContainer.querySelectorAll('.window-tray-dot')) as HTMLElement[];
            const itemsArray = Array.from(this.items.values());

            dots.forEach((dot, index) => {
                // Cache the initial position (when dot is 8×8) to prevent flickering
                // as the element grows and its bounds change
                if (!dot.dataset.initialCenterX) {
                    const rect = dot.getBoundingClientRect();
                    dot.dataset.initialCenterX = String(rect.left + rect.width / 2);
                    dot.dataset.initialCenterY = String(rect.top + rect.height / 2);
                }

                const centerX = parseFloat(dot.dataset.initialCenterX);
                const centerY = parseFloat(dot.dataset.initialCenterY);

                // Calculate distance from mouse to initial dot center
                const dx = this.mouseX - centerX;
                const dy = this.mouseY - centerY;
                const distance = Math.sqrt(dx * dx + dy * dy);

                // Calculate proximity factor (1.0 = at dot, 0.0 = at threshold or beyond)
                const proximityRaw = Math.max(0, 1 - (distance / this.proximityThreshold));

                // Snap to 100% when 90% close to prevent flickering/micro-adjustments
                let proximity: number;
                if (proximityRaw >= 0.9) {
                    proximity = 1.0;
                } else if (proximityRaw < 0.8) {
                    // First 80% of distance: only morph 40% (0.0 → 0.4)
                    proximity = (proximityRaw / 0.8) * 0.4;
                } else {
                    // 80-90% of distance: morph remaining 60% (0.4 → 1.0)
                    proximity = 0.4 + ((proximityRaw - 0.8) / 0.1) * 0.6;
                }

                // Interpolate dimensions to match actual tray item size
                // Start: 8px × 8px square
                // End: ~220px × 32px (actual tray item dimensions)
                const minWidth = 8;
                const maxWidth = 220;
                const minHeight = 8;
                const maxHeight = 32;
                const width = minWidth + (maxWidth - minWidth) * proximity;
                const height = minHeight + (maxHeight - minHeight) * proximity;

                // Interpolate border radius (2px square -> 0px for full item)
                const borderRadius = 2 * (1 - proximity);

                // Interpolate colors using RGB interpolation
                // Start: --bg-gray (#999 = rgb(153,153,153))
                // End: --bg-almost-black (#1a1a1a = rgb(26,26,26))
                const startR = 153, startG = 153, startB = 153;
                const endR = 26, endG = 26, endB = 26;
                const r = Math.round(startR + (endR - startR) * proximity);
                const g = Math.round(startG + (endG - startG) * proximity);
                const b = Math.round(startB + (endB - startB) * proximity);

                // Apply morphing styles
                dot.style.width = `${width}px`;
                dot.style.height = `${height}px`;
                dot.style.borderRadius = `${borderRadius}px`;
                dot.style.backgroundColor = `rgb(${r}, ${g}, ${b})`;

                // Show title text when proximity is high enough
                if (proximity > 0.5 && index < itemsArray.length) {
                    const item = itemsArray[index];
                    const title = this.stripHtml(item.title);

                    // Add text content if not already present
                    if (!dot.dataset.hasText) {
                        dot.style.display = 'flex';
                        dot.style.alignItems = 'center';
                        dot.style.justifyContent = 'flex-start'; // Left-align text (normal)
                        dot.style.padding = '6px 10px';
                        dot.style.whiteSpace = 'nowrap';
                        dot.textContent = title;
                        dot.dataset.hasText = 'true';
                    }
                    // Fade in text based on proximity
                    dot.style.opacity = String(0.5 + (proximity - 0.5));
                } else {
                    // Hide text when far away
                    if (dot.dataset.hasText) {
                        dot.textContent = '';
                        dot.style.display = '';
                        dot.style.alignItems = '';
                        dot.style.justifyContent = '';
                        dot.style.padding = '';
                        dot.style.whiteSpace = '';
                        dot.style.textAlign = '';
                        delete dot.dataset.hasText;
                    }
                    dot.style.opacity = '1';
                }
            });

            this.proximityRAF = null;
        });
    }

    /**
     * Add a minimized window to the tray
     */
    public add(item: TrayItem): void {
        this.init(); // Ensure initialized

        if (this.items.has(item.id)) {
            return; // Already in tray
        }

        this.items.set(item.id, item);
        this.renderItems();
        this.element?.setAttribute('data-empty', 'false');
    }

    /**
     * Remove a window from the tray (when restored or closed)
     */
    public remove(id: string): void {
        if (!this.items.has(id)) return;

        this.items.delete(id);
        this.renderItems();

        if (this.items.size === 0) {
            this.element?.setAttribute('data-empty', 'true');
            this.element?.setAttribute('data-revealed', 'false');
            this.isRevealed = false;
        }
    }

    /**
     * Check if a window is in the tray
     */
    public has(id: string): boolean {
        return this.items.has(id);
    }

    /**
     * Get the tray element position for minimize animation target
     */
    public getTargetPosition(): { x: number; y: number } | null {
        if (!this.element) return null;
        const rect = this.element.getBoundingClientRect();
        return {
            x: rect.left + rect.width / 2,
            y: rect.top + rect.height / 2
        };
    }

    private renderItems(): void {
        if (!this.itemsContainer || !this.indicatorContainer) return;

        // Clear existing
        this.itemsContainer.innerHTML = '';
        this.indicatorContainer.innerHTML = '';

        // Render indicators (dots) with click handlers
        this.items.forEach((item) => {
            const dot = document.createElement('div');
            dot.className = 'window-tray-dot';
            dot.setAttribute('data-window-id', item.id);

            // Restore window on click
            dot.addEventListener('click', (e) => {
                e.stopPropagation();
                item.onRestore();
            });

            this.indicatorContainer!.appendChild(dot);
        });

        // Render items
        this.items.forEach((item) => {
            const itemEl = document.createElement('div');
            itemEl.className = 'window-tray-item';
            itemEl.setAttribute('data-window-id', item.id);

            const titleEl = document.createElement('span');
            titleEl.className = 'window-tray-item-title';
            titleEl.textContent = this.stripHtml(item.title);
            itemEl.appendChild(titleEl);

            // Restore on click
            itemEl.addEventListener('click', (e) => {
                e.stopPropagation();
                item.onRestore();
            });

            this.itemsContainer!.appendChild(itemEl);
        });
    }

    /**
     * Strip HTML tags from title for plain text display
     */
    private stripHtml(html: string): string {
        const tmp = document.createElement('div');
        tmp.innerHTML = html;
        return tmp.textContent || tmp.innerText || '';
    }

    /**
     * Get count of minimized windows
     */
    public get count(): number {
        return this.items.size;
    }
}

// Singleton instance
export const windowTray = new WindowTrayImpl();
