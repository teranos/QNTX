/**
 * API utilities - provides backend URL resolution
 */

import { connectivityManager } from './connectivity';

/**
 * Get the backend base URL from injected global or current origin
 */
function getBackendUrl(): string {
    return (window as any).__BACKEND_URL__ || window.location.origin;
}

/**
 * Fetch wrapper that uses backend URL.
 * Reports HTTP health to connectivity manager:
 *   - Any response (including 4xx/5xx) = HTTP healthy
 *   - Network-level failure (fetch throws) = HTTP failure
 */
export function apiFetch(path: string, init?: RequestInit): Promise<Response> {
    const url = getBackendUrl() + path;
    return fetch(url, init).then(
        response => {
            connectivityManager.reportHttpSuccess();
            return response;
        },
        error => {
            connectivityManager.reportHttpFailure();
            throw error;
        }
    );
}
