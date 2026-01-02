/**
 * BasePanel - Abstract base class for slide-in panels
 *
 * Handles common lifecycle, visibility, and event patterns:
 * - Overlay creation (optional)
 * - Show/hide/toggle with CSS class management
 * - Close button, escape key, click-outside handlers
 * - DOM insertion strategies
 *
 * Subclasses implement:
 * - getTemplate(): HTML content
 * - setupEventListeners(): custom handlers
 * - onShow()/onHide(): lifecycle hooks
 */

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
        overlay.className = `panel-overlay ${this.config.id}-overlay`;

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
        // Close button
        const closeBtn = this.panel?.querySelector('[class*="close"]');
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

        this.panel.classList.add('visible');
        this.panel.classList.remove('hidden');
        this.overlay?.classList.add('visible');
        this.overlay?.classList.remove('hidden');
        this.isVisible = true;

        await this.onShow();
    }

    public hide(): void {
        if (!this.panel) return;

        // Allow subclass to prevent hide (e.g., unsaved changes prompt)
        if (!this.beforeHide()) return;

        this.panel.classList.remove('visible');
        this.panel.classList.add('hidden');
        this.overlay?.classList.remove('visible');
        this.overlay?.classList.add('hidden');
        this.isVisible = false;

        this.onHide();
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
}
