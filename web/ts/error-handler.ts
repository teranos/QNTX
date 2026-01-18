/**
 * Unified Error Handler
 *
 * Provides consistent error handling across the QNTX web UI.
 * Integrates with the logger for console output and toast for user notifications.
 *
 * Benefits:
 * - Normalizes unknown catch values to Error objects
 * - Consistent logging with SEG context
 * - Optional user-facing toast notifications
 * - Reduces boilerplate in catch blocks
 *
 * Usage:
 *   import { handleError, createErrorHandler } from './error-handler';
 *
 *   // Basic usage - logs and shows toast
 *   try {
 *     await fetchData();
 *   } catch (e) {
 *     handleError(e, 'Failed to fetch data');
 *   }
 *
 *   // Silent mode - logs only, no toast
 *   handleError(e, 'Background sync failed', { silent: true });
 *
 *   // With custom SEG context
 *   handleError(e, 'Job execution failed', { context: SEG.PULSE });
 *
 *   // Create a reusable handler for a component
 *   const handlePanelError = createErrorHandler('PulsePanel', SEG.PULSE);
 *   handlePanelError(e, 'Failed to load jobs');
 *
 * TODO: Migrate catch blocks in these files to use handleError:
 *   - pulse/job-detail-panel.ts (5 catch blocks)
 *   - pulse/scheduling-controls.ts (4 catch blocks)
 *   - pulse/api.ts (3 catch blocks)
 *   - pulse/ats-node-view.ts (1 catch block)
 *   - pulse/active-queue.ts (1 catch block)
 *   - pulse/panel-state.ts (2 catch blocks)
 *   - pulse/realtime-handlers.ts (1 catch block)
 *   - pulse/execution-api.ts (1 catch block)
 *   - ai-provider-window.ts (6 catch blocks)
 *   - plugin-panel.ts (7 catch blocks)
 *   - config-panel.ts (2 catch blocks)
 *   - code/panel.ts (7 catch blocks)
 *   - code/suggestions.ts (2 catch blocks)
 *   - prose/editor.ts (3 catch blocks)
 *   - filetree/navigator.ts (3 catch blocks)
 *   - python/panel.ts (5 catch blocks)
 *   - webscraper-panel.ts (1 catch block)
 */

import { log, SEG } from './logger';
import { toast } from './toast';

/**
 * Options for error handling behavior
 */
export interface ErrorHandlerOptions {
    /** SEG symbol for log context (default: SEG.ERROR) */
    context?: string;
    /** If true, don't show toast notification (default: false) */
    silent?: boolean;
    /** If true, show build info in toast (default: false for errors) */
    showBuildInfo?: boolean;
}

/**
 * Normalize any caught value to an Error object
 * Handles: Error, string, object with message, unknown
 */
export function normalizeError(error: unknown): Error {
    if (error instanceof Error) {
        return error;
    }
    if (typeof error === 'string') {
        return new Error(error);
    }
    if (error && typeof error === 'object' && 'message' in error) {
        return new Error(String((error as { message: unknown }).message));
    }
    return new Error(String(error));
}

/**
 * Extract a user-friendly message from an error
 * Strips technical prefixes and formats for display
 */
export function getUserMessage(error: Error): string {
    let message = error.message;

    // Remove common technical prefixes
    message = message.replace(/^(Error|TypeError|ReferenceError|NetworkError):\s*/i, '');

    // Truncate very long messages
    if (message.length > 200) {
        message = message.substring(0, 197) + '...';
    }

    return message;
}

/**
 * Handle an error with logging and optional toast notification
 *
 * @param error - The caught error (any type)
 * @param userMessage - Human-readable description for the user
 * @param options - Additional options for handling behavior
 * @returns The normalized Error object for further handling if needed
 */
export function handleError(
    error: unknown,
    userMessage: string,
    options: ErrorHandlerOptions = {}
): Error {
    const {
        context = SEG.ERROR,
        silent = false,
        showBuildInfo = false,
    } = options;

    const err = normalizeError(error);

    // Always log to console with full details
    log.error(context, userMessage, err.message, err);

    // Show toast unless silent mode
    if (!silent) {
        // Combine user message with error details if they differ
        const errorDetail = getUserMessage(err);
        const toastMessage = errorDetail && errorDetail !== userMessage
            ? `${userMessage}: ${errorDetail}`
            : userMessage;

        toast.error(toastMessage, showBuildInfo);
    }

    return err;
}

/**
 * Handle an error silently (log only, no toast)
 * Convenience wrapper for handleError with silent: true
 */
export function handleErrorSilent(
    error: unknown,
    userMessage: string,
    context: string = SEG.ERROR
): Error {
    return handleError(error, userMessage, { context, silent: true });
}

/**
 * Create a scoped error handler for a specific component/module
 * Returns a function with pre-configured context
 *
 * @param componentName - Name of the component for logging
 * @param defaultContext - Default SEG symbol for this handler
 */
export function createErrorHandler(
    componentName: string,
    defaultContext: string = SEG.UI
): (error: unknown, message: string, options?: ErrorHandlerOptions) => Error {
    return (error: unknown, message: string, options: ErrorHandlerOptions = {}) => {
        const prefixedMessage = `[${componentName}] ${message}`;
        return handleError(error, prefixedMessage, {
            context: defaultContext,
            ...options,
        });
    };
}

/**
 * Wrap an async function with error handling
 * Useful for event handlers and callbacks
 *
 * @param fn - Async function to wrap
 * @param errorMessage - Message to show if function throws
 * @param options - Error handler options
 */
export function withErrorHandling<T extends (...args: unknown[]) => Promise<unknown>>(
    fn: T,
    errorMessage: string,
    options: ErrorHandlerOptions = {}
): (...args: Parameters<T>) => Promise<ReturnType<T> | undefined> {
    return async (...args: Parameters<T>): Promise<ReturnType<T> | undefined> => {
        try {
            return await fn(...args) as ReturnType<T>;
        } catch (error: unknown) {
            handleError(error, errorMessage, options);
            return undefined;
        }
    };
}

/**
 * Re-export SEG for convenience when importing error-handler
 */
export { SEG } from './logger';
