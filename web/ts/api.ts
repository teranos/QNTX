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
 * Reports 401 responses to connectivity manager as unauthenticated state.
 */
export function apiFetch(path: string, init?: RequestInit): Promise<Response> {
    const url = getBackendUrl() + path;
    // credentials: 'include' ensures cookies are sent on cross-origin requests
    // (dev mode: frontend on :8826, backend on :8776 — different origin, same site)
    const fetchInit: RequestInit = { credentials: 'include', ...init };
    return fetch(url, fetchInit).then(
        response => {
            connectivityManager.reportHttpSuccess();
            if (response.status === 401 && !path.startsWith('/auth/')) {
                connectivityManager.reportUnauthenticated();
            } else if (response.status !== 401) {
                connectivityManager.reportAuthenticated();
            }
            return response;
        },
        error => {
            connectivityManager.reportHttpFailure();
            throw error;
        }
    );
}

/**
 * Get the backend auth login URL for explicit user-initiated navigation.
 */
export function getAuthLoginUrl(): string {
    return getBackendUrl() + '/auth/login';
}
