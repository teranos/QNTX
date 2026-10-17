/**
 * Button — the door's buttons (door.ts, ceremony.ts), with loading and error.
 *
 * Every other button in QNTX web is a press of @teranos/elements (pressFrom),
 * and so is every two-stage one: this one has no second stage to give.
 */

import { log, SEG } from '../logger';

export type ButtonVariant = 'default' | 'primary' | 'secondary' | 'danger' | 'warning' | 'ghost' | 'success';
export type ButtonSize = 'small' | 'medium' | 'large';

export interface ButtonConfig {
    /** Button label text */
    label: string;
    /** Click handler - can be async */
    onClick: () => void | Promise<void>;
    /** Visual variant (default: 'default') */
    variant?: ButtonVariant;
    /** Button size (default: 'medium') */
    size?: ButtonSize;
    /** Accessible label if different from visible text */
    ariaLabel?: string;
    /** Initial disabled state */
    disabled?: boolean;
    /** Tooltip when disabled */
    disabledTooltip?: string;
    /** Additional CSS classes */
    className?: string;
    /** Button type attribute (default: 'button') */
    type?: 'button' | 'submit' | 'reset';
    /** Icon to display (text/emoji, displayed before label) */
    icon?: string;
    /** Drawn artwork, displayed before the label. A provider's own mark. */
    mark?: SVGSVGElement | null;
    /** Show only the icon or mark. The label reaches a reader as aria-label. */
    markOnly?: boolean;
}

export interface ButtonState {
    loading: boolean;
    error: Error | null;
    disabled: boolean;
}

/**
 * Button component with loading, error, and confirmation states
 */
export class Button {
    public readonly element: HTMLButtonElement;
    private config: Required<Omit<ButtonConfig, 'disabledTooltip' | 'icon' | 'mark' | 'markOnly'>> & {
        disabledTooltip?: string;
        icon?: string;
        mark?: SVGSVGElement | null;
        markOnly?: boolean;
    };
    private state: ButtonState;
    private originalLabel: string;
    private errorBox: HTMLDivElement | null = null;

    constructor(config: ButtonConfig) {
        this.config = {
            variant: 'default',
            size: 'medium',
            ariaLabel: config.label,
            disabled: false,
            className: '',
            type: 'button',
            ...config
        };

        this.state = {
            loading: false,
            error: null,
            disabled: this.config.disabled,
        };

        this.originalLabel = this.config.label;
        this.element = this.createElement();
        this.setupEventListeners();
        this.render();
    }

    private createElement(): HTMLButtonElement {
        const btn = document.createElement('button');
        btn.type = this.config.type;
        btn.className = this.buildClassName();

        if (this.config.ariaLabel) {
            btn.setAttribute('aria-label', this.config.ariaLabel);
        }

        return btn;
    }

    private buildClassName(): string {
        const classes = [
            'qntx-btn',
            `qntx-btn-${this.config.variant}`,
            `qntx-btn-${this.config.size}`
        ];

        if (this.config.className) {
            classes.push(this.config.className);
        }

        return classes.join(' ');
    }

    private setupEventListeners(): void {
        this.element.addEventListener('click', (e) => {
            e.preventDefault();
            // handleClick shows its own failures via setError — nothing can escape it
            void this.handleClick();
        });

        // Keyboard activation
        this.element.addEventListener('keydown', (e) => {
            if (e.key === 'Enter' || e.key === ' ') {
                e.preventDefault();
                void this.handleClick();
            }
        });
    }

    private async handleClick(): Promise<void> {
        // Ignore clicks when loading or disabled
        if (this.state.loading || this.state.disabled) {
            return;
        }

        // Clear any existing error
        this.clearError();

        // Execute the action
        await this.executeAction();
    }

    private async executeAction(): Promise<void> {
        this.setLoading(true);

        try {
            await this.config.onClick();
        } catch (error: unknown) {
            const err = error instanceof Error ? error : new Error(String(error));
            // What the button shows, the logger carries: on a phone the
            // slide-out is read by one person once, and an error line leaves.
            log.error(SEG.UI, `[Button] ${this.config.label}:`, err);
            this.setError(err);
        } finally {
            this.setLoading(false);
        }
    }

    /**
     * Render button content based on current state
     */
    private render(): void {
        // Update classes
        this.element.className = this.buildClassName();
        this.element.classList.toggle('qntx-btn-loading', this.state.loading);
        this.element.classList.toggle('qntx-btn-error', this.state.error !== null);
        this.element.classList.toggle('qntx-btn-disabled', this.state.disabled);

        // Update disabled state
        this.element.disabled = this.state.disabled || this.state.loading;
        this.element.setAttribute('aria-disabled', String(this.state.disabled));
        this.element.setAttribute('aria-busy', String(this.state.loading));

        // Update tooltip
        if (this.state.disabled && this.config.disabledTooltip) {
            this.element.title = this.config.disabledTooltip;
        } else {
            this.element.title = this.config.markOnly ? this.config.ariaLabel : '';
        }

        // The failure reason sits beside the button, not on top of it or
        // hidden in a hover.
        if (this.state.error) {
            this.showErrorBox(this.state.error.message);
        } else {
            this.hideErrorBox();
        }

        // Build content
        this.element.innerHTML = '';

        if (this.state.loading) {
            // Loading: spinner only, no label change
            const spinner = document.createElement('span');
            spinner.className = 'qntx-btn-spinner';
            spinner.setAttribute('aria-hidden', 'true');
            this.element.appendChild(spinner);
        } else {
            if (this.config.mark) {
                this.element.appendChild(this.config.mark);
            } else if (this.config.icon) {
                const iconSpan = document.createElement('span');
                iconSpan.className = 'qntx-btn-icon';
                iconSpan.textContent = this.config.icon;
                this.element.appendChild(iconSpan);
            }

            // A button carrying only its mark says its name to a screen reader
            // and to a hover, and nowhere else.
            if (this.config.markOnly) return;

            // Label
            const labelSpan = document.createElement('span');
            labelSpan.className = 'qntx-btn-label';

            labelSpan.textContent = this.originalLabel;

            this.element.appendChild(labelSpan);
        }
    }

    /**
     * A failure reason as a sibling of the button: selectable text, a press
     * away from the clipboard, the way tokens-element.ts didCell() copies a DID.
     */
    private showErrorBox(message: string): void {
        if (!this.errorBox) {
            this.errorBox = document.createElement('div');
            this.errorBox.className = 'qntx-btn-error-box';
        }
        const box = this.errorBox;
        box.textContent = message;
        box.onclick = (e) => {
            e.stopPropagation();
            void navigator.clipboard.writeText(message).then(
                () => { box.textContent = 'copied'; setTimeout(() => { box.textContent = message; }, 1200); },
                () => { box.textContent = 'refused'; setTimeout(() => { box.textContent = message; }, 1200); },
            );
        };
        if (this.element.parentNode) {
            this.element.insertAdjacentElement('afterend', box);
        }
    }

    private hideErrorBox(): void {
        this.errorBox?.remove();
    }

    // Public API

    /**
     * Set loading state
     */
    public setLoading(loading: boolean): void {
        this.state.loading = loading;
        this.render();
    }

    /**
     * Set error state with slide-out display
     */
    public setError(error: Error | null): void {
        this.state.error = error;
        this.render();
    }

    /**
     * Clear error state
     */
    public clearError(): void {
        this.state.error = null;
        this.render();
    }

    /**
     * Set disabled state
     */
    public setDisabled(disabled: boolean, tooltip?: string): void {
        this.state.disabled = disabled;
        if (tooltip !== undefined) {
            this.config.disabledTooltip = tooltip;
        }
        this.render();
    }

    /**
     * Update button label
     */
    public setLabel(label: string): void {
        this.originalLabel = label;
        this.config.label = label;
        this.render();
    }

    /**
     * Update click handler
     */
    public setOnClick(onClick: () => void | Promise<void>): void {
        this.config.onClick = onClick;
    }

    /**
     * Get current state
     */
    public getState(): Readonly<ButtonState> {
        return { ...this.state };
    }

    /**
     * Reset button to initial state
     */
    public reset(): void {
        this.clearError();
        this.setLoading(false);
    }

    /**
     * Cleanup resources
     */
    public destroy(): void {
        this.errorBox?.remove();
        this.element.remove();
    }
}

/**
 * Factory function for creating buttons
 */
export function createButton(config: ButtonConfig): Button {
    return new Button(config);
}
