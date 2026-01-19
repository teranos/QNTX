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

    constructor(config: WindowConfig) {
        this.config = config;
        this.element = this.createElement();
        this.header = this.element.querySelector('.draggable-window-header') as HTMLElement;
        this.contentContainer = this.element.querySelector('.draggable-window-content') as HTMLElement;
        this.footerContainer = this.element.querySelector('.draggable-window-footer') as HTMLElement | null;

        document.body.appendChild(this.element);
        this.setupEventListeners();
        Window.openWindows.add(this);

        // Check if this window was previously minimized
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
        if (this.config.onShow) {
            this.config.onShow();
        }
    }

    /**
     * Hide the window
     */
    public hide(): void {
        this.element.setAttribute('data-visible', 'false');
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
            // Window was minimized in previous session - minimize it silently
            this.minimize();
        }
    }

    /**
     * Minimize window to tray with animation
     */
    public minimize(): void {
        if (this.minimized) return;

        // Save current position
        const rect = this.element.getBoundingClientRect();
        this.savedPosition = { x: rect.left, y: rect.top };

        // Get tray target for animation
        const trayTarget = windowTray.getTargetPosition();

        // Animate to tray
        if (trayTarget) {
            this.element.style.transition = 'transform 0.2s ease, opacity 0.2s ease';
            const dx = trayTarget.x - (rect.left + rect.width / 2);
            const dy = trayTarget.y - (rect.top + rect.height / 2);
            this.element.style.transform = `translate(${dx}px, ${dy}px) scale(0.1)`;
            this.element.style.opacity = '0';
        }

        // After animation, hide and add to tray
        setTimeout(() => {
            this.element.style.transition = '';
            this.element.style.transform = '';
            this.element.style.opacity = '';
            this.element.setAttribute('data-visible', 'false');

            this.minimized = true;

            // Add to tray
            windowTray.add({
                id: this.config.id,
                title: this.config.title,
                onRestore: () => this.restore(),
                onClose: () => {
                    if (this.config.onClose) {
                        this.config.onClose();
                    }
                    this.hide();
                }
            });

            if (this.config.onMinimize) {
                this.config.onMinimize();
            }
        }, 200);
    }

    /**
     * Restore window from tray with animation
     */
    public restore(): void {
        if (!this.minimized) return;

        // Remove from tray
        windowTray.remove(this.config.id);

        // Restore position
        if (this.savedPosition) {
            this.element.style.left = `${this.savedPosition.x}px`;
            this.element.style.top = `${this.savedPosition.y}px`;
        }

        // Prepare for animation - start from tray position
        const trayTarget = windowTray.getTargetPosition();
        if (trayTarget && this.savedPosition) {
            const rect = this.element.getBoundingClientRect();
            const dx = trayTarget.x - (this.savedPosition.x + rect.width / 2);
            const dy = trayTarget.y - (this.savedPosition.y + rect.height / 2);
            this.element.style.transform = `translate(${dx}px, ${dy}px) scale(0.1)`;
            this.element.style.opacity = '0';
        }

        // Show and animate back
        this.element.setAttribute('data-visible', 'true');

        // Force reflow
        void this.element.offsetHeight;

        // Animate to original position
        this.element.style.transition = 'transform 0.2s ease, opacity 0.2s ease';
        this.element.style.transform = '';
        this.element.style.opacity = '';

        setTimeout(() => {
            this.element.style.transition = '';
            this.minimized = false;
            this.bringToFront();

            if (this.config.onRestore) {
                this.config.onRestore();
            }
        }, 200);
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
}
