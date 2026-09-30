/**
 * Tooltip Module - Interactive terminal-style tooltips
 *
 * Provides rich, multi-line tooltips with terminal styling.
 * Designed for observability - show metadata, build times, status details.
 *
 * Usage:
 * 1. Add data-tooltip attribute with tooltip text to elements
 * 2. Add an interactive class (e.g., 'has-tooltip') to trigger tooltip behavior
 * 3. Call tooltip.attach(container, selector) to enable tooltips
 *
 * Features:
 * - 300ms delay to prevent hover noise
 * - Terminal-style dark background with monospace font
 * - Multi-line support (use \n in tooltip text)
 * - Viewport-constrained positioning
 * - Touch support with tap-to-toggle
 * - Auto-cleanup on container removal
 *
 * A tally is shown here and nowhere else: "For one changing value, direct view
 * of time and value", "Same would go for doughnut, revealing a legend as well",
 * "And a longer hover should expand the tooltip showing the bigger picture".
 * - data-tooltip-series: one [moment, value] per step along the trigger's
 *   width, and the tooltip says the one under the pointer as it moves.
 * - expands(): what a longer hover grows the tooltip into, found by the data
 *   attribute the trigger or one of its ancestors carries.
 */

export interface TooltipConfig {
    /** Delay in ms before showing tooltip (default: 300) */
    delay?: number;
    /** CSS class for interactive elements (default: 'has-tooltip') */
    triggerClass?: string;
    /** Max width of tooltip in pixels (default: 400) */
    maxWidth?: number;
    /** Position relative to trigger element (default: 'bottom') */
    position?: 'top' | 'bottom';
    /** Hover in ms, from the pointer arriving, before the tooltip grows into the bigger picture (default: 1200) */
    expandDelay?: number;
}

/** What a longer hover grows the tooltip into, built from the element carrying the attribute. */
export type Expansion = (from: HTMLElement) => HTMLElement | null;

/** A series trigger's steps: when, and the value then. */
type Step = [string, number];

const SERIES = '[data-tooltip-series]';

const DEFAULT_CONFIG: Required<TooltipConfig> = {
    delay: 300,
    triggerClass: 'has-tooltip',
    maxWidth: 400,
    position: 'bottom',
    expandDelay: 1200,
};

class TooltipManager {
    private tooltip: HTMLElement | null = null;
    private tooltipTimeout: number | null = null;
    private currentTrigger: HTMLElement | null = null;
    private config: Required<TooltipConfig>;
    private expandTimeout: number | null = null;
    private expanded = false;
    private pointerX = 0;
    private expansions: [string, Expansion][] = [];

    constructor(config: TooltipConfig = {}) {
        this.config = { ...DEFAULT_CONFIG, ...config };
    }

    /**
     * What a longer hover over anything carrying `attribute` (a data-* name,
     * as written in HTML) grows the tooltip into.
     */
    expands(attribute: string, build: Expansion): void {
        this.expansions = this.expansions.filter(([a]) => a !== attribute);
        this.expansions.push([attribute, build]);
    }

    /** The trigger under a target: a series first, as the innermost thing pointed at. */
    private triggerOf(target: HTMLElement, selector: string, container: HTMLElement): HTMLElement | null {
        const found = (target.closest(SERIES) ?? target.closest(selector)) as HTMLElement | null;
        return found && container.contains(found) ? found : null;
    }

    /** What the tooltip says for a trigger: a series says the step under the pointer. */
    private textFor(trigger: HTMLElement): string {
        const series = trigger.dataset.tooltipSeries;
        if (!series) return trigger.dataset.tooltip ?? '';
        let steps: Step[];
        try {
            steps = JSON.parse(series) as Step[];
        } catch (err: unknown) {
            return `this line's moments could not be read: ${err instanceof Error ? err.message : String(err)}`;
        }
        if (steps.length === 0) return '';
        const rect = trigger.getBoundingClientRect();
        const along = rect.width > 0 ? (this.pointerX - rect.left) / rect.width : 0;
        const i = Math.min(steps.length - 1, Math.max(0, Math.round(along * (steps.length - 1))));
        const [at, value] = steps[i];
        return `${at} · ${value}`;
    }

    /** The bigger picture for a trigger, if anything it sits in has one. */
    private expansionFor(trigger: HTMLElement): HTMLElement | null {
        for (const [attribute, build] of this.expansions) {
            const from = trigger.closest(`[${attribute}]`) as HTMLElement | null;
            if (from) return build(from);
        }
        return null;
    }

    /**
     * Attach tooltip behavior to elements within a container
     * Uses event delegation for performance
     *
     * @param container The container element to attach listeners to
     * @param triggerSelector Optional CSS selector for trigger elements (default uses triggerClass)
     * @returns Cleanup function to remove listeners
     */
    attach(container: HTMLElement, triggerSelector?: string): () => void {
        const selector = triggerSelector || `.${this.config.triggerClass}`;

        const handleMouseEnter = (e: Event) => {
            const target = e.target as HTMLElement;
            if (e instanceof MouseEvent) this.pointerX = e.clientX;
            const trigger = this.triggerOf(target, selector, container);
            if (trigger && trigger !== this.currentTrigger && this.textFor(trigger)) {
                this.show(trigger, this.textFor(trigger));
            }
        };

        const handleMouseLeave = (e: Event) => {
            const target = e.target as HTMLElement;
            if (!target.matches(`${SERIES}, ${selector}`)) return;
            const trigger = this.triggerOf(target, selector, container);
            if (trigger && trigger === this.currentTrigger) {
                this.hide();
            }
        };

        // The one changing value follows the pointer along a series. Once the
        // tooltip has grown into the bigger picture it holds still.
        const handleMouseMove = (e: MouseEvent) => {
            this.pointerX = e.clientX;
            const trigger = this.currentTrigger;
            if (!trigger || !trigger.dataset.tooltipSeries || !this.tooltip || this.expanded) return;
            this.tooltip.textContent = this.textFor(trigger);
            this.positionTooltip(trigger);
        };

        // Touch support: long press to show tooltip
        // Use passive touchstart to track touch position, then check on touchend
        let touchStartTarget: HTMLElement | null = null;
        let touchStartTime = 0;

        const handleTouchStart = (e: TouchEvent) => {
            const target = e.target as HTMLElement;
            touchStartTarget = this.triggerOf(target, selector, container);
            touchStartTime = Date.now();
        };

        const handleTouchEnd = (e: TouchEvent) => {
            const target = e.target as HTMLElement;
            const trigger = this.triggerOf(target, selector, container);
            const touch = e.changedTouches[0];
            if (touch) this.pointerX = touch.clientX;

            // Only handle if touch ended on same element it started
            if (trigger && trigger === touchStartTarget) {
                const touchDuration = Date.now() - touchStartTime;
                const tooltipText = this.textFor(trigger);

                if (tooltipText) {
                    // If tooltip is already showing for this trigger, hide it
                    if (this.currentTrigger === trigger && this.tooltip) {
                        this.hide();
                    } else if (touchDuration < 500) {
                        // Quick tap: show tooltip immediately
                        this.hideImmediate();
                        this.showImmediate(trigger, tooltipText);
                    } else if (touchDuration >= this.config.expandDelay) {
                        // Held as long as a longer hover: the bigger picture
                        this.hideImmediate();
                        this.showImmediate(trigger, tooltipText);
                        this.expand(trigger);
                    }
                }
            }

            touchStartTarget = null;
        };

        container.addEventListener('mouseenter', handleMouseEnter, true);
        container.addEventListener('mouseleave', handleMouseLeave, true);
        container.addEventListener('mousemove', handleMouseMove, true);
        container.addEventListener('touchstart', handleTouchStart, { capture: true, passive: true });
        container.addEventListener('touchend', handleTouchEnd, { capture: true, passive: true });

        // Return cleanup function
        return () => {
            container.removeEventListener('mouseenter', handleMouseEnter, true);
            container.removeEventListener('mouseleave', handleMouseLeave, true);
            container.removeEventListener('mousemove', handleMouseMove, true);
            container.removeEventListener('touchstart', handleTouchStart, true);
            container.removeEventListener('touchend', handleTouchEnd, true);
            this.hide();
        };
    }

    /**
     * Show tooltip after delay
     */
    show(trigger: HTMLElement, text: string): void {
        // Clear any existing timeout
        if (this.tooltipTimeout) {
            clearTimeout(this.tooltipTimeout);
        }

        if (this.expandTimeout) {
            clearTimeout(this.expandTimeout);
        }

        this.currentTrigger = trigger;

        // Show tooltip after delay
        this.tooltipTimeout = window.setTimeout(() => {
            // Remove old tooltip if exists
            this.hideImmediate();

            // Create new tooltip, saying the step under the pointer by now
            this.tooltip = this.createTooltipElement(trigger.dataset.tooltipSeries ? this.textFor(trigger) : text);

            // Append to DOM before positioning (getBoundingClientRect needs element in DOM)
            document.body.appendChild(this.tooltip);

            // Position tooltip
            this.positionTooltip(trigger);
        }, this.config.delay);

        // A longer hover grows the same tooltip into the bigger picture
        this.expandTimeout = window.setTimeout(() => {
            if (this.currentTrigger === trigger) this.expand(trigger);
        }, this.config.expandDelay);
    }

    /** Grows the tooltip over a trigger into the bigger picture, when there is one. */
    private expand(trigger: HTMLElement): void {
        const picture = this.expansionFor(trigger);
        if (!picture) return;
        if (!this.tooltip) {
            this.tooltip = this.createTooltipElement('');
            document.body.appendChild(this.tooltip);
        }
        this.tooltip.replaceChildren(picture);
        this.tooltip.classList.add('panel-tooltip-expanded');
        this.tooltip.style.maxWidth = '';
        this.expanded = true;
        this.positionTooltip(trigger);
    }

    /**
     * Show tooltip immediately without delay (for touch events)
     */
    showImmediate(trigger: HTMLElement, text: string): void {
        this.currentTrigger = trigger;

        // Remove old tooltip if exists
        this.hideImmediate();

        // Create new tooltip
        this.tooltip = this.createTooltipElement(text);

        // Append to DOM before positioning (getBoundingClientRect needs element in DOM)
        document.body.appendChild(this.tooltip);

        // Position tooltip
        this.positionTooltip(trigger);
    }

    /**
     * Cancel pending tooltip or hide visible one
     */
    hide(): void {
        if (this.tooltipTimeout) {
            clearTimeout(this.tooltipTimeout);
            this.tooltipTimeout = null;
        }
        if (this.expandTimeout) {
            clearTimeout(this.expandTimeout);
            this.expandTimeout = null;
        }
        this.hideImmediate();
        this.currentTrigger = null;
    }

    /**
     * Create a tooltip DOM element with proper semantics
     */
    private createTooltipElement(text: string): HTMLElement {
        const el = document.createElement('div');
        el.className = 'panel-tooltip';
        el.setAttribute('role', 'tooltip');
        if (this.config.position === 'top') {
            el.classList.add('panel-tooltip-top');
        }
        el.textContent = text;
        el.style.maxWidth = `${this.config.maxWidth}px`;
        return el;
    }

    /**
     * Immediately remove tooltip from DOM
     */
    private hideImmediate(): void {
        if (this.tooltip) {
            this.tooltip.remove();
            this.tooltip = null;
        }
        this.expanded = false;
    }

    /**
     * Position tooltip relative to trigger element
     */
    private positionTooltip(trigger: HTMLElement): void {
        if (!this.tooltip) return;

        const rect = trigger.getBoundingClientRect();
        const tooltipRect = this.tooltip.getBoundingClientRect();

        if (this.config.position === 'top') {
            this.tooltip.style.left = `${rect.left + (rect.width / 2) - (tooltipRect.width / 2)}px`;
            this.tooltip.style.top = `${rect.top - tooltipRect.height - 8}px`;
        } else {
            this.tooltip.style.left = `${rect.left}px`;
            this.tooltip.style.top = `${rect.bottom + 8}px`;
        }

        // Ensure tooltip stays within viewport
        this.constrainToViewport();
    }

    /**
     * Adjust tooltip position to stay within viewport
     */
    private constrainToViewport(): void {
        if (!this.tooltip) return;

        const rect = this.tooltip.getBoundingClientRect();
        const padding = 8;

        // Constrain horizontally
        if (rect.right > window.innerWidth - padding) {
            this.tooltip.style.left = `${window.innerWidth - rect.width - padding}px`;
        }
        if (rect.left < padding) {
            this.tooltip.style.left = `${padding}px`;
        }

        // Constrain vertically (flip if needed)
        if (rect.bottom > window.innerHeight - padding && this.config.position === 'bottom') {
            const trigger = this.currentTrigger;
            if (trigger) {
                const triggerRect = trigger.getBoundingClientRect();
                this.tooltip.style.top = `${triggerRect.top - rect.height - 8}px`;
                this.tooltip.classList.add('panel-tooltip-top');
            }
        }
    }

    /**
     * Update configuration
     */
    configure(config: Partial<TooltipConfig>): void {
        this.config = { ...this.config, ...config };
    }
}

// Export singleton instance for global use
export const tooltip = new TooltipManager();

// Export class for custom instances
export { TooltipManager };

// Re-exported for default-elements.ts
export { formatBuildTime } from '../html-utils';

/**
 * Build a multi-line tooltip string from key-value pairs
 * Formats as "key: value" with line breaks between entries
 *
 * @param entries Object with string keys and any values
 * @param options Optional separator and filter options
 * @returns Formatted tooltip string
 */
export function buildTooltipText(
    entries: Record<string, unknown>,
    options: {
        separator?: string;
        omitEmpty?: boolean;
    } = {}
): string {
    const { separator = '\n', omitEmpty = true } = options;

    return Object.entries(entries)
        .filter(([, value]) => !omitEmpty || (value !== undefined && value !== null && value !== ''))
        .map(([key, value]) => {
            let displayValue: string;
            if (typeof value === 'object') {
                displayValue = JSON.stringify(value);
            } else {
                displayValue = String(value);
            }
            return `${key}: ${displayValue}`;
        })
        .join(separator);
}
