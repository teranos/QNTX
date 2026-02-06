/**
 * Dev Mode Detection
 *
 * Checks if the server is running in development mode (--dev flag).
 * Used across the app to enable/disable editing features.
 */

import { apiFetch } from './api.ts';
import { log, SEG } from './logger.ts';

let devMode: boolean | null = null;

/**
 * Fetch and cache dev mode status from server
 */
export async function fetchDevMode(): Promise<boolean> {
    if (devMode !== null) {
        return devMode;
    }

    try {
        const response = await apiFetch('/api/dev');
        if (!response.ok) {
            log.warn(SEG.SELF, 'Failed to fetch dev mode status, defaulting to false');
            devMode = false;
            return devMode;
        }
        const text = await response.text();
        devMode = text.trim() === 'true';
        return devMode;
    } catch (error: unknown) {
        log.error(SEG.SELF, 'Failed to fetch dev mode:', error);
        devMode = false;
        return devMode;
    }
}

/**
 * Get cached dev mode status (returns null if not yet fetched)
 */
export function getDevMode(): boolean | null {
    return devMode;
}

/**
 * Check if dev mode is enabled (fetches if not cached)
 */
export async function isDevMode(): Promise<boolean> {
    return await fetchDevMode();
}
