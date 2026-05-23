/**
 * HTTP transport — apiFetch with connectivity reporting.
 *
 * Imports directly from sibling modules (url, connectivity).
 * No circular imports — all dependencies are leaf modules.
 */

import { backendUrl } from './url';
import { connectivity } from './connectivity';

/**
 * Fetch wrapper that uses backend URL.
 * Reports HTTP health to connectivity manager:
 *   - Any response (including 4xx/5xx) = HTTP healthy
 *   - Network-level failure (fetch throws) = HTTP failure
 * Reports 401 responses to connectivity manager as unauthenticated state.
 */
export function apiFetch(path: string, init?: RequestInit): Promise<Response> {
    const url = backendUrl() + path;
    // credentials: 'include' ensures cookies are sent on cross-origin requests
    // (dev mode: frontend on :8826, backend on :8776 — different origin, same site)
    const fetchInit: RequestInit = { credentials: 'include', ...init };
    return fetch(url, fetchInit).then(
        response => {
            connectivity.reportHttpSuccess();
            if (response.status === 401 && !path.startsWith('/auth/')) {
                connectivity.reportUnauthenticated();
            } else if (response.status !== 401) {
                connectivity.reportAuthenticated();
            }
            return response;
        },
        error => {
            connectivity.reportHttpFailure();
            throw error;
        }
    );
}
