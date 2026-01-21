/**
 * Dot - Primary UI Primitive
 *
 * The dot is the fundamental unit of QNTX UI. Everything that can be interacted
 * with (features, windows, panels) is represented as a dot that can expand.
 *
 * Design Philosophy:
 * - The dot IS the thing (like macOS dock icons)
 * - Windows are temporary expanded views
 * - Dots persist and maintain identity
 * - Unifies Symbol Palette and WindowTray into single system
 *
 * Visual States:
 * - Idle: 8px circle (like WindowTray dots)
 * - Proximity: Morphs 8px → 220px with text (preserves WindowTray morphing)
 * - Expanded: Full Window component visible
 */

import { Window, type WindowConfig } from './window';
import { uiState } from '../state/ui';
import { log, SEG } from '../logger';

export interface DotConfig {
    id: string;
    symbol: string;  // Unicode symbol or emoji
    title: string;
    tooltip?: string;

    // Window configuration (if this dot expands to a window)
    windowConfig?: Omit<WindowConfig, 'id' | 'title'>;

    // Panel configuration (if this dot expands to a panel)
    // TODO: Will be added when migrating BasePanel

    // Callbacks
    onExpand?: () => void;
    onClick?: () => void;  // Custom click handler (overrides default expand)
    onCollapse?: () => void;
}

export interface DotState {
    id: string;
    expanded: boolean;
    position?: { x: number; y: number };  // Only relevant when expanded as window
}

/**
 * Dot - The primary primitive
 */
export class Dot {
    private config: DotConfig;
    private element: HTMLElement | null = null;
    private window: Window | null = null;
    private expanded: boolean = false;

    constructor(config: DotConfig) {
        this.config = config;
        this.loadState();
    }

    /**
     * Create the dot's DOM element (8px circle)
     * Called by Dock during rendering
     */
    public createElement(): HTMLElement {
        if (this.element) return this.element;

        const dot = document.createElement('div');
        dot.className = 'dock-dot';
        dot.setAttribute('data-dot-id', this.config.id);

        if (this.config.tooltip) {
            dot.setAttribute('data-tooltip', this.config.tooltip);
            dot.classList.add('has-tooltip');
        }

        // Click handler
        dot.addEventListener('click', (e) => {
            e.stopPropagation();
            this.handleClick();
        });

        this.element = dot;
        return dot;
    }

    /**
     * Get dot's DOM element
     */
    public getElement(): HTMLElement | null {
        return this.element;
    }

    /**
     * Handle dot click - expand or collapse
     */
    private handleClick(): void {
        // Custom click handler overrides default behavior
        if (this.config.onClick) {
            this.config.onClick();
            return;
        }

        // Default: toggle expand/collapse
        if (this.expanded) {
            this.collapse();
        } else {
            this.expand();
        }
    }

    /**
     * Expand dot to window
     */
    public expand(): void {
        if (this.expanded) return;

        log.debug(SEG.UI, `Expanding dot: ${this.config.id}`);

        // Create window if needed
        if (!this.window && this.config.windowConfig) {
            this.window = new Window({
                id: this.config.id,
                title: this.config.title,
                ...this.config.windowConfig,
                onClose: () => this.handleWindowClose(),
            });
        }

        // Show window
        if (this.window) {
            this.window.show();
        }

        this.expanded = true;
        this.element?.classList.add('expanded');
        this.saveState();

        // Callback
        this.config.onExpand?.();
    }

    /**
     * Collapse window back to dot
     */
    public collapse(): void {
        if (!this.expanded) return;

        log.debug(SEG.UI, `Collapsing dot: ${this.config.id}`);

        // Hide window (but don't destroy - dot owns it)
        if (this.window) {
            this.window.hide();
        }

        this.expanded = false;
        this.element?.classList.remove('expanded');
        this.saveState();

        // Callback
        this.config.onCollapse?.();
    }

    /**
     * Handle window close button
     * For now, collapse back to dot (can be configured per dot)
     */
    private handleWindowClose(): void {
        // Default: collapse instead of destroy
        this.collapse();
    }

    /**
     * Destroy dot and its window permanently
     */
    public destroy(): void {
        if (this.window) {
            this.window.destroy();
            this.window = null;
        }

        if (this.element) {
            this.element.remove();
            this.element = null;
        }

        // Remove from uiState
        uiState.removeWindowState(this.config.id);
    }

    /**
     * Get dot ID
     */
    public get id(): string {
        return this.config.id;
    }

    /**
     * Get symbol (for rendering in Dock)
     */
    public get symbol(): string {
        return this.config.symbol;
    }

    /**
     * Get title (for expanded state)
     */
    public get title(): string {
        return this.config.title;
    }

    /**
     * Check if dot is expanded
     */
    public isExpanded(): boolean {
        return this.expanded;
    }

    /**
     * Update dot config
     */
    public updateConfig(config: Partial<DotConfig>): void {
        this.config = { ...this.config, ...config };

        // Update window if exists
        if (this.window && config.title) {
            // TODO: Add Window.updateTitle() method
        }
    }

    /**
     * Load state from uiState
     */
    private loadState(): void {
        const state = uiState.getWindowState(this.config.id);
        if (state) {
            // Restore expanded state on init
            // Note: actual expansion happens after Dock is initialized
            this.expanded = !state.minimized;
        }
    }

    /**
     * Save state to uiState
     */
    private saveState(): void {
        if (!this.window) return;

        const windowState = uiState.getWindowState(this.config.id);
        if (windowState) {
            uiState.updateWindowState(this.config.id, {
                minimized: !this.expanded,
            });
        }
    }
}
