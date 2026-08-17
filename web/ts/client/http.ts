/**
 * HTTP transport — apiFetch with connectivity reporting.
 *
 * Imports directly from sibling modules (url, connectivity).
 * No circular imports — all dependencies are leaf modules.
 */

import { backendUrl } from './url';
import { connectivity } from './connectivity';

/**
 * Fetch + assertOk + JSON parse in one call.
 * Use apiFetch directly when you need the raw Response
 * (streaming, text responses, status-specific branching).
 */
export async function apiJson<T>(path: string, init?: RequestInit): Promise<T> {
    const response = await apiFetch(path, init);
    if (!response.ok) {
        const body = await response.text().catch(() => '');
        throw new Error(`${path}: HTTP ${response.status} ${body || response.statusText}`);
    }
    return await response.json() as T;
}

/**
 * Fetch wrapper that uses backend URL. It reports what it saw, never what it
 * worked out: a response arriving means the node was reachable, and a 401 means
 * the node said you are not signed in.
 */
export function apiFetch(path: string, init?: RequestInit): Promise<Response> {
    const url = backendUrl() + path;
    // credentials: 'include' ensures cookies are sent on cross-origin requests
    // (dev mode: frontend on :8826, backend on :8776 — different origin, same site)
    const fetchInit: RequestInit = { credentials: 'include', ...init };
    return fetch(url, fetchInit).then(
        response => {
            connectivity.reportReachable();
            // A status that is not 401 says nothing about who you are. Reading
            // it as "signed in" made a 500 report an identity, which is how a
            // node that could not read a credential showed you as logged in.
            if (response.status === 401 && !path.startsWith('/auth/')) {
                connectivity.reportUnauthenticated();
            }
            return response;
        },
        error => {
            connectivity.reportHttpFailure(url, error);
            throw error;
        }
    );
}
