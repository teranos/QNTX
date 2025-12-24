/**
 * API utilities - provides backend URL resolution
 */

/**
 * Get the backend base URL from injected global or current origin
 */
function getBackendUrl(): string {
    return (window as any).__BACKEND_URL__ || window.location.origin;
}

/**
 * Fetch wrapper that uses backend URL
 */
export function apiFetch(path: string, init?: RequestInit): Promise<Response> {
    const url = getBackendUrl() + path;
    return fetch(url, init);
}
