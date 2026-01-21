/**
 * Window Tray - Hidden dock for minimized windows
 *
 * Design: Nearly invisible until needed. Shows tiny dot indicators when
 * windows are minimized. Hovering near the zone reveals the window items.
 * The minimize animation teaches users where windows go.
 */

import { log, SEG } from '../logger';
import { uiState } from '../state/ui';

export interface TrayItem {
    id: string;
    title: string;
    symbol?: string;  // Symbol to show in collapsed/morphed state

    // Content rendering (new dot-as-primitive)
    renderContent?: () => HTMLElement;  // Returns the content to display when expanded

    // Window configuration (new dot-as-primitive)
    initialWidth?: string;
    initialHeight?: string;
    defaultX?: number;
    defaultY?: number;

    // Callbacks
    onExpand?: () => void;
    onCollapse?: () => void;
    onClose?: () => void;

    // DEPRECATED (for backward compatibility during migration)
    onRestore?: (sourceRect?: DOMRect) => void;
}

class WindowTrayImpl {
    // Proximity morphing configuration
    private readonly PROXIMITY_THRESHOLD_HORIZONTAL = 30; // Max distance for horizontal approach (px)
    private readonly PROXIMITY_THRESHOLD_VERTICAL = 110; // Max distance for vertical approach (px)
    private readonly SNAP_THRESHOLD = 0.9; // Snap to 100% at this proximity to prevent flickering
    private readonly BASELINE_BOOST_TRIGGER = 0.80; // Trigger baseline boost when any item this close
    private readonly BASELINE_BOOST_AMOUNT = 0.3; // Amount to boost all items (0.0-1.0)
    private readonly TEXT_FADE_THRESHOLD = 0.5; // Show text when proximity exceeds this

    // Deferred items to add after init
    private deferredItems: TrayItem[] = [];

    // Horizontal easing: gradual approach, dramatic finish
    private readonly HORIZONTAL_EASE_BREAKPOINT = 0.8; // 80% proximity
    private readonly HORIZONTAL_EASE_EARLY = 0.4; // Transform 40% by breakpoint
    private readonly HORIZONTAL_EASE_LATE = 0.6; // Remaining 60% in final stretch

    // Vertical easing: fast bloom, slow refinement (inverted)
    private readonly VERTICAL_EASE_BREAKPOINT = 0.55; // 55% proximity
    private readonly VERTICAL_EASE_EARLY = 0.8; // Transform 80% by breakpoint
    private readonly VERTICAL_EASE_LATE = 0.2; // Remaining 20% in final stretch

    // Morphing dimensions
    private readonly DOT_MIN_WIDTH = 8;
    private readonly DOT_MIN_HEIGHT = 8;
    private readonly DOT_MAX_WIDTH = 220;
    private readonly DOT_MAX_HEIGHT = 32;
    private readonly DOT_BORDER_RADIUS_MAX = 2; // Initial border radius for dots

    // Component state
    private element: HTMLElement | null = null;
    private indicatorContainer: HTMLElement | null = null;
    private items: Map<string, TrayItem> = new Map();
    private mouseX: number = 0;
    private mouseY: number = 0;
    private proximityRAF: number | null = null;
    private isRestoring: boolean = false; // Disable proximity morphing during restore
    private expandedDots: Map<string, HTMLElement> = new Map(); // Track expanded windows (dot-as-primitive)

    /**
     * Initialize the tray and attach to DOM
     * Call this once when the app starts
     */
    public init(): void {
        if (this.element) return; // Already initialized

        const graphContainer = document.getElementById('graph-container');
        if (!graphContainer) {
            log.warn(SEG.UI, 'WindowTray: #graph-container not found, deferring init');
            return;
        }

        this.element = document.createElement('div');
        this.element.className = 'window-tray';
        this.element.setAttribute('data-empty', 'true');

        // Indicator dots
        this.indicatorContainer = document.createElement('div');
        this.indicatorContainer.className = 'window-tray-indicators';
        this.element.appendChild(this.indicatorContainer);

        graphContainer.appendChild(this.element);

        this.setupEventListeners();

        // Process any deferred items that tried to add before init
        if (this.deferredItems.length > 0) {
            const itemsToAdd = [...this.deferredItems];
            this.deferredItems = [];
            itemsToAdd.forEach(item => {
                if (!this.items.has(item.id)) {
                    this.items.set(item.id, item);
                }
            });
            this.renderItems();
            if (this.items.size > 0) {
                this.element.setAttribute('data-empty', 'false');
                // Sync to uiState
                const ids = Array.from(this.items.keys());
                ids.forEach(id => uiState.addMinimizedWindow(id));
            }
        }
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

    /**
     * Calculate proximity metrics for a dot element
     */
    private calculateProximity(dot: HTMLElement): {
        distance: number;
        distanceX: number;
        distanceY: number;
        proximityRaw: number;
        isVerticalApproach: boolean;
    } {
        const rect = dot.getBoundingClientRect();
        let distanceX = 0, distanceY = 0;

        // Horizontal distance to nearest edge
        if (this.mouseX < rect.left) {
            distanceX = rect.left - this.mouseX;
        } else if (this.mouseX > rect.right) {
            distanceX = this.mouseX - rect.right;
        }

        // Vertical distance to nearest edge
        if (this.mouseY < rect.top) {
            distanceY = rect.top - this.mouseY;
        } else if (this.mouseY > rect.bottom) {
            distanceY = this.mouseY - rect.bottom;
        }

        // Euclidean distance to nearest edge (0 if inside)
        const distance = Math.sqrt(distanceX * distanceX + distanceY * distanceY);

        // Determine approach direction
        const isVerticalApproach = distanceY > distanceX;

        // Use appropriate threshold based on approach direction
        const threshold = isVerticalApproach
            ? this.PROXIMITY_THRESHOLD_VERTICAL
            : this.PROXIMITY_THRESHOLD_HORIZONTAL;

        // Calculate proximity factor (1.0 = at dot, 0.0 = at threshold or beyond)
        const proximityRaw = Math.max(0, 1 - (distance / threshold));

        return { distance, distanceX, distanceY, proximityRaw, isVerticalApproach };
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
            if (!this.indicatorContainer || this.isRestoring) return;

            const dots = Array.from(this.indicatorContainer.querySelectorAll('.window-tray-dot')) as HTMLElement[];
            const itemsArray = Array.from(this.items.values());

            // First pass: check if any dot is highly proximate (gives baseline boost to all)
            let maxProximityRaw = 0;
            dots.forEach((dot) => {
                const { proximityRaw } = this.calculateProximity(dot);
                maxProximityRaw = Math.max(maxProximityRaw, proximityRaw);
            });

            // Calculate baseline boost when any dot is nearly fully expanded
            const baselineBoost = maxProximityRaw > this.BASELINE_BOOST_TRIGGER ? this.BASELINE_BOOST_AMOUNT : 0;

            dots.forEach((dot, index) => {
                const { proximityRaw, isVerticalApproach } = this.calculateProximity(dot);

                // Apply different easing based on approach direction
                let proximity: number;

                // Snap to 100% when very close to prevent flickering
                if (proximityRaw >= this.SNAP_THRESHOLD) {
                    proximity = 1.0;
                } else {

                    if (isVerticalApproach) {
                        // VERTICAL: Inverted easing - fast early growth, slow refinement
                        if (proximityRaw < this.VERTICAL_EASE_BREAKPOINT) {
                            proximity = (proximityRaw / this.VERTICAL_EASE_BREAKPOINT) * this.VERTICAL_EASE_EARLY;
                        } else {
                            const remaining = 1.0 - this.VERTICAL_EASE_BREAKPOINT;
                            proximity = this.VERTICAL_EASE_EARLY +
                                      ((proximityRaw - this.VERTICAL_EASE_BREAKPOINT) / remaining) * this.VERTICAL_EASE_LATE;
                        }
                    } else {
                        // HORIZONTAL: Gradual growth, dramatic finish
                        if (proximityRaw < this.HORIZONTAL_EASE_BREAKPOINT) {
                            proximity = (proximityRaw / this.HORIZONTAL_EASE_BREAKPOINT) * this.HORIZONTAL_EASE_EARLY;
                        } else {
                            const remaining = this.SNAP_THRESHOLD - this.HORIZONTAL_EASE_BREAKPOINT;
                            proximity = this.HORIZONTAL_EASE_EARLY +
                                      ((proximityRaw - this.HORIZONTAL_EASE_BREAKPOINT) / remaining) * this.HORIZONTAL_EASE_LATE;
                        }
                    }
                }

                // Apply baseline boost when any item is being hovered
                proximity = Math.min(1.0, proximity + baselineBoost);

                // Interpolate dimensions to match actual tray item size
                const width = this.DOT_MIN_WIDTH + (this.DOT_MAX_WIDTH - this.DOT_MIN_WIDTH) * proximity;
                const height = this.DOT_MIN_HEIGHT + (this.DOT_MAX_HEIGHT - this.DOT_MIN_HEIGHT) * proximity;

                // Interpolate border radius (starts at max, goes to 0 for full item)
                const borderRadius = this.DOT_BORDER_RADIUS_MAX * (1 - proximity);

                // Interpolate colors using RGB interpolation
                // Start: --bg-gray (#999 = rgb(153,153,153))
                // End: --bg-almost-black (#1a1a1a = rgb(26,26,26))
                const startR = 153, startG = 153, startB = 153;
                const endR = 26, endG = 26, endB = 26;
                let r = Math.round(startR + (endR - startR) * proximity);
                let g = Math.round(startG + (endG - startG) * proximity);
                let b = Math.round(startB + (endB - startB) * proximity);

                // Brighten on hover (10% lighter)
                const isHovered = dot.matches(':hover');
                if (isHovered) {
                    r = Math.min(255, Math.round(r + (255 - r) * 0.1));
                    g = Math.min(255, Math.round(g + (255 - g) * 0.1));
                    b = Math.min(255, Math.round(b + (255 - b) * 0.1));
                }

                // Apply morphing styles
                dot.style.width = `${width}px`;
                dot.style.height = `${height}px`;
                dot.style.borderRadius = `${borderRadius}px`;
                dot.style.backgroundColor = `rgb(${r}, ${g}, ${b})`;

                // Show title text when proximity exceeds threshold
                if (proximity > this.TEXT_FADE_THRESHOLD && index < itemsArray.length) {
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
                    // Fade in text based on proximity (above threshold)
                    dot.style.opacity = String(this.TEXT_FADE_THRESHOLD + (proximity - this.TEXT_FADE_THRESHOLD));
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
     * Load tray state from uiState
     * Returns array of window IDs that were minimized
     */
    public loadState(): string[] {
        return uiState.getMinimizedWindows();
    }

    /**
     * Add a minimized window to the tray
     */
    public add(item: TrayItem, skipSave: boolean = false): void {
        // Try to initialize, but if it fails, defer the item
        this.init();

        if (!this.element) {
            // Tray not ready yet, defer this item
            this.deferredItems.push(item);
            return;
        }

        if (this.items.has(item.id)) {
            return; // Already in tray
        }

        this.items.set(item.id, item);
        this.renderItems();
        this.element.setAttribute('data-empty', 'false');

        // Only save state if not skipping (skip during restore from uiState)
        if (!skipSave) {
            uiState.addMinimizedWindow(item.id);
        }
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
        }

        // Remove from uiState
        uiState.removeMinimizedWindow(id);
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
        if (!this.indicatorContainer) return;

        // Clear existing
        this.indicatorContainer.innerHTML = '';

        // Render indicators (dots) with click handlers
        this.items.forEach((item) => {
            const dot = document.createElement('div');
            dot.className = 'window-tray-dot';
            dot.setAttribute('data-window-id', item.id);

            // Restore window on click
            dot.addEventListener('click', (e) => {
                e.stopPropagation();

                // Disable proximity morphing - let CSS transition handle the collapse
                this.isRestoring = true;

                // Get dot's current position for spatial continuity
                const dotRect = dot.getBoundingClientRect();

                // New dot-as-primitive: expand if renderContent exists
                if (item.renderContent) {
                    this.expand(item.id);
                    // Re-enable proximity after animation
                    setTimeout(() => {
                        this.isRestoring = false;
                    }, 300);
                    return;
                }

                // Legacy: Start window restore immediately (animates from dot's exact position)
                if (item.onRestore) {
                    item.onRestore(dotRect);
                }

                // Keep clicked dot visible during window animation, collapse others
                const allDots = this.indicatorContainer!.querySelectorAll('.window-tray-dot');
                allDots.forEach((d) => {
                    const htmlDot = d as HTMLElement;

                    if (htmlDot !== dot) {
                        // Other dots: collapse back to square immediately
                        htmlDot.style.width = '';
                        htmlDot.style.height = '';
                        htmlDot.style.borderRadius = '';
                        htmlDot.style.backgroundColor = '';
                        htmlDot.style.opacity = '';
                        htmlDot.style.display = '';
                        htmlDot.style.alignItems = '';
                        htmlDot.style.justifyContent = '';
                        htmlDot.style.padding = '';
                        htmlDot.style.whiteSpace = '';
                        htmlDot.textContent = '';
                        delete htmlDot.dataset.hasText;
                    }
                });

                // Fade out clicked dot as window morphs from it
                setTimeout(() => {
                    dot.style.transition = 'opacity 0.2s ease-out';
                    dot.style.opacity = '0';
                }, 50);

                // Re-enable proximity after animation
                setTimeout(() => {
                    this.isRestoring = false;
                }, 300);
            });

            this.indicatorContainer!.appendChild(dot);
        });
    }

    /**
     * Expand a dot into full window (dot-as-primitive)
     */
    public expand(id: string): void {
        const item = this.items.get(id);
        if (!item || !item.renderContent || this.expandedDots.has(id)) return;

        log.debug(SEG.UI, `Expanding dot: ${id}`);

        // Create window container
        const windowEl = document.createElement('div');
        windowEl.className = 'tray-window';
        windowEl.setAttribute('data-window-id', id);

        // Create title bar
        const titleBar = this.createTitleBar(item);
        windowEl.appendChild(titleBar);

        // Create content area
        const contentArea = document.createElement('div');
        contentArea.className = 'tray-window-content';
        contentArea.appendChild(item.renderContent());
        windowEl.appendChild(contentArea);

        // Position window (restore from state or use defaults)
        const state = uiState.getWindowState(id);
        if (state) {
            windowEl.style.left = `${state.x}px`;
            windowEl.style.top = `${state.y}px`;
            windowEl.style.width = state.width;
            if (state.height) {
                windowEl.style.height = state.height;
            }
        } else {
            // Default positioning
            windowEl.style.left = `${item.defaultX || 100}px`;
            windowEl.style.top = `${item.defaultY || 100}px`;
            windowEl.style.width = item.initialWidth || '600px';
            if (item.initialHeight) {
                windowEl.style.height = item.initialHeight;
            }
        }

        // Add to DOM
        document.body.appendChild(windowEl);

        // Track expanded state
        this.expandedDots.set(id, windowEl);
        uiState.updateWindowState(id, { minimized: false });

        // Make draggable
        this.makeDraggable(windowEl);

        // Callback
        item.onExpand?.();
    }

    /**
     * Collapse expanded window back to dot (dot-as-primitive)
     */
    public collapse(id: string): void {
        const windowEl = this.expandedDots.get(id);
        if (!windowEl) return;

        const item = this.items.get(id);
        log.debug(SEG.UI, `Collapsing dot: ${id}`);

        // Animate collapse back to tray (optional)
        // windowEl.style.transition = 'all 0.3s ease';
        // const trayPos = this.getTargetPosition();
        // windowEl.style.transform = `translate(${trayPos.x}px, ${trayPos.y}px) scale(0)`;

        // Save final state before removing
        const rect = windowEl.getBoundingClientRect();
        uiState.updateWindowState(id, {
            x: rect.left,
            y: rect.top,
            width: windowEl.style.width,
            height: windowEl.style.height || undefined,
            minimized: true
        });

        // Remove from DOM
        windowEl.remove();
        this.expandedDots.delete(id);

        // Callback
        item?.onCollapse?.();
    }

    /**
     * Create title bar with minimize/close buttons (dot-as-primitive)
     */
    private createTitleBar(item: TrayItem): HTMLElement {
        const titleBar = document.createElement('div');
        titleBar.className = 'tray-window-title-bar';

        const title = document.createElement('span');
        title.className = 'tray-window-title';
        title.textContent = item.title;
        titleBar.appendChild(title);

        const buttons = document.createElement('div');
        buttons.className = 'tray-window-buttons';

        // Minimize button
        const minimizeBtn = document.createElement('button');
        minimizeBtn.className = 'tray-window-button-minimize';
        minimizeBtn.textContent = '−';
        minimizeBtn.setAttribute('aria-label', 'Minimize');
        minimizeBtn.addEventListener('click', () => this.collapse(item.id));
        buttons.appendChild(minimizeBtn);

        // Close button
        const closeBtn = document.createElement('button');
        closeBtn.className = 'tray-window-button-close';
        closeBtn.textContent = '×';
        closeBtn.setAttribute('aria-label', 'Close');
        closeBtn.addEventListener('click', () => {
            if (item.onClose) {
                item.onClose();
            } else {
                this.collapse(item.id); // Default: just minimize
            }
        });
        buttons.appendChild(closeBtn);

        titleBar.appendChild(buttons);

        return titleBar;
    }

    /**
     * Make window draggable (dot-as-primitive)
     */
    private makeDraggable(windowEl: HTMLElement): void {
        const titleBar = windowEl.querySelector('.tray-window-title-bar') as HTMLElement;
        if (!titleBar) return;

        let isDragging = false;
        let offsetX = 0;
        let offsetY = 0;

        const onMouseDown = (e: MouseEvent) => {
            isDragging = true;
            const rect = windowEl.getBoundingClientRect();
            offsetX = e.clientX - rect.left;
            offsetY = e.clientY - rect.top;

            document.addEventListener('mousemove', onMouseMove);
            document.addEventListener('mouseup', onMouseUp);

            // Prevent text selection during drag
            e.preventDefault();
        };

        const onMouseMove = (e: MouseEvent) => {
            if (!isDragging) return;

            const x = e.clientX - offsetX;
            const y = e.clientY - offsetY;

            windowEl.style.left = `${x}px`;
            windowEl.style.top = `${y}px`;
        };

        const onMouseUp = () => {
            if (!isDragging) return;
            isDragging = false;

            document.removeEventListener('mousemove', onMouseMove);
            document.removeEventListener('mouseup', onMouseUp);

            // Save position
            const id = windowEl.getAttribute('data-window-id');
            if (id) {
                const rect = windowEl.getBoundingClientRect();
                uiState.updateWindowState(id, {
                    x: rect.left,
                    y: rect.top
                });
            }
        };

        titleBar.addEventListener('mousedown', onMouseDown);
    }

    /**
     * Register a dot (dot-as-primitive)
     * Used to register dots on startup rather than only when minimizing
     */
    public register(item: TrayItem): void {
        if (this.items.has(item.id)) {
            log.warn(SEG.UI, `Dot already registered: ${item.id}`);
            return;
        }

        this.items.set(item.id, item);
        this.renderItems();
        if (this.element) {
            this.element.setAttribute('data-empty', 'false');
        }
        log.debug(SEG.UI, `Dot registered: ${item.id}`);
    }

    /**
     * Strip HTML tags from title for plain text display in expanded dots
     */
    private stripHtml(html: string): string {
        const doc = new DOMParser().parseFromString(html, 'text/html');
        return doc.body.textContent || '';
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
