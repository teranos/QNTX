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

        // Reveal on mouse enter
        this.element.addEventListener('mouseenter', () => {
            this.reveal();
        });

        // Hide on mouse leave (with delay)
        this.element.addEventListener('mouseleave', () => {
            this.scheduleHide();
        });
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

        // Render indicators (dots)
        this.items.forEach(() => {
            const dot = document.createElement('div');
            dot.className = 'window-tray-dot';
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
