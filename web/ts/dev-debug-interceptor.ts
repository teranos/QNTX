/**
 * Debug Interceptor - Captures browser console logs and sends to backend
 *
 * In dev mode, intercepts console methods and reports them to /api/debug
 * endpoint, enabling automated tools to see frontend errors without manual
 * browser inspection.
 */

import { apiFetch } from './api.ts';
import { fetchDevMode } from './dev-mode.ts';

interface ConsoleLog {
    timestamp?: string;
    level: string;
    message: string;
    stack?: string;
    url?: string;
}

let isInitialized = false;
const originalConsole = {
    log: console.log.bind(console),
    warn: console.warn.bind(console),
    error: console.error.bind(console),
    info: console.info.bind(console),
    debug: console.debug.bind(console),
};

/**
 * Format arguments to string (handle objects, arrays, etc.)
 */
function formatArgs(args: any[]): string {
    return args.map(arg => {
        if (typeof arg === 'string') {
            return arg;
        }
        if (arg instanceof Error) {
            return arg.message;
        }
        try {
            return JSON.stringify(arg);
        } catch {
            return String(arg);
        }
    }).join(' ');
}

/**
 * Send console log to backend
 */
async function sendLog(level: string, args: any[]): Promise<void> {
    const message = formatArgs(args);

    // Get stack trace for errors
    let stack: string | undefined;
    if (level === 'error') {
        const err = args.find(arg => arg instanceof Error) as Error | undefined;
        if (err?.stack) {
            stack = err.stack;
        } else {
            // Try to get a stack trace
            try {
                throw new Error();
            } catch (error: unknown) {
                stack = (error as any).stack;
            }
        }
    }

    const log: ConsoleLog = {
        level,
        message,
        stack,
        url: window.location.href,
    };

    try {
        // Send to backend (fire and forget, don't await)
        apiFetch('/api/debug', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify(log),
        }).catch((error: unknown) => {
            // Silently fail if backend is unavailable
            // Use original console to avoid infinite loop
            // Use original console to avoid infinite loop and logging infrastructure issues
            originalConsole.debug('[Debug Interceptor] Failed to send log:', error);
        });
    } catch (error: unknown) {
        // Silently fail - use original console to avoid infinite loop
        originalConsole.debug('[Debug Interceptor] Failed to send log:', error);
    }
}

/**
 * Initialize debug interceptor (dev mode only)
 */
export async function initDebugInterceptor(): Promise<void> {
    if (isInitialized) {
        return;
    }

    // Fetch dev mode status
    const devModeEnabled = await fetchDevMode();
    if (!devModeEnabled) {
        originalConsole.log('[Debug Interceptor] Dev mode disabled, debug interception disabled');
        return;
    }

    isInitialized = true;

    // Intercept console methods
    console.log = function(...args: any[]) {
        originalConsole.log(...args);
        sendLog('info', args);
    };

    console.info = function(...args: any[]) {
        originalConsole.info(...args);
        sendLog('info', args);
    };

    console.warn = function(...args: any[]) {
        originalConsole.warn(...args);
        sendLog('warn', args);
    };

    console.error = function(...args: any[]) {
        originalConsole.error(...args);
        sendLog('error', args);
    };

    console.debug = function(...args: any[]) {
        originalConsole.debug(...args);
        sendLog('debug', args);
    };

    // Log to original console since we've already hooked it
    originalConsole.log('[Debug Interceptor] Initialized - console logs will be sent to backend');
}

/**
 * Restore original console methods
 */
export function disableDebugInterceptor(): void {
    if (!isInitialized) {
        return;
    }

    console.log = originalConsole.log;
    console.info = originalConsole.info;
    console.warn = originalConsole.warn;
    console.error = originalConsole.error;
    console.debug = originalConsole.debug;

    isInitialized = false;
    originalConsole.log('[Debug Interceptor] Disabled');
}
