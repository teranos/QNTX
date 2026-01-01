/**
 * API utilities - provides backend URL resolution
 */

/**
 * Get the backend base URL from injected global or current origin
 * In Tauri, defaults to http://localhost:877
 */
function getBackendUrl(): string {
    const isTauri = window.location.protocol === 'tauri:';
    const defaultUrl = isTauri ? 'http://localhost:877' : window.location.origin;
    return (window as any).__BACKEND_URL__ || defaultUrl;
}

/**
 * Fetch wrapper that uses backend URL
 */
export function apiFetch(path: string, init?: RequestInit): Promise<Response> {
    const url = getBackendUrl() + path;
    return fetch(url, init);
}
