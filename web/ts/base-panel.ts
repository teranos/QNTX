/**
 * BasePanel - Abstract base class for slide-in panels
 *
 * Handles common lifecycle, visibility, and event patterns:
 * - Overlay creation (optional)
 * - Show/hide/toggle with CSS class management
 * - Close button, escape key, click-outside handlers
 * - DOM insertion strategies
 * - Error boundaries with recovery support
 * - Reusable DOM element builders
 *
 * Subclasses implement:
 * - getTemplate(): HTML content
 * - setupEventListeners(): custom handlers
 * - onShow()/onHide(): lifecycle hooks
 *
 * Configuration guidelines:
 * - useOverlay: true (default) for modal-style panels that dim the rest of the UI
 * - useOverlay: false for inline panels that integrate with the page (click-outside closes)
 * - insertAfter: specify a selector to insert panel after a specific element (e.g., '#symbolPalette')
 * - insertAfter: '' (default) appends panel to document.body
 *
 * DOM Helpers (use in getTemplate() or dynamically):
 * - createCloseButton(): Standard accessible close button
 * - createHeader(title): Header with title and close button
 * - createLoadingState(message): Loading indicator
 * - createEmptyState(title, hint): Empty state placeholder
 * - createErrorState(title, message, onRetry): Error with optional retry
 *
 * Error Boundaries:
 * - Errors in onShow() are caught and displayed via showErrorState()
 * - Use hasError to check current error status
 * - Call clearError() to reset error state before retry
 */

import { CSS, DATA, setVisibility, setLoading } from './css-classes.ts';

export interface PanelConfig {
    id: string;
    classes?: string[];
    useOverlay?: boolean;
    closeOnEscape?: boolean;
    closeOnOverlayClick?: boolean;
    insertAfter?: string;  // Selector, e.g., '#symbolPalette'
}

export abstract class BasePanel {
    protected panel: HTMLElement | null = null;
    protected overlay: HTMLElement | null = null;
    protected isVisible: boolean = false;
    protected config: Required<PanelConfig>;

    /** Whether the panel is currently in an error state */
    protected hasError: boolean = false;
    /** The last error that occurred during lifecycle methods */
    protected lastError: Error | null = null;

    private escapeHandler: ((e: KeyboardEvent) => void) | null = null;
    private clickOutsideHandler: ((e: Event) => void) | null = null;

    constructor(config: PanelConfig) {
        this.config = {
            classes: ['panel-slide-left'],
            useOverlay: true,
            closeOnEscape: true,
            closeOnOverlayClick: true,
            insertAfter: '',
            ...config
        };
        this.initialize();
    }

    protected initialize(): void {
        // Create overlay if configured
        if (this.config.useOverlay) {
            this.overlay = this.createOverlay();
        }

        // Create panel
        this.panel = this.createPanel();
        this.panel.innerHTML = this.getTemplate();

        // Insert into DOM
        this.insertPanel();

        // Attach common handlers
        this.attachCommonListeners();

        // Subclass custom setup
        this.setupEventListeners();
    }

    protected createOverlay(): HTMLElement {
        const overlay = document.createElement('div');
        overlay.className = `${CSS.PANEL.OVERLAY} ${this.config.id}-overlay`;
        // Start overlays hidden by default using data attribute (issue #114)
        setVisibility(overlay, DATA.VISIBILITY.HIDDEN);

        if (this.config.closeOnOverlayClick) {
            overlay.addEventListener('click', () => this.hide());
        }

        document.body.appendChild(overlay);
        return overlay;
    }

    protected createPanel(): HTMLElement {
        const panel = document.createElement('div');
        panel.id = this.config.id;
        panel.className = this.config.classes.join(' ');
        // Start panels hidden by default using data attribute (issue #114)
        setVisibility(panel, DATA.VISIBILITY.HIDDEN);
        return panel;
    }

    protected insertPanel(): void {
        if (!this.panel) return;

        if (this.config.insertAfter) {
            const target = document.querySelector(this.config.insertAfter);
            if (target?.parentNode) {
                target.parentNode.insertBefore(this.panel, target.nextSibling);
                return;
            }
        }
        document.body.appendChild(this.panel);
    }

    private attachCommonListeners(): void {
        // Close button - using specific class selector for safety
        const closeBtn = this.panel?.querySelector(`.${CSS.PANEL.CLOSE}`);
        closeBtn?.addEventListener('click', () => this.hide());

        // Escape key
        if (this.config.closeOnEscape) {
            this.escapeHandler = (e: KeyboardEvent) => {
                if (e.key === 'Escape' && this.isVisible) {
                    this.hide();
                }
            };
            document.addEventListener('keydown', this.escapeHandler);
        }

        // Click outside (for panels without overlay)
        if (!this.config.useOverlay) {
            this.clickOutsideHandler = (e: Event) => {
                const target = e.target as HTMLElement;
                if (
                    this.isVisible &&
                    this.panel &&
                    !this.panel.contains(target) &&
                    !target.closest(`[data-cmd]`)
                ) {
                    this.hide();
                }
            };
            document.addEventListener('click', this.clickOutsideHandler);
        }
    }

    public async show(): Promise<void> {
        if (!this.panel) return;

        // Allow subclass to prevent show (e.g., unsaved changes check)
        if (!await this.beforeShow()) return;

        this.updateVisibility(true);

        // Error boundary: wrap onShow() to catch and display errors
        try {
            this.clearError();
            await this.onShow();
        } catch (error) {
            const err = error instanceof Error ? error : new Error(String(error));
            console.error(`[${this.config.id}] Error in onShow():`, err);
            this.showErrorState(err);
        }
    }

    public hide(): void {
        if (!this.panel) return;

        // Allow subclass to prevent hide (e.g., unsaved changes prompt)
        if (!this.beforeHide()) return;

        this.updateVisibility(false);
        this.onHide();
    }

    /**
     * Set visibility state for panel and overlay
     * Uses data-visibility attribute for cleaner state management (issue #114)
     */
    protected updateVisibility(visible: boolean): void {
        const state = visible ? DATA.VISIBILITY.VISIBLE : DATA.VISIBILITY.HIDDEN;
        setVisibility(this.panel, state);
        setVisibility(this.overlay, state);
        this.isVisible = visible;
    }

    public toggle(): void {
        if (this.isVisible) {
            this.hide();
        } else {
            this.show();
        }
    }

    public destroy(): void {
        this.onDestroy();

        if (this.escapeHandler) {
            document.removeEventListener('keydown', this.escapeHandler);
        }
        if (this.clickOutsideHandler) {
            document.removeEventListener('click', this.clickOutsideHandler);
        }

        this.panel?.remove();
        this.overlay?.remove();
        this.panel = null;
        this.overlay = null;
    }

    // Utility methods
    protected $<T extends Element = Element>(selector: string): T | null {
        return this.panel?.querySelector(selector) ?? null;
    }

    protected $$<T extends Element = Element>(selector: string): T[] {
        return this.panel ? Array.from(this.panel.querySelectorAll(selector)) : [];
    }

    // Abstract - must implement
    protected abstract getTemplate(): string;
    protected abstract setupEventListeners(): void;

    // Hooks - override as needed
    protected async beforeShow(): Promise<boolean> { return true; }
    protected beforeHide(): boolean { return true; }
    protected async onShow(): Promise<void> {}
    protected onHide(): void {}
    protected onDestroy(): void {}

    // =========================================================================
    // DOM Helper Methods
    // =========================================================================

    /**
     * Create a standard close button with accessibility attributes
     * @param onClick Optional click handler (defaults to this.hide())
     */
    protected createCloseButton(onClick?: () => void): HTMLButtonElement {
        const btn = document.createElement('button');
        btn.className = CSS.PANEL.CLOSE;
        btn.setAttribute('aria-label', 'Close');
        btn.setAttribute('type', 'button');
        btn.textContent = '✕';
        btn.addEventListener('click', (e) => {
            e.preventDefault();
            if (onClick) {
                onClick();
            } else {
                this.hide();
            }
        });
        return btn;
    }

    /**
     * Create a standard panel header with title and close button
     * @param title The header title text
     * @param options Optional configuration
     */
    protected createHeader(
        title: string,
        options: { includeClose?: boolean; className?: string } = {}
    ): HTMLElement {
        const { includeClose = true, className = '' } = options;

        const header = document.createElement('div');
        header.className = `${CSS.PANEL.HEADER}${className ? ` ${className}` : ''}`;

        const titleEl = document.createElement('h3');
        titleEl.className = CSS.PANEL.TITLE;
        titleEl.textContent = title;
        header.appendChild(titleEl);

        if (includeClose) {
            header.appendChild(this.createCloseButton());
        }

        return header;
    }

    /**
     * Create a loading state element
     * @param message Optional loading message (defaults to "Loading...")
     */
    protected createLoadingState(message: string = 'Loading...'): HTMLElement {
        const container = document.createElement('div');
        container.className = CSS.PANEL.LOADING;
        container.setAttribute('role', 'status');
        container.setAttribute('aria-live', 'polite');

        const text = document.createElement('p');
        text.textContent = message;
        container.appendChild(text);

        return container;
    }

    /**
     * Create an empty state element
     * @param title Primary empty state message
     * @param hint Optional secondary hint text
     */
    protected createEmptyState(title: string, hint?: string): HTMLElement {
        const container = document.createElement('div');
        container.className = CSS.PANEL.EMPTY;

        const titleEl = document.createElement('p');
        titleEl.textContent = title;
        container.appendChild(titleEl);

        if (hint) {
            const hintEl = document.createElement('p');
            hintEl.className = 'panel-empty-hint';
            hintEl.textContent = hint;
            container.appendChild(hintEl);
        }

        return container;
    }

    /**
     * Create an error state element with optional retry button
     * @param title Error title
     * @param message Error message/details
     * @param onRetry Optional retry callback - if provided, shows retry button
     */
    protected createErrorState(
        title: string,
        message: string,
        onRetry?: () => void
    ): HTMLElement {
        const container = document.createElement('div');
        container.className = CSS.PANEL.ERROR;
        container.setAttribute('role', 'alert');

        const titleEl = document.createElement('p');
        titleEl.className = 'panel-error-title';
        titleEl.textContent = title;
        container.appendChild(titleEl);

        const messageEl = document.createElement('p');
        messageEl.className = 'panel-error-message';
        messageEl.textContent = message;
        container.appendChild(messageEl);

        if (onRetry) {
            const retryBtn = document.createElement('button');
            retryBtn.className = 'panel-error-retry';
            retryBtn.setAttribute('type', 'button');
            retryBtn.textContent = 'Retry';
            retryBtn.addEventListener('click', (e) => {
                e.preventDefault();
                onRetry();
            });
            container.appendChild(retryBtn);
        }

        return container;
    }

    // =========================================================================
    // Error Boundary Methods
    // =========================================================================

    /**
     * Display an error state in the panel content area
     * Called automatically when onShow() throws, or can be called manually
     * @param error The error to display
     */
    protected showErrorState(error: Error): void {
        this.hasError = true;
        this.lastError = error;

        const content = this.$<HTMLElement>(`.${CSS.PANEL.CONTENT}`);
        if (!content) {
            console.warn(`[${this.config.id}] No .${CSS.PANEL.CONTENT} element found for error display`);
            return;
        }

        // Clear existing content and show error
        content.innerHTML = '';
        const errorEl = this.createErrorState(
            'Something went wrong',
            error.message,
            () => this.retryShow()
        );
        content.appendChild(errorEl);

        // Set loading state to error for CSS styling
        setLoading(content, DATA.LOADING.ERROR);
    }

    /**
     * Clear the error state
     * Called automatically before onShow(), or can be called manually
     */
    protected clearError(): void {
        this.hasError = false;
        this.lastError = null;

        const content = this.$<HTMLElement>(`.${CSS.PANEL.CONTENT}`);
        if (content) {
            setLoading(content, DATA.LOADING.IDLE);
        }
    }

    /**
     * Retry showing the panel after an error
     * Clears error state and calls show() again
     */
    protected async retryShow(): Promise<void> {
        this.clearError();
        // Clear content before retry
        const content = this.$<HTMLElement>(`.${CSS.PANEL.CONTENT}`);
        if (content) {
            content.innerHTML = '';
            content.appendChild(this.createLoadingState());
        }
        // Re-run onShow with error boundary
        try {
            await this.onShow();
        } catch (error) {
            const err = error instanceof Error ? error : new Error(String(error));
            console.error(`[${this.config.id}] Error in retryShow():`, err);
            this.showErrorState(err);
        }
    }

    /**
     * Show loading state in the panel content area
     * @param message Optional loading message
     */
    protected showLoading(message?: string): void {
        const content = this.$<HTMLElement>(`.${CSS.PANEL.CONTENT}`);
        if (!content) return;

        content.innerHTML = '';
        content.appendChild(this.createLoadingState(message));
        setLoading(content, DATA.LOADING.LOADING);
    }

    /**
     * Hide loading state (resets to idle)
     */
    protected hideLoading(): void {
        const content = this.$<HTMLElement>(`.${CSS.PANEL.CONTENT}`);
        if (content) {
            setLoading(content, DATA.LOADING.IDLE);
        }
    }
}
