/**
 * BasePanel Error Handling
 *
 * Error state management and recovery for BasePanel:
 * - Error state display and clearing
 * - Retry mechanism with error boundaries
 * - Loading state management
 * - Error UI creation
 */

import { CSS, DATA, setLoading } from './css-classes.ts';

/**
 * Error state for a panel
 */
export interface PanelErrorState {
    hasError: boolean;
    lastError: Error | null;
}

/**
 * Context needed for error handling operations
 */
export interface ErrorHandlingContext {
    /** Panel configuration ID for logging */
    panelId: string;
    /** Error state to manage */
    errorState: PanelErrorState;
    /** Query selector scoped to panel */
    $: <T extends HTMLElement = HTMLElement>(selector: string) => T | null;
    /** Lifecycle hook to retry */
    onShow: () => Promise<void>;
}

/**
 * Create an error state UI element
 */
export function createErrorState(
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

/**
 * Create a loading state UI element
 */
export function createLoadingState(message: string = 'Loading...'): HTMLElement {
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
 * Display an error state in the panel content area
 */
export function showErrorState(ctx: ErrorHandlingContext, error: Error): void {
    ctx.errorState.hasError = true;
    ctx.errorState.lastError = error;

    const content = ctx.$<HTMLElement>(`.${CSS.PANEL.CONTENT}`);
    if (!content) {
        console.warn(`[${ctx.panelId}] No .${CSS.PANEL.CONTENT} element found for error display`);
        return;
    }

    // Clear existing content and show error
    content.innerHTML = '';
    const errorEl = createErrorState(
        'Something went wrong',
        error.message,
        () => retryShow(ctx)
    );
    content.appendChild(errorEl);

    // Set loading state to error for CSS styling
    setLoading(content, DATA.LOADING.ERROR);
}

/**
 * Clear the error state
 */
export function clearError(ctx: ErrorHandlingContext): void {
    ctx.errorState.hasError = false;
    ctx.errorState.lastError = null;

    const content = ctx.$<HTMLElement>(`.${CSS.PANEL.CONTENT}`);
    if (content) {
        setLoading(content, DATA.LOADING.IDLE);
    }
}

/**
 * Retry showing the panel after an error
 */
export async function retryShow(ctx: ErrorHandlingContext): Promise<void> {
    clearError(ctx);

    // Clear content before retry
    const content = ctx.$<HTMLElement>(`.${CSS.PANEL.CONTENT}`);
    if (content) {
        content.innerHTML = '';
        content.appendChild(createLoadingState());
    }

    // Re-run onShow with error boundary
    try {
        await ctx.onShow();
    } catch (error) {
        const err = error instanceof Error ? error : new Error(String(error));
        console.error(`[${ctx.panelId}] Error in retryShow():`, err);
        showErrorState(ctx, err);
    }
}

/**
 * Show loading state in the panel content area
 */
export function showLoading(ctx: ErrorHandlingContext, message?: string): void {
    const content = ctx.$<HTMLElement>(`.${CSS.PANEL.CONTENT}`);
    if (!content) return;

    content.innerHTML = '';
    content.appendChild(createLoadingState(message));
    setLoading(content, DATA.LOADING.LOADING);
}

/**
 * Hide loading state (resets to idle)
 */
export function hideLoading(ctx: ErrorHandlingContext): void {
    const content = ctx.$<HTMLElement>(`.${CSS.PANEL.CONTENT}`);
    if (content) {
        setLoading(content, DATA.LOADING.IDLE);
    }
}
