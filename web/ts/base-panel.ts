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

import { CSS, DATA, setVisibility } from './css-classes.ts';
import * as PanelError from './base-panel-error.ts';
import type { PanelErrorState, ErrorHandlingContext } from './base-panel-error.ts';
import { tooltip as tooltipManager, type TooltipConfig } from './components/tooltip.ts';
import { log, SEG } from './logger.ts';

export interface PanelConfig {
    id: string;
    classes?: string[];
    useOverlay?: boolean;
    closeOnEscape?: boolean;
    closeOnOverlayClick?: boolean;
    insertAfter?: string;  // Selector, e.g., '#symbolPalette'
    /** Enable interactive tooltips for elements with data-tooltip attribute */
    enableTooltips?: boolean;
    /** Custom tooltip configuration */
    tooltipConfig?: TooltipConfig;
    /** Panel slides from right instead of left */
    slideFromRight?: boolean;
}

export abstract class BasePanel {
    protected panel: HTMLElement | null = null;
    protected overlay: HTMLElement | null = null;
    protected isVisible: boolean = false;
    protected isFullscreen: boolean = false;
    protected config: Required<Omit<PanelConfig, 'tooltipConfig'>> & { tooltipConfig?: TooltipConfig };

    /** Error state management */
    protected errorState: PanelErrorState = {
        hasError: false,
        lastError: null
    };

    private escapeHandler: ((e: KeyboardEvent) => void) | null = null;
    private clickOutsideHandler: ((e: Event) => void) | null = null;
    private tooltipCleanup: (() => void) | null = null;
    private fullscreenHandler: ((e: KeyboardEvent) => void) | null = null;

    constructor(config: PanelConfig) {
        this.config = {
            classes: ['panel-slide-left'],
            useOverlay: true,
            closeOnEscape: true,
            closeOnOverlayClick: true,
            insertAfter: '',
            enableTooltips: true, // Enable by default for observability
            slideFromRight: false,
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

        // Add expand button AFTER setting template
        this.createExpandButton(this.panel);

        // Insert into DOM
        this.insertPanel();

        // Attach common handlers
        this.attachCommonListeners();

        // Setup tooltips if enabled
        if (this.config.enableTooltips && this.panel) {
            this.setupTooltips();
        }

        // Error boundary: wrap setupEventListeners() to catch initialization errors
        try {
            // Subclass custom setup
            this.setupEventListeners();
        } catch (error: unknown) {
            const err = error instanceof Error ? error : new Error(String(error));
            log.error(SEG.UI, `[${this.config.id}] Error in setupEventListeners():`, err);
            // Log error but allow panel to be created - it may still be partially functional
        }
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
        // Start in normal panel mode
        panel.setAttribute('data-mode', 'panel');
        return panel;
    }

    /**
     * Create the expand/collapse button on the edge of the panel
     */
    protected createExpandButton(panel: HTMLElement): void {
        const expandBtn = document.createElement('button');
        expandBtn.className = 'panel-expand-btn';
        expandBtn.setAttribute('aria-label', 'Expand panel');
        expandBtn.setAttribute('title', 'Expand panel');
        // No text content - button shape is created with CSS borders
        expandBtn.addEventListener('click', () => this.toggleFullscreen());
        panel.appendChild(expandBtn);
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

        // Fullscreen button
        const fullscreenBtn = this.panel?.querySelector('.panel-fullscreen-toggle');
        fullscreenBtn?.addEventListener('click', () => this.toggleFullscreen());

        // Escape key
        if (this.config.closeOnEscape) {
            this.escapeHandler = (e: KeyboardEvent) => {
                if (e.key === 'Escape' && this.isVisible) {
                    this.hide();
                }
            };
            document.addEventListener('keydown', this.escapeHandler);
        }

        // F11 key for fullscreen toggle
        this.fullscreenHandler = (e: KeyboardEvent) => {
            if (e.key === 'F11' && this.isVisible) {
                e.preventDefault();
                this.toggleFullscreen();
            }
        };
        document.addEventListener('keydown', this.fullscreenHandler);

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

        // Error boundary: wrap beforeShow() to catch and display errors
        try {
            // Allow subclass to prevent show (e.g., unsaved changes check)
            if (!await this.beforeShow()) return;
        } catch (error: unknown) {
            const err = error instanceof Error ? error : new Error(String(error));
            log.error(SEG.UI, `[${this.config.id}] Error in beforeShow():`, err);
            this.showErrorState(err);
            return;
        }

        this.updateVisibility(true);

        // Error boundary: wrap onShow() to catch and display errors
        try {
            this.clearError();
            await this.onShow();
        } catch (error: unknown) {
            const err = error instanceof Error ? error : new Error(String(error));
            log.error(SEG.UI, `[${this.config.id}] Error in onShow():`, err);
            this.showErrorState(err);
        }
    }

    public hide(): void {
        if (!this.panel) return;

        // Error boundary: wrap beforeHide() to catch errors
        try {
            // Allow subclass to prevent hide (e.g., unsaved changes prompt)
            if (!this.beforeHide()) return;
        } catch (error: unknown) {
            const err = error instanceof Error ? error : new Error(String(error));
            log.error(SEG.UI, `[${this.config.id}] Error in beforeHide():`, err);
            // Don't show error state during hide, just log and continue
        }

        this.updateVisibility(false);

        // Error boundary: wrap onHide() to catch errors
        try {
            this.onHide();
        } catch (error: unknown) {
            const err = error instanceof Error ? error : new Error(String(error));
            log.error(SEG.UI, `[${this.config.id}] Error in onHide():`, err);
            // Don't show error state during hide, just log
        }
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

    /**
     * Toggle fullscreen mode for the panel
     */
    public toggleFullscreen(): void {
        if (!this.panel) return;

        this.isFullscreen = !this.isFullscreen;
        this.panel.setAttribute('data-mode', this.isFullscreen ? 'fullscreen' : 'panel');

        // Update expand button tooltip
        const expandBtn = this.panel.querySelector('.panel-expand-btn');
        if (expandBtn) {
            // CSS handles the arrow shape change
            expandBtn.setAttribute('aria-label', this.isFullscreen ? 'Collapse panel' : 'Expand panel');
            expandBtn.setAttribute('title', this.isFullscreen ? 'Collapse panel' : 'Expand panel');
        }

        // Update fullscreen button icon/state (if exists in header)
        const fullscreenBtn = this.panel.querySelector('.panel-fullscreen-toggle');
        if (fullscreenBtn) {
            fullscreenBtn.textContent = this.isFullscreen ? '⊗' : '⛶';
            fullscreenBtn.setAttribute('aria-label', this.isFullscreen ? 'Exit fullscreen' : 'Enter fullscreen');
        }

        // Hide overlay in fullscreen mode for better visibility
        if (this.overlay && this.config.useOverlay) {
            setVisibility(this.overlay, this.isFullscreen ? DATA.VISIBILITY.HIDDEN : DATA.VISIBILITY.VISIBLE);
        }

        log(SEG.UI, `[${this.config.id}] Toggled fullscreen: ${this.isFullscreen}`);
    }

    public destroy(): void {
        this.onDestroy();

        if (this.escapeHandler) {
            document.removeEventListener('keydown', this.escapeHandler);
        }
        if (this.clickOutsideHandler) {
            document.removeEventListener('click', this.clickOutsideHandler);
        }
        if (this.fullscreenHandler) {
            document.removeEventListener('keydown', this.fullscreenHandler);
        }
        if (this.tooltipCleanup) {
            this.tooltipCleanup();
            this.tooltipCleanup = null;
        }

        this.panel?.remove();
        this.overlay?.remove();
        this.panel = null;
        this.overlay = null;
    }

    /**
     * Setup interactive tooltips for elements with data-tooltip attribute
     * Uses the shared tooltip manager for consistent styling
     */
    protected setupTooltips(): void {
        if (!this.panel) return;

        // Use 'has-tooltip' class by default, can be customized via config
        const selector = this.config.tooltipConfig?.triggerClass
            ? `.${this.config.tooltipConfig.triggerClass}`
            : '.has-tooltip';

        this.tooltipCleanup = tooltipManager.attach(this.panel, selector);
    }

    /**
     * Manually refresh tooltip bindings
     * Useful after dynamic content updates that add new tooltip elements
     */
    protected refreshTooltips(): void {
        if (this.tooltipCleanup) {
            this.tooltipCleanup();
        }
        this.setupTooltips();
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

    /**
     * Get error handling context for delegating to error module
     */
    private getErrorContext(): ErrorHandlingContext {
        return {
            panelId: this.config.id,
            errorState: this.errorState,
            $: this.$.bind(this),
            onShow: this.onShow.bind(this)
        };
    }

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
        options: { includeClose?: boolean; includeFullscreen?: boolean; className?: string } = {}
    ): HTMLElement {
        const { includeClose = true, includeFullscreen = true, className = '' } = options;

        const header = document.createElement('div');
        header.className = `${CSS.PANEL.HEADER}${className ? ` ${className}` : ''}`;

        const titleEl = document.createElement('h3');
        titleEl.className = CSS.PANEL.TITLE;
        titleEl.textContent = title;
        header.appendChild(titleEl);

        // Create action buttons container
        const actions = document.createElement('div');
        actions.className = 'panel-header-actions';

        if (includeFullscreen) {
            const fullscreenBtn = document.createElement('button');
            fullscreenBtn.className = 'panel-fullscreen-toggle';
            fullscreenBtn.setAttribute('aria-label', 'Enter fullscreen');
            fullscreenBtn.setAttribute('title', 'Toggle fullscreen (F11)');
            fullscreenBtn.setAttribute('type', 'button');
            fullscreenBtn.textContent = '⛶';
            actions.appendChild(fullscreenBtn);
        }

        if (includeClose) {
            actions.appendChild(this.createCloseButton());
        }

        if (actions.children.length > 0) {
            header.appendChild(actions);
        }

        return header;
    }

    /**
     * Create a loading state element
     * @param message Optional loading message (defaults to "Loading...")
     */
    protected createLoadingState(message: string = 'Loading...'): HTMLElement {
        return PanelError.createLoadingState(message);
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
        return PanelError.createErrorState(title, message, onRetry);
    }

    // =========================================================================
    // Error Boundary Methods (delegated to base-panel-error module)
    // =========================================================================

    /**
     * Display an error state in the panel content area
     * Called automatically when onShow() throws, or can be called manually
     * @param error The error to display
     */
    protected showErrorState(error: Error): void {
        PanelError.showErrorState(this.getErrorContext(), error);
    }

    /**
     * Clear the error state
     * Called automatically before onShow(), or can be called manually
     */
    protected clearError(): void {
        PanelError.clearError(this.getErrorContext());
    }

    /**
     * Retry showing the panel after an error
     * Clears error state and calls show() again
     */
    protected async retryShow(): Promise<void> {
        await PanelError.retryShow(this.getErrorContext());
    }

    /**
     * Show loading state in the panel content area
     * @param message Optional loading message
     */
    protected showLoading(message?: string): void {
        PanelError.showLoading(this.getErrorContext(), message);
    }

    /**
     * Hide loading state (resets to idle)
     */
    protected hideLoading(): void {
        PanelError.hideLoading(this.getErrorContext());
    }

    /** Whether the panel is currently in an error state */
    protected get hasError(): boolean {
        return this.errorState.hasError;
    }

    /** The last error that occurred during lifecycle methods */
    protected get lastError(): Error | null {
        return this.errorState.lastError;
    }
}
