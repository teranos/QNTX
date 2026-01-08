/**
 * BasePanel Error Handling
 *
 * Error state management and recovery for BasePanel:
 * - Error state display and clearing
 * - Retry mechanism with error boundaries
 * - Loading state management
 * - Rich error UI with expandable details
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
 * Rich error information for detailed display
 */
export interface RichError {
    /** Error title (e.g., "Internal Server Error", "Connection Failed") */
    title: string;
    /** Main error message */
    message: string;
    /** Optional detailed error information (stack trace, response body, etc.) */
    details?: string;
    /** HTTP status code if applicable */
    status?: number;
    /** Suggestion for how to resolve the error */
    suggestion?: string;
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
 * Create a rich error state UI element with expandable details
 */
export function createRichErrorState(
    error: RichError,
    onRetry?: () => void
): HTMLElement {
    const container = document.createElement('div');
    container.className = CSS.PANEL.ERROR;
    container.setAttribute('role', 'alert');

    // Error title
    const titleEl = document.createElement('div');
    titleEl.className = 'panel-error-title';
    titleEl.textContent = error.title;
    container.appendChild(titleEl);

    // Error message
    const messageEl = document.createElement('div');
    messageEl.className = 'panel-error-message';
    messageEl.textContent = error.message;
    container.appendChild(messageEl);

    // Suggestion if provided
    if (error.suggestion) {
        const suggestionEl = document.createElement('div');
        suggestionEl.className = 'panel-error-suggestion';
        suggestionEl.textContent = error.suggestion;
        container.appendChild(suggestionEl);
    }

    // Expandable details if provided
    if (error.details) {
        const detailsEl = document.createElement('details');
        detailsEl.className = 'panel-error-details';

        const summaryEl = document.createElement('summary');
        summaryEl.textContent = 'Error Details';
        detailsEl.appendChild(summaryEl);

        const preEl = document.createElement('pre');
        preEl.className = 'panel-error-details-content';
        preEl.textContent = error.details;
        detailsEl.appendChild(preEl);

        container.appendChild(detailsEl);
    }

    // Retry button if callback provided
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
 * Parse an error into a RichError object
 * Handles various error formats including HTTP errors, network errors, etc.
 */
export function parseError(error: unknown): RichError {
    // Already a RichError
    if (isRichError(error)) {
        return error;
    }

    // Standard Error object
    if (error instanceof Error) {
        return parseErrorObject(error);
    }

    // String error
    if (typeof error === 'string') {
        return {
            title: 'Error',
            message: error
        };
    }

    // Unknown error type
    return {
        title: 'Unknown Error',
        message: String(error)
    };
}

/**
 * Check if an object is already a RichError
 */
function isRichError(obj: unknown): obj is RichError {
    return typeof obj === 'object' &&
           obj !== null &&
           'title' in obj &&
           'message' in obj &&
           typeof (obj as RichError).title === 'string' &&
           typeof (obj as RichError).message === 'string';
}

/**
 * Parse a standard Error object into RichError
 */
function parseErrorObject(error: Error): RichError {
    const message = error.message;

    // Check for HTTP status codes in the message
    const httpMatch = message.match(/HTTP\s*(\d{3})/i);
    if (httpMatch) {
        const status = parseInt(httpMatch[1], 10);
        return parseHttpError(status, message, error.stack);
    }

    // Check for network errors
    if (message.includes('NetworkError') || message.includes('Failed to fetch') || message.includes('Network request failed')) {
        return {
            title: 'Network Error',
            message: 'Unable to connect to the server',
            suggestion: 'Check your network connection and ensure the QNTX server is running',
            details: error.stack
        };
    }

    // Check for timeout errors
    if (message.includes('timeout') || message.includes('Timeout')) {
        return {
            title: 'Request Timeout',
            message: 'The server took too long to respond',
            suggestion: 'Try again or check if the server is under heavy load',
            details: error.stack
        };
    }

    // Generic error
    return {
        title: 'Error',
        message: message,
        details: error.stack
    };
}

/**
 * Parse HTTP error status into RichError
 */
function parseHttpError(status: number, message: string, stack?: string): RichError {
    const statusMessages: Record<number, { title: string; suggestion: string }> = {
        400: { title: 'Bad Request', suggestion: 'Check your input and try again' },
        401: { title: 'Unauthorized', suggestion: 'You may need to log in or refresh your session' },
        403: { title: 'Forbidden', suggestion: 'You do not have permission to access this resource' },
        404: { title: 'Not Found', suggestion: 'The requested resource does not exist' },
        408: { title: 'Request Timeout', suggestion: 'The server took too long to respond' },
        422: { title: 'Validation Error', suggestion: 'Check your input for errors' },
        429: { title: 'Too Many Requests', suggestion: 'Please wait before trying again' },
        500: { title: 'Internal Server Error', suggestion: 'An unexpected error occurred on the server' },
        502: { title: 'Bad Gateway', suggestion: 'The server is temporarily unavailable' },
        503: { title: 'Service Unavailable', suggestion: 'The server is temporarily unavailable, try again later' },
        504: { title: 'Gateway Timeout', suggestion: 'The server took too long to respond' }
    };

    const info = statusMessages[status] || {
        title: `HTTP Error ${status}`,
        suggestion: 'An unexpected error occurred'
    };

    return {
        title: info.title,
        message: message,
        status: status,
        suggestion: info.suggestion,
        details: stack
    };
}

/**
 * Display an error state in the panel content area
 * Uses rich error display with expandable details
 */
export function showErrorState(ctx: ErrorHandlingContext, error: Error): void {
    ctx.errorState.hasError = true;
    ctx.errorState.lastError = error;

    const content = ctx.$<HTMLElement>(`.${CSS.PANEL.CONTENT}`);
    if (!content) {
        console.warn(`[${ctx.panelId}] No .${CSS.PANEL.CONTENT} element found for error display`);
        return;
    }

    // Parse the error into a rich error format
    const richError = parseError(error);

    // Clear existing content and show rich error
    content.innerHTML = '';
    const errorEl = createRichErrorState(richError, () => retryShow(ctx));
    content.appendChild(errorEl);

    // Set loading state to error for CSS styling
    setLoading(content, DATA.LOADING.ERROR);
}

/**
 * Display a rich error in the panel content area
 * Allows passing a pre-formatted RichError
 */
export function showRichError(ctx: ErrorHandlingContext, error: RichError, onRetry?: () => void): void {
    ctx.errorState.hasError = true;
    ctx.errorState.lastError = new Error(error.message);

    const content = ctx.$<HTMLElement>(`.${CSS.PANEL.CONTENT}`);
    if (!content) {
        console.warn(`[${ctx.panelId}] No .${CSS.PANEL.CONTENT} element found for error display`);
        return;
    }

    // Clear existing content and show rich error
    content.innerHTML = '';
    const errorEl = createRichErrorState(error, onRetry || (() => retryShow(ctx)));
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
