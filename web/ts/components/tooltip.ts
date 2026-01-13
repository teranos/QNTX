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
}

const DEFAULT_CONFIG: Required<TooltipConfig> = {
    delay: 300,
    triggerClass: 'has-tooltip',
    maxWidth: 400,
    position: 'bottom'
};

class TooltipManager {
    private tooltip: HTMLElement | null = null;
    private tooltipTimeout: number | null = null;
    private currentTrigger: HTMLElement | null = null;
    private config: Required<TooltipConfig>;

    constructor(config: TooltipConfig = {}) {
        this.config = { ...DEFAULT_CONFIG, ...config };
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
            const trigger = target.closest(selector) as HTMLElement | null;
            if (trigger) {
                const tooltipText = trigger.dataset.tooltip;
                if (tooltipText) {
                    this.show(trigger, tooltipText);
                }
            }
        };

        const handleMouseLeave = (e: Event) => {
            const target = e.target as HTMLElement;
            const trigger = target.closest(selector) as HTMLElement | null;
            if (trigger) {
                this.hide();
            }
        };

        // Touch support: long press to show tooltip
        // Use passive touchstart to track touch position, then check on touchend
        let touchStartTarget: HTMLElement | null = null;
        let touchStartTime = 0;

        const handleTouchStart = (e: TouchEvent) => {
            const target = e.target as HTMLElement;
            touchStartTarget = target.closest(selector) as HTMLElement | null;
            touchStartTime = Date.now();
        };

        const handleTouchEnd = (e: TouchEvent) => {
            const target = e.target as HTMLElement;
            const trigger = target.closest(selector) as HTMLElement | null;

            // Only handle if touch ended on same element it started
            if (trigger && trigger === touchStartTarget) {
                const touchDuration = Date.now() - touchStartTime;
                const tooltipText = trigger.dataset.tooltip;

                if (tooltipText) {
                    // If tooltip is already showing for this trigger, hide it
                    if (this.currentTrigger === trigger && this.tooltip) {
                        this.hide();
                    } else if (touchDuration < 500) {
                        // Quick tap: show tooltip immediately
                        this.hideImmediate();
                        this.showImmediate(trigger, tooltipText);
                    }
                }
            }

            touchStartTarget = null;
        };

        container.addEventListener('mouseenter', handleMouseEnter, true);
        container.addEventListener('mouseleave', handleMouseLeave, true);
        container.addEventListener('touchstart', handleTouchStart, { capture: true, passive: true });
        container.addEventListener('touchend', handleTouchEnd, { capture: true, passive: true });

        // Return cleanup function
        return () => {
            container.removeEventListener('mouseenter', handleMouseEnter, true);
            container.removeEventListener('mouseleave', handleMouseLeave, true);
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

        this.currentTrigger = trigger;

        // Show tooltip after delay
        this.tooltipTimeout = window.setTimeout(() => {
            // Remove old tooltip if exists
            this.hideImmediate();

            // Create new tooltip
            this.tooltip = document.createElement('div');
            this.tooltip.className = 'panel-tooltip';
            if (this.config.position === 'top') {
                this.tooltip.classList.add('panel-tooltip-top');
            }
            this.tooltip.textContent = text;
            this.tooltip.style.maxWidth = `${this.config.maxWidth}px`;

            // Append to DOM before positioning (getBoundingClientRect needs element in DOM)
            document.body.appendChild(this.tooltip);

            // Position tooltip
            this.positionTooltip(trigger);
        }, this.config.delay);
    }

    /**
     * Show tooltip immediately without delay (for touch events)
     */
    showImmediate(trigger: HTMLElement, text: string): void {
        this.currentTrigger = trigger;

        // Remove old tooltip if exists
        this.hideImmediate();

        // Create new tooltip
        this.tooltip = document.createElement('div');
        this.tooltip.className = 'panel-tooltip';
        if (this.config.position === 'top') {
            this.tooltip.classList.add('panel-tooltip-top');
        }
        this.tooltip.textContent = text;
        this.tooltip.style.maxWidth = `${this.config.maxWidth}px`;

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
        this.hideImmediate();
        this.currentTrigger = null;
    }

    /**
     * Immediately remove tooltip from DOM
     */
    private hideImmediate(): void {
        if (this.tooltip) {
            this.tooltip.remove();
            this.tooltip = null;
        }
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

/**
 * Helper to format build timestamps into relative time + absolute date
 * Matches the format used in plugin panel
 *
 * @param timestamp RFC3339 timestamp string or Unix epoch in seconds
 * @returns Formatted string like "2h ago (Jan 8, 2026 3:48 AM)" or null if invalid
 */
export function formatBuildTime(timestamp?: string | number): string | null {
    if (!timestamp || timestamp === 'dev' || timestamp === 'unknown') {
        return null;
    }

    let date: Date;

    if (typeof timestamp === 'number') {
        // Unix epoch in seconds
        date = new Date(timestamp * 1000);
    } else {
        // Try RFC3339 first, then Unix epoch string
        date = new Date(timestamp);
        if (isNaN(date.getTime())) {
            const epochSeconds = parseInt(timestamp, 10);
            if (!isNaN(epochSeconds)) {
                date = new Date(epochSeconds * 1000);
            }
        }
    }

    if (isNaN(date.getTime())) {
        return null;
    }

    const now = new Date();
    const diffMs = now.getTime() - date.getTime();

    // Don't show relative time for future dates
    if (diffMs < 0) {
        return date.toLocaleString();
    }

    const diffMins = Math.floor(diffMs / 60000);
    const diffHours = Math.floor(diffMins / 60);
    const diffDays = Math.floor(diffHours / 24);

    let relativeTime: string;
    if (diffMins < 1) {
        relativeTime = 'just now';
    } else if (diffMins < 60) {
        relativeTime = `${diffMins}m ago`;
    } else if (diffHours < 24) {
        relativeTime = `${diffHours}h ago`;
    } else {
        relativeTime = `${diffDays}d ago`;
    }

    const formattedDate = date.toLocaleString();
    return `${relativeTime} (${formattedDate})`;
}

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
