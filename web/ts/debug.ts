/**
 * Debug Logging Utility
 *
 * Provides conditional console logging based on dev mode.
 * Logs are only shown when server is running with --dev flag.
 */

import { getDevMode } from './dev-mode.ts';

/**
 * Debug log - only logs in dev mode
 */
export function debugLog(...args: any[]): void {
    if (getDevMode()) {
        console.log(...args);
    }
}

/**
 * Debug warn - only logs in dev mode
 */
export function debugWarn(...args: any[]): void {
    if (getDevMode()) {
        console.warn(...args);
    }
}

/**
 * Debug error - only logs in dev mode
 * NOTE: Real errors should still use console.error() directly
 */
export function debugError(...args: any[]): void {
    if (getDevMode()) {
        console.error(...args);
    }
}

/**
 * Always log - bypasses dev mode check (for critical logs)
 */
export function alwaysLog(...args: any[]): void {
    console.log(...args);
}
