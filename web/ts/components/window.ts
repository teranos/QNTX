/**
 * Generic draggable, non-modal window component
 * Supports multiple windows with z-index stacking
 */

import { windowTray } from './window-tray';

export interface WindowConfig {
    id: string;
    title: string;
    width?: string;
    height?: string;
    initialX?: number;
    initialY?: number;
    onClose?: () => void;
    onShow?: () => void;
    onHide?: () => void;
    onMinimize?: () => void;
    onRestore?: () => void;
}

export class Window {
    private element: HTMLElement;
    private header: HTMLElement;
    private contentContainer: HTMLElement;
    private footerContainer: HTMLElement | null = null;
    private config: WindowConfig;

    // Drag state
    private isDragging: boolean = false;
    private dragOffsetX: number = 0;
    private dragOffsetY: number = 0;

    // Minimize state
    private minimized: boolean = false;
    private savedPosition: { x: number; y: number } | null = null;

    // Global window management
    private static zIndexCounter: number = 9999;
    private static openWindows: Set<Window> = new Set();
    private static readonly CASCADE_OFFSET = 30; // px offset for cascading windows

    // Animation timing - must match CSS transition duration (0.3s)
    private static readonly ANIMATION_DURATION_MS = 300;

    // LocalStorage key for window state persistence
    private static readonly STORAGE_KEY = 'qntx_window_state';

    constructor(config: WindowConfig) {
        this.config = config;
        this.element = this.createElement();
        this.header = this.element.querySelector('.draggable-window-header') as HTMLElement;
        this.contentContainer = this.element.querySelector('.draggable-window-content') as HTMLElement;
        this.footerContainer = this.element.querySelector('.draggable-window-footer') as HTMLElement | null;

        document.body.appendChild(this.element);
        this.setupEventListeners();
        Window.openWindows.add(this);

        // Restore previous session state (position, size, visibility, minimized)
        this.restoreState();

        // Check if this window was previously minimized
        // Call immediately so it can be deferred before windowTray.init() runs
        this.restoreMinimizedState();
    }

    private createElement(): HTMLElement {
        const win = document.createElement('div');
        win.id = this.config.id;
        win.className = 'draggable-window';

        // Set dimensions
        if (this.config.width) win.style.width = this.config.width;
        if (this.config.height) win.style.height = this.config.height;

        // Position with cascade offset for multiple windows
        const offset = (Window.openWindows.size) * Window.CASCADE_OFFSET;
        const x = this.config.initialX ?? (100 + offset);
        const y = this.config.initialY ?? (100 + offset);
        win.style.left = `${x}px`;
        win.style.top = `${y}px`;

        // Initial z-index
        win.style.zIndex = String(Window.zIndexCounter++);

        win.innerHTML = `
            <div class="draggable-window-header">
                <span class="draggable-window-title">${this.config.title}</span>
                <div class="draggable-window-controls">
                    <button class="panel-minimize" aria-label="Minimize">&minus;</button>
                    <button class="panel-close" aria-label="Close">&times;</button>
                </div>
            </div>
            <div class="draggable-window-content"></div>
            <div class="draggable-window-footer"></div>
        `;

        return win;
    }

    private setupEventListeners(): void {
        const closeBtn = this.element.querySelector('.panel-close') as HTMLButtonElement;
        const minimizeBtn = this.element.querySelector('.panel-minimize') as HTMLButtonElement;

        // Dragging - mousedown on header
        this.header.addEventListener('mousedown', (e) => {
            // Ignore if clicking control buttons
            if ((e.target as HTMLElement).closest('.draggable-window-controls')) return;

            this.isDragging = true;
            const rect = this.element.getBoundingClientRect();
            this.dragOffsetX = e.clientX - rect.left;
            this.dragOffsetY = e.clientY - rect.top;
            this.header.style.cursor = 'grabbing';
            this.bringToFront();
        });

        // Dragging - mousemove on document
        document.addEventListener('mousemove', (e) => {
            if (!this.isDragging) return;
            const x = e.clientX - this.dragOffsetX;
            const y = e.clientY - this.dragOffsetY;
            this.element.style.left = `${x}px`;
            this.element.style.top = `${y}px`;
        });

        // Dragging - mouseup on document
        document.addEventListener('mouseup', () => {
            if (this.isDragging) {
                this.isDragging = false;
                this.header.style.cursor = 'move';
                // Save position after drag completes
                this.saveState();
            }
        });

        // Click anywhere on window to bring to front
        this.element.addEventListener('mousedown', () => {
            this.bringToFront();
        });

        // Close button
        closeBtn.addEventListener('click', () => {
            if (this.config.onClose) {
                this.config.onClose();
            }
            this.hide();
        });

        // Minimize button
        minimizeBtn.addEventListener('click', () => {
            this.minimize();
        });
    }

    /**
     * Set window content from HTML string or DOM element
     */
    public setContent(content: string | HTMLElement): void {
        if (typeof content === 'string') {
            this.contentContainer.innerHTML = content;
        } else {
            this.contentContainer.innerHTML = '';
            this.contentContainer.appendChild(content);
        }
    }

    /**
     * Get the content container element for direct manipulation
     */
    public getContentElement(): HTMLElement {
        return this.contentContainer;
    }

    /**
     * Get the footer container element for direct manipulation
     * Returns null if footer doesn't exist
     */
    public getFooterElement(): HTMLElement | null {
        return this.footerContainer;
    }

    /**
     * Set footer content from HTML string or DOM element
     * If content is empty/null, hides the footer
     */
    public setFooterContent(content: string | HTMLElement | null): void {
        if (!this.footerContainer) return;

        if (!content) {
            // Hide footer if content is empty
            this.footerContainer.style.display = 'none';
            this.footerContainer.innerHTML = '';
            return;
        }

        // Show and populate footer
        this.footerContainer.style.display = '';
        if (typeof content === 'string') {
            this.footerContainer.innerHTML = content;
        } else {
            this.footerContainer.innerHTML = '';
            this.footerContainer.appendChild(content);
        }
    }

    /**
     * Get the root window element
     */
    public getElement(): HTMLElement {
        return this.element;
    }

    /**
     * Show the window
     */
    public show(): void {
        this.element.setAttribute('data-visible', 'true');
        this.bringToFront();
        this.saveState();
        if (this.config.onShow) {
            this.config.onShow();
        }
    }

    /**
     * Hide the window
     */
    public hide(): void {
        this.element.setAttribute('data-visible', 'false');
        this.saveState();
        if (this.config.onHide) {
            this.config.onHide();
        }
    }

    /**
     * Toggle window visibility
     */
    public toggle(): void {
        const isVisible = this.element.getAttribute('data-visible') === 'true';
        if (isVisible) {
            this.hide();
        } else {
            this.show();
        }
    }

    /**
     * Check if window is visible
     */
    public isVisible(): boolean {
        return this.element.getAttribute('data-visible') === 'true';
    }

    /**
     * Check if window is minimized
     */
    public isMinimized(): boolean {
        return this.minimized;
    }

    /**
     * Restore minimized state from localStorage on window creation
     */
    private restoreMinimizedState(): void {
        const minimizedIds = windowTray.loadState();
        if (minimizedIds.includes(this.config.id)) {
            // Window was minimized in previous session - minimize it silently (no animation, no save)
            // skipSave=true prevents overwriting localStorage before all windows have restored
            this.minimize(true, true);
        }
    }

    /**
     * Save window state to localStorage
     */
    private saveState(): void {
        try {
            const allState = this.loadAllWindowState();

            // Get current position
            let x: number, y: number;
            if (!this.isVisible() && this.savedPosition) {
                // Window is hidden - use savedPosition if available (set during minimize)
                x = this.savedPosition.x;
                y = this.savedPosition.y;
            } else {
                // Window is visible - use actual bounding rect
                const rect = this.element.getBoundingClientRect();
                x = rect.left;
                y = rect.top;
            }

            allState[this.config.id] = {
                x,
                y,
                width: this.element.style.width || this.config.width || '400px',
                visible: this.isVisible(),
                minimized: this.minimized
            };

            localStorage.setItem(Window.STORAGE_KEY, JSON.stringify(allState));
        } catch (error) {
            console.warn('Failed to save window state:', error);
        }
    }

    /**
     * Restore window state from localStorage on construction
     */
    private restoreState(): void {
        try {
            const allState = this.loadAllWindowState();
            const state = allState[this.config.id];

            if (!state) return; // No saved state for this window

            // Always restore position and width (needed even for minimized windows)
            if (state.x !== undefined && state.y !== undefined) {
                // Clamp to viewport to handle window resize
                const clampedX = Math.max(0, Math.min(state.x, window.innerWidth - 100));
                const clampedY = Math.max(0, Math.min(state.y, window.innerHeight - 50));
                this.element.style.left = `${clampedX}px`;
                this.element.style.top = `${clampedY}px`;
            }

            // Restore width (user preference) but not height (should fit content)
            if (state.width) this.element.style.width = state.width;

            // Skip visibility restoration if window was minimized - restoreMinimizedState() handles it
            if (state.minimized) return;

            // Don't restore visibility during page load - it will flash above loading screen
            // Visibility will be restored after hideLoadingScreen() via finishWindowRestore()
            // Just store that we should restore it later
            if (state.visible) {
                this.element.setAttribute('data-should-restore-visibility', 'true');
            }
        } catch (error) {
            console.warn('Failed to restore window state:', error);
        }
    }

    /**
     * Load all window state from localStorage
     */
    private loadAllWindowState(): Record<string, {
        x: number;
        y: number;
        width: string;
        visible: boolean;
        minimized: boolean;
    }> {
        try {
            const stored = localStorage.getItem(Window.STORAGE_KEY);
            return stored ? JSON.parse(stored) : {};
        } catch (error) {
            console.warn('Failed to load window state:', error);
            return {};
        }
    }

    /**
     * Minimize window to tray with animation
     * @param skipAnimation Skip animation for silent minimize (e.g., restoring from localStorage)
     */
    public minimize(skipAnimation: boolean = false, skipSave: boolean = false): void {
        if (this.minimized) return;

        // Clear any existing transforms first
        this.element.style.transform = '';
        this.element.style.transition = '';

        // Save current position (after clearing transforms)
        // For hidden windows (display: none), getBoundingClientRect returns incorrect values
        // so we need to parse from inline styles instead
        const rect = this.element.getBoundingClientRect();
        if (!this.isVisible()) {
            // Window is hidden - parse position from inline styles
            const left = parseFloat(this.element.style.left) || 0;
            const top = parseFloat(this.element.style.top) || 0;
            this.savedPosition = { x: left, y: top };
        } else {
            // Window is visible - use actual bounding rect
            this.savedPosition = { x: rect.left, y: rect.top };
        }

        // Get tray target for animation
        const trayTarget = windowTray.getTargetPosition();

        // Animate to tray (unless skipping animation)
        if (trayTarget && !skipAnimation) {
            const duration = Window.ANIMATION_DURATION_MS / 1000; // Convert to seconds for CSS
            this.element.style.transition = `transform ${duration}s ease, opacity ${duration}s ease`;
            const dx = trayTarget.x - (rect.left + rect.width / 2);
            const dy = trayTarget.y - (rect.top + rect.height / 2);
            this.element.style.transform = `translate(${dx}px, ${dy}px) scale(0.1)`;
            this.element.style.opacity = '0';
        }

        // After animation, hide and add to tray
        const finishMinimize = () => {
            this.element.style.transition = '';
            this.element.style.transform = '';
            this.element.style.opacity = '';
            this.element.setAttribute('data-visible', 'false');

            this.minimized = true;

            // Save state to persist minimized status
            this.saveState();

            // Add to tray (skipSave during restore to avoid overwriting localStorage prematurely)
            windowTray.add({
                id: this.config.id,
                title: this.config.title,
                onRestore: (sourceRect?: DOMRect) => this.restore(sourceRect),
                onClose: () => {
                    if (this.config.onClose) {
                        this.config.onClose();
                    }
                    this.hide();
                }
            }, skipSave);

            if (this.config.onMinimize) {
                this.config.onMinimize();
            }
        };

        if (skipAnimation) {
            // Execute immediately for silent minimize (no animation)
            finishMinimize();
        } else {
            // Delay for animation
            setTimeout(finishMinimize, Window.ANIMATION_DURATION_MS);
        }
    }

    /**
     * Restore window from tray with animation
     * @param sourceRect Optional rect of the clicked tray item for spatial continuity
     */
    public restore(sourceRect?: DOMRect): void {
        if (!this.minimized) return;

        // Remove from tray
        windowTray.remove(this.config.id);

        // Restore position
        if (this.savedPosition) {
            this.element.style.left = `${this.savedPosition.x}px`;
            this.element.style.top = `${this.savedPosition.y}px`;
        }

        // Show window first so we can get accurate dimensions
        this.element.setAttribute('data-visible', 'true');
        this.minimized = false; // Mark as not minimized before animation starts

        // Prepare for animation - start from source rect (expanded dot) or tray position
        if (this.savedPosition) {
            const windowRect = this.element.getBoundingClientRect();
            const finalWidth = windowRect.width;
            const finalHeight = windowRect.height;

            if (sourceRect) {
                // Start from expanded dot's exact position and size
                const dx = sourceRect.left - this.savedPosition.x;
                const dy = sourceRect.top - this.savedPosition.y;
                const scaleX = sourceRect.width / finalWidth;
                const scaleY = sourceRect.height / finalHeight;
                this.element.style.transform = `translate(${dx}px, ${dy}px) scale(${scaleX}, ${scaleY})`;
                this.element.style.transformOrigin = 'top left';
                this.element.style.opacity = '1'; // Dot is visible, window should be too
            } else {
                // Fallback: use tray center position with small scale
                const trayTarget = windowTray.getTargetPosition();
                if (trayTarget) {
                    const dx = trayTarget.x - (this.savedPosition.x + finalWidth / 2);
                    const dy = trayTarget.y - (this.savedPosition.y + finalHeight / 2);
                    this.element.style.transform = `translate(${dx}px, ${dy}px) scale(0.1)`;
                    this.element.style.opacity = '0';
                }
            }
        }

        // Force reflow
        void this.element.offsetHeight;

        // Animate to original position
        const duration = Window.ANIMATION_DURATION_MS / 1000; // Convert to seconds for CSS
        this.element.style.transition = `transform ${duration}s ease, opacity ${duration}s ease`;
        this.element.style.transform = '';
        this.element.style.opacity = '';

        setTimeout(() => {
            this.element.style.transition = '';
            this.element.style.transformOrigin = ''; // Clear transform origin
            this.bringToFront();

            // Save state after animation completes and transforms are cleared
            this.saveState();

            if (this.config.onRestore) {
                this.config.onRestore();
            }
        }, Window.ANIMATION_DURATION_MS);
    }

    /**
     * Bring window to front (highest z-index)
     */
    public bringToFront(): void {
        this.element.style.zIndex = String(Window.zIndexCounter++);
    }

    /**
     * Destroy window and remove from DOM
     */
    public destroy(): void {
        // Remove from tray if minimized
        if (this.minimized) {
            windowTray.remove(this.config.id);
        }
        if (this.config.onClose) {
            this.config.onClose();
        }
        Window.openWindows.delete(this);
        this.element.remove();
    }

    /**
     * Update window title (accepts HTML)
     */
    public setTitle(title: string): void {
        const titleEl = this.header.querySelector('.draggable-window-title');
        if (titleEl) {
            titleEl.innerHTML = title;
        }
    }

    /**
     * Get all open windows
     */
    public static getOpenWindows(): Window[] {
        return Array.from(Window.openWindows);
    }

    /**
     * Close all open windows
     */
    public static closeAll(): void {
        Window.openWindows.forEach(win => win.destroy());
    }

    /**
     * Finish restoring window visibility after loading screen completes
     * Called once after hideLoadingScreen() to show windows that should be visible
     */
    public static finishWindowRestore(): void {
        Window.openWindows.forEach(win => {
            if (win.element.getAttribute('data-should-restore-visibility') === 'true') {
                win.element.removeAttribute('data-should-restore-visibility');
                win.element.setAttribute('data-visible', 'true');
            }
        });
    }
}
