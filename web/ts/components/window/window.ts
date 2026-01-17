/**
 * Generic draggable, non-modal window component
 * Supports multiple windows with z-index stacking
 */

import styles from './window.module.css';

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
}

export class Window {
    private element: HTMLElement;
    private header: HTMLElement;
    private contentContainer: HTMLElement;
    private footerContainer: HTMLElement | null = null;
    private config: WindowConfig;
    private closeBtn: HTMLButtonElement;

    // Drag state
    private isDragging: boolean = false;
    private dragOffsetX: number = 0;
    private dragOffsetY: number = 0;

    // Global window management
    private static zIndexCounter: number = 9999;
    private static openWindows: Set<Window> = new Set();
    private static readonly CASCADE_OFFSET = 30; // px offset for cascading windows

    constructor(config: WindowConfig) {
        this.config = config;
        this.element = this.createElement();
        this.header = this.element.querySelector(`.${styles.header}`) as HTMLElement;
        this.contentContainer = this.element.querySelector(`.${styles.content}`) as HTMLElement;
        this.footerContainer = this.element.querySelector(`.${styles.footer}`) as HTMLElement | null;
        this.closeBtn = this.element.querySelector(`.${styles.close}`) as HTMLButtonElement;

        document.body.appendChild(this.element);
        this.setupEventListeners();
        Window.openWindows.add(this);
    }

    private createElement(): HTMLElement {
        const win = document.createElement('div');
        win.id = this.config.id;
        win.className = styles.window;

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

        // Build DOM structure
        const header = document.createElement('div');
        header.className = styles.header;

        const title = document.createElement('span');
        title.className = styles.title;
        title.textContent = this.config.title;

        const closeBtn = document.createElement('button');
        closeBtn.className = styles.close;
        closeBtn.setAttribute('aria-label', 'Close');
        closeBtn.textContent = '×';

        header.appendChild(title);
        header.appendChild(closeBtn);

        const content = document.createElement('div');
        content.className = styles.content;

        const footer = document.createElement('div');
        footer.className = styles.footer;

        win.appendChild(header);
        win.appendChild(content);
        win.appendChild(footer);

        return win;
    }

    private setupEventListeners(): void {
        // Dragging - mousedown on header
        this.header.addEventListener('mousedown', (e) => {
            // Ignore if clicking close button
            if ((e.target as HTMLElement).closest(`.${styles.close}`)) return;

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
        this.closeBtn.addEventListener('click', () => {
            if (this.config.onClose) {
                this.config.onClose();
            }
            this.hide();
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
     * Bring window to front (highest z-index)
     */
    public bringToFront(): void {
        this.element.style.zIndex = String(Window.zIndexCounter++);
    }

    /**
     * Destroy window and remove from DOM
     */
    public destroy(): void {
        if (this.config.onClose) {
            this.config.onClose();
        }
        Window.openWindows.delete(this);
        this.element.remove();
    }

    /**
     * Update window title
     */
    public setTitle(title: string): void {
        const titleEl = this.header.querySelector(`.${styles.title}`);
        if (titleEl) {
            titleEl.textContent = title;
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
     * Expose styles for consumers that need to add window-specific classes
     */
    public static get styles() {
        return styles;
    }
}
