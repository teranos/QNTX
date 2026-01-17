/**
 * Button Component - Reusable button with loading, error, and confirmation states
 *
 * Features:
 * - Proper accessibility (aria-label, aria-disabled, aria-busy)
 * - Loading state with spinner and disabled interaction
 * - Error state with slide-out error display
 * - Disabled state with optional tooltip
 * - Two-stage confirmation for destructive actions
 * - Keyboard activation (Enter, Space)
 *
 * Usage:
 * ```typescript
 * const btn = new Button({
 *     label: 'Save',
 *     onClick: async () => { await saveData(); },
 *     variant: 'primary'
 * });
 * container.appendChild(btn.element);
 *
 * // With confirmation
 * const deleteBtn = new Button({
 *     label: 'Delete',
 *     onClick: async () => { await deleteItem(); },
 *     variant: 'danger',
 *     confirmation: {
 *         label: 'Confirm Delete',
 *         timeout: 5000
 *     }
 * });
 * ```
 */

export type ButtonVariant = 'default' | 'primary' | 'secondary' | 'danger' | 'warning' | 'ghost';
export type ButtonSize = 'small' | 'medium' | 'large';

/** Default timeout in ms for confirmation state before reverting */
const DEFAULT_CONFIRMATION_TIMEOUT = 5000;

export interface ButtonConfirmation {
    /** Label to show in confirmation state */
    label: string;
    /** Timeout in ms before reverting to original state (default: DEFAULT_CONFIRMATION_TIMEOUT) */
    timeout?: number;
}

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
    /** Two-stage confirmation config for destructive actions */
    confirmation?: ButtonConfirmation;
    /** Additional CSS classes */
    className?: string;
    /** Button type attribute (default: 'button') */
    type?: 'button' | 'submit' | 'reset';
    /** Icon to display (text/emoji, displayed before label) */
    icon?: string;
}

export interface ButtonState {
    loading: boolean;
    error: Error | null;
    disabled: boolean;
    confirming: boolean;
}

/**
 * Button component with loading, error, and confirmation states
 */
export class Button {
    public readonly element: HTMLButtonElement;
    private config: Required<Omit<ButtonConfig, 'confirmation' | 'disabledTooltip' | 'icon'>> & {
        confirmation?: ButtonConfirmation;
        disabledTooltip?: string;
        icon?: string;
    };
    private state: ButtonState;
    private originalLabel: string;
    private confirmTimeout: number | null = null;
    private errorElement: HTMLElement | null = null;

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
            confirming: false
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
            this.handleClick();
        });

        // Keyboard activation
        this.element.addEventListener('keydown', (e) => {
            if (e.key === 'Enter' || e.key === ' ') {
                e.preventDefault();
                this.handleClick();
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

        // Handle two-stage confirmation
        if (this.config.confirmation) {
            if (!this.state.confirming) {
                // First click: enter confirmation state
                this.enterConfirmState();
                return;
            }
            // Second click: proceed with action
            this.exitConfirmState();
        }

        // Execute the action
        await this.executeAction();
    }

    private async executeAction(): Promise<void> {
        this.setLoading(true);

        try {
            await this.config.onClick();
        } catch (error) {
            const err = error instanceof Error ? error : new Error(String(error));
            this.setError(err);
        } finally {
            this.setLoading(false);
        }
    }

    private enterConfirmState(): void {
        this.state.confirming = true;
        this.render();

        // Set timeout to auto-revert
        const timeout = this.config.confirmation?.timeout ?? DEFAULT_CONFIRMATION_TIMEOUT;
        this.confirmTimeout = window.setTimeout(() => {
            this.exitConfirmState();
        }, timeout);
    }

    private exitConfirmState(): void {
        if (this.confirmTimeout) {
            clearTimeout(this.confirmTimeout);
            this.confirmTimeout = null;
        }
        this.state.confirming = false;
        this.render();
    }

    /**
     * Render button content based on current state
     */
    private render(): void {
        // Update classes
        this.element.className = this.buildClassName();
        this.element.classList.toggle('qntx-btn-loading', this.state.loading);
        this.element.classList.toggle('qntx-btn-error', this.state.error !== null);
        this.element.classList.toggle('qntx-btn-confirming', this.state.confirming);
        this.element.classList.toggle('qntx-btn-disabled', this.state.disabled);

        // Update disabled state
        this.element.disabled = this.state.disabled || this.state.loading;
        this.element.setAttribute('aria-disabled', String(this.state.disabled));
        this.element.setAttribute('aria-busy', String(this.state.loading));

        // Update tooltip
        if (this.state.disabled && this.config.disabledTooltip) {
            this.element.title = this.config.disabledTooltip;
        } else {
            this.element.title = '';
        }

        // Build content
        this.element.innerHTML = '';

        // Icon (if present)
        if (this.config.icon && !this.state.loading) {
            const iconSpan = document.createElement('span');
            iconSpan.className = 'qntx-btn-icon';
            iconSpan.textContent = this.config.icon;
            this.element.appendChild(iconSpan);
        }

        // Loading spinner
        if (this.state.loading) {
            const spinner = document.createElement('span');
            spinner.className = 'qntx-btn-spinner';
            spinner.setAttribute('aria-hidden', 'true');
            this.element.appendChild(spinner);
        }

        // Label
        const labelSpan = document.createElement('span');
        labelSpan.className = 'qntx-btn-label';

        if (this.state.loading) {
            labelSpan.textContent = this.originalLabel + '...';
        } else if (this.state.confirming && this.config.confirmation) {
            labelSpan.textContent = this.config.confirmation.label;
        } else {
            labelSpan.textContent = this.originalLabel;
        }

        this.element.appendChild(labelSpan);

        // Render error if present
        this.renderError();
    }

    /**
     * Render slide-out error display
     */
    private renderError(): void {
        // Remove existing error element
        if (this.errorElement) {
            this.errorElement.remove();
            this.errorElement = null;
        }

        if (!this.state.error) {
            return;
        }

        // Create error element that slides out from button
        this.errorElement = document.createElement('div');
        this.errorElement.className = 'qntx-btn-error-display';
        this.errorElement.setAttribute('role', 'alert');
        this.errorElement.setAttribute('aria-live', 'polite');

        const errorIcon = document.createElement('span');
        errorIcon.className = 'qntx-btn-error-icon';
        errorIcon.textContent = '⚠';
        errorIcon.setAttribute('aria-hidden', 'true');

        const errorMessage = document.createElement('span');
        errorMessage.className = 'qntx-btn-error-message';
        errorMessage.textContent = this.state.error.message;

        const dismissBtn = document.createElement('button');
        dismissBtn.className = 'qntx-btn-error-dismiss';
        dismissBtn.setAttribute('aria-label', 'Dismiss error');
        dismissBtn.textContent = '×';
        dismissBtn.addEventListener('click', (e) => {
            e.stopPropagation();
            this.clearError();
        });

        this.errorElement.appendChild(errorIcon);
        this.errorElement.appendChild(errorMessage);
        this.errorElement.appendChild(dismissBtn);

        // Insert after button (requires wrapper or absolute positioning)
        this.element.insertAdjacentElement('afterend', this.errorElement);

        // Trigger animation (use setTimeout fallback for test environments)
        const scheduleFrame = typeof requestAnimationFrame !== 'undefined'
            ? requestAnimationFrame
            : (cb: () => void) => setTimeout(cb, 0);

        // Capture element in closure to avoid race condition with clearError()
        const errorEl = this.errorElement;
        scheduleFrame(() => {
            errorEl?.classList.add('qntx-btn-error-visible');
        });
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
        if (this.errorElement) {
            this.errorElement.classList.remove('qntx-btn-error-visible');
            setTimeout(() => {
                this.errorElement?.remove();
                this.errorElement = null;
            }, 200);
        }
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
        this.exitConfirmState();
        this.clearError();
        this.setLoading(false);
    }

    /**
     * Cleanup resources
     */
    public destroy(): void {
        if (this.confirmTimeout) {
            clearTimeout(this.confirmTimeout);
        }
        if (this.errorElement) {
            this.errorElement.remove();
        }
        this.element.remove();

        // Auto-unregister from button registry if registered
        // This prevents dangling references when buttons are destroyed
        // without explicit unregistration
        for (const [operationId, btn] of buttonRegistry.entries()) {
            if (btn === this) {
                buttonRegistry.delete(operationId);
                break;
            }
        }
    }
}

/**
 * Factory function for creating buttons
 */
export function createButton(config: ButtonConfig): Button {
    return new Button(config);
}

/**
 * Create a primary action button
 */
export function createPrimaryButton(label: string, onClick: () => void | Promise<void>): Button {
    return new Button({ label, onClick, variant: 'primary' });
}

/**
 * Create a danger button with confirmation
 */
export function createDangerButton(
    label: string,
    confirmLabel: string,
    onClick: () => void | Promise<void>
): Button {
    return new Button({
        label,
        onClick,
        variant: 'danger',
        confirmation: {
            label: confirmLabel,
            timeout: DEFAULT_CONFIRMATION_TIMEOUT
        }
    });
}

/**
 * Create a ghost/minimal button
 */
export function createGhostButton(label: string, onClick: () => void | Promise<void>): Button {
    return new Button({ label, onClick, variant: 'ghost' });
}

/**
 * Hydration configuration for replacing placeholder buttons
 */
export interface HydrateConfig {
    [buttonId: string]: ButtonConfig;
}

/**
 * Hydrate placeholder buttons in a container with Button instances
 *
 * This enables panels using HTML templates to adopt the Button component:
 *
 * 1. In your template, use placeholder buttons:
 *    ```html
 *    <button class="qntx-btn-placeholder" data-button-id="save">Save</button>
 *    <button class="qntx-btn-placeholder" data-button-id="delete">Delete</button>
 *    ```
 *
 * 2. After setting innerHTML, call hydrate:
 *    ```typescript
 *    content.innerHTML = template;
 *    const buttons = hydrateButtons(content, {
 *        save: { label: 'Save', onClick: () => this.save(), variant: 'primary' },
 *        delete: { label: 'Delete', onClick: () => this.delete(), variant: 'danger',
 *                  confirmation: { label: 'Confirm Delete' } }
 *    });
 *    // buttons.save and buttons.delete are now Button instances
 *    ```
 *
 * @param container Element containing placeholder buttons
 * @param config Map of button IDs to their configurations
 * @returns Map of button IDs to Button instances
 */
export function hydrateButtons(
    container: HTMLElement,
    config: HydrateConfig
): Record<string, Button> {
    const buttons: Record<string, Button> = {};
    const placeholders = container.querySelectorAll<HTMLElement>('.qntx-btn-placeholder');

    placeholders.forEach(placeholder => {
        const buttonId = placeholder.dataset.buttonId;
        if (!buttonId || !config[buttonId]) {
            return;
        }

        const buttonConfig = config[buttonId];
        const button = new Button(buttonConfig);

        // Preserve any additional classes from placeholder
        placeholder.classList.forEach(cls => {
            if (cls !== 'qntx-btn-placeholder' && !cls.startsWith('qntx-btn')) {
                button.element.classList.add(cls);
            }
        });

        // Preserve data attributes
        Object.keys(placeholder.dataset).forEach(key => {
            if (key !== 'buttonId') {
                button.element.dataset[key] = placeholder.dataset[key];
            }
        });

        // Replace placeholder with button
        placeholder.replaceWith(button.element);
        buttons[buttonId] = button;
    });

    return buttons;
}

/**
 * Generate placeholder HTML for use in templates
 *
 * @param buttonId Unique ID for hydration matching
 * @param label Visible label (will be replaced by Button, but shown briefly during load)
 * @param extraClasses Additional CSS classes to preserve
 * @returns HTML string for placeholder button
 */
export function buttonPlaceholder(
    buttonId: string,
    label: string,
    extraClasses?: string
): string {
    const classes = ['qntx-btn-placeholder', extraClasses].filter(Boolean).join(' ');
    return `<button class="${classes}" data-button-id="${buttonId}">${label}</button>`;
}

// ============================================================================
// Button Registry for WebSocket-driven updates
// NOTE: This registry is currently unused in the codebase. It was designed
// for future WebSocket-driven button state updates but is not yet integrated
// with any WebSocket handlers. The registry functionality is exported and
// ready to use when needed.
// ============================================================================

/**
 * Registry for buttons that can receive server-driven state updates.
 *
 * Use this when you need buttons to respond to WebSocket messages.
 * Example: Show loading state when operation starts, error when it fails.
 *
 * ```typescript
 * // When creating a button for an async operation
 * const btn = new Button({
 *     label: 'Process',
 *     onClick: async () => {
 *         const operationId = await startOperation();
 *         registerButton(operationId, btn);
 *     }
 * });
 *
 * // In WebSocket handler
 * function handleOperationStatus(data: { operationId: string; status: string }) {
 *     const btn = getButton(data.operationId);
 *     if (!btn) return;
 *
 *     switch (data.status) {
 *         case 'processing':
 *             btn.setLoading(true);
 *             break;
 *         case 'complete':
 *             btn.setLoading(false);
 *             unregisterButton(data.operationId);
 *             break;
 *         case 'error':
 *             btn.setError(new Error(data.message));
 *             unregisterButton(data.operationId);
 *             break;
 *     }
 * }
 * ```
 */
const buttonRegistry = new Map<string, Button>();

/**
 * Register a button for server-driven updates
 * @param operationId Unique ID to identify this operation (e.g., job ID, request ID)
 * @param button The Button instance to update
 */
export function registerButton(operationId: string, button: Button): void {
    buttonRegistry.set(operationId, button);
}

/**
 * Get a registered button by operation ID
 * @param operationId The operation ID used during registration
 * @returns The Button instance, or undefined if not found
 */
export function getButton(operationId: string): Button | undefined {
    return buttonRegistry.get(operationId);
}

/**
 * Unregister a button (call when operation completes or button is destroyed)
 * @param operationId The operation ID to unregister
 */
export function unregisterButton(operationId: string): void {
    buttonRegistry.delete(operationId);
}

/**
 * Clear all registered buttons
 * Useful for cleanup during page transitions
 */
export function clearButtonRegistry(): void {
    buttonRegistry.clear();
}

/**
 * Get count of registered buttons (useful for debugging)
 */
export function getRegisteredButtonCount(): number {
    return buttonRegistry.size;
}
