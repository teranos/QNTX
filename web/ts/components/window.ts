/**
 * Generic draggable, non-modal window component
 * Supports multiple windows with z-index stacking
 */

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
    private config: WindowConfig;

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
        this.header = this.element.querySelector('.draggable-window-header') as HTMLElement;
        this.contentContainer = this.element.querySelector('.draggable-window-content') as HTMLElement;

        document.body.appendChild(this.element);
        this.setupEventListeners();
        Window.openWindows.add(this);
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
                <button class="panel-close" aria-label="Close">&times;</button>
            </div>
            <div class="draggable-window-content"></div>
        `;

        return win;
    }

    private setupEventListeners(): void {
        const closeBtn = this.element.querySelector('.panel-close') as HTMLButtonElement;

        // Dragging - mousedown on header
        this.header.addEventListener('mousedown', (e) => {
            // Ignore if clicking close button
            if ((e.target as HTMLElement).closest('.panel-close')) return;

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
        const titleEl = this.header.querySelector('.draggable-window-title');
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
}
