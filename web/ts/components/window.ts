/**
 * Generic draggable, non-modal window component
 * Supports multiple windows with z-index stacking
 *
 * State Machine:
 * ┌─────────┐  show()   ┌─────────┐  minimize()  ┌───────────┐
 * │ Hidden  │ ────────> │ Visible │ ───────────> │ Minimized │
 * │         │ <──────── │         │ <─────────── │ (in tray) │
 * └─────────┘  hide()   └─────────┘  restore()   └───────────┘
 *      │                     │                         │
 *      └─────────────────────┴─────────────────────────┘
 *                      destroy() - permanent removal
 *
 * States:
 * - Hidden: Window exists in DOM but not visible (visible=false, minimized=false)
 * - Visible: Window displayed on screen (visible=true, minimized=false)
 * - Minimized: Hidden with tray dot for quick restore (visible=false, minimized=true)
 *
 * Operations:
 * - show(): Make visible. Auto-restores if minimized.
 * - hide(): Hide window. No-op if already minimized.
 * - minimize(): Hide to tray with animation. Adds tray dot.
 * - restore(): Bring back from tray with animation. Removes tray dot.
 * - destroy(): Permanent removal from DOM. Not reversible.
 * - toggle(): show() if hidden, hide() if visible
 *
 * Close button behavior:
 * - Calls destroy() for permanent removal
 * - Window wrappers (VidStreamWindow, etc.) can recreate via singleton pattern
 */

import { handleErrorSilent } from '../error-handler';
import { SEG } from '../logger';
import { setVisibility, getVisibility, DATA } from '../css-classes';
import { uiState } from '../state/ui';

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

        // Start hidden by default
        setVisibility(win, DATA.VISIBILITY.HIDDEN);

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

        // Click anywhere on window to bring to front (or restore if minimized)
        this.element.addEventListener('mousedown', () => {
            if (this.minimized) {
                this.restore();
            } else {
                this.bringToFront();
            }
        });

        // Close button - destroys the window permanently
        closeBtn.addEventListener('click', () => {
            this.destroy();
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
     * If window is minimized, restores it from tray instead
     */
    public show(): void {
        // Auto-restore if minimized
        if (this.minimized) {
            this.restore();
            return;
        }

        setVisibility(this.element, DATA.VISIBILITY.VISIBLE);
        this.bringToFront();
        this.saveState();
        if (this.config.onShow) {
            this.config.onShow();
        }
    }

    /**
     * Hide the window
     * No-op if window is minimized (use restore() to bring back minimized windows)
     */
    public hide(): void {
        // No-op if already minimized (already hidden via minimize)
        if (this.minimized) {
            return;
        }

        setVisibility(this.element, DATA.VISIBILITY.HIDDEN);
        this.saveState();
        if (this.config.onHide) {
            this.config.onHide();
        }
    }

    /**
     * Toggle window visibility
     */
    public toggle(): void {
        if (this.isVisible()) {
            this.hide();
        } else {
            this.show();
        }
    }

    /**
     * Check if window is visible
     */
    public isVisible(): boolean {
        return getVisibility(this.element) === DATA.VISIBILITY.VISIBLE;
    }

    /**
     * Check if window is minimized
     */
    public isMinimized(): boolean {
        return this.minimized;
    }

    /**
     * Restore minimized state from uiState on window creation
     */
    private restoreMinimizedState(): void {
        const state = uiState.getWindowState(this.config.id);
        if (state?.minimized) {
            // Window was minimized in previous session - minimize it silently (no animation)
            this.minimize(true);
        }
    }

    /**
     * Save window state to uiState
     */
    private saveState(): void {
        try {
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

            uiState.setWindowState(this.config.id, {
                x,
                y,
                width: this.element.style.width || this.config.width || '400px',
                minimized: this.minimized
            });
        } catch (error) {
            handleErrorSilent(error, 'Failed to save window state', SEG.UI);
        }
    }

    /**
     * Restore window state from uiState on construction
     */
    private restoreState(): void {
        try {
            const state = uiState.getWindowState(this.config.id);

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
            // Mark non-minimized windows to be shown after loading completes
            this.element.setAttribute('data-should-restore-visibility', 'true');
        } catch (error) {
            handleErrorSilent(error, 'Failed to restore window state', SEG.UI);
        }
    }

    /**
     * Minimize window - window element becomes the dot
     * @param skipAnimation Skip animation for silent minimize (e.g., restoring from localStorage)
     */
    public minimize(skipAnimation: boolean = false): void {
        if (this.minimized) return;

        // Save current position and size
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

        // Mark as minimized FIRST so isVisible() returns false
        this.minimized = true;

        // Add minimized class (CSS handles dot styling)
        this.element.classList.add('minimized');

        // Calculate tray position (bottom-right corner with spacing)
        const trayX = window.innerWidth - 100;
        const trayY = window.innerHeight - 50;

        // Reposition to tray
        this.element.style.left = `${trayX}px`;
        this.element.style.top = `${trayY}px`;

        // Save state to persist minimized status
        this.saveState();

        if (this.config.onMinimize) {
            this.config.onMinimize();
        }
    }

    /**
     * Restore window from minimized state
     */
    public restore(): void {
        if (!this.minimized) return;

        // Mark as not minimized FIRST so isVisible() returns true
        this.minimized = false;

        // Remove minimized class
        this.element.classList.remove('minimized');

        // Restore position
        if (this.savedPosition) {
            this.element.style.left = `${this.savedPosition.x}px`;
            this.element.style.top = `${this.savedPosition.y}px`;
        }

        this.bringToFront();

        // Save state
        this.saveState();

        if (this.config.onRestore) {
            this.config.onRestore();
        }
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
                setVisibility(win.element, DATA.VISIBILITY.VISIBLE);
            }
        });
    }
}
