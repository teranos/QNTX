/**
 * Backend URL Resolution Utilities
 *
 * Centralizes all backend URL resolution logic to ensure consistent
 * behavior across WebSocket connections, API calls, and LSP clients.
 *
 * URL Resolution Priority:
 * 1. window.__BACKEND_URL__ (injected by dev server or Tauri)
 * 2. window.location.origin (same-origin fallback)
 */

/**
 * Validate and sanitize a backend URL
 * Returns the validated URL origin or null if invalid
 *
 * @param url - URL to validate
 * @returns Validated URL origin or null if invalid
 */
export function validateBackendUrl(url: string): string | null {
    try {
        const parsed = new URL(url, window.location.origin);

        // Only allow http/https protocols (will be converted to ws/wss)
        if (!['http:', 'https:'].includes(parsed.protocol)) {
            return null;
        }

        return parsed.origin;
    } catch {
        return null;
    }
}

/**
 * Get the backend base URL
 * Resolves from injected global or falls back to same-origin
 *
 * @returns Backend base URL (e.g., "http://localhost:877")
 */
export function getBackendUrl(): string {
    return (window as any).__BACKEND_URL__ || window.location.origin;
}

/**
 * Get the backend base URL with validation
 * Returns validated URL or falls back to same-origin if invalid
 *
 * @returns Validated backend base URL
 */
export function getValidatedBackendUrl(): string {
    const rawUrl = (window as any).__BACKEND_URL__ || window.location.origin;
    const validatedUrl = validateBackendUrl(rawUrl);
    return validatedUrl || window.location.origin;
}

/**
 * Get the WebSocket URL for a given path
 * Converts http(s) to ws(s) protocol automatically
 *
 * @param path - WebSocket endpoint path (e.g., "/ws", "/lsp", "/gopls")
 * @returns Full WebSocket URL (e.g., "ws://localhost:877/ws")
 */
export function getWebSocketUrl(path: string): string {
    const backendUrl = getValidatedBackendUrl();
    const backendHost = backendUrl.replace(/^https?:\/\//, '');
    const protocol = backendUrl.startsWith('https') ? 'wss:' : 'ws:';
    return `${protocol}//${backendHost}${path}`;
}

/**
 * Get a typed WebSocket URL for use with codemirror-languageserver
 * Returns URL with proper type annotation
 *
 * @param path - WebSocket endpoint path (e.g., "/lsp", "/gopls")
 * @returns Typed WebSocket URL
 */
export function getTypedWebSocketUrl(path: string): `ws://${string}` | `wss://${string}` {
    return getWebSocketUrl(path) as `ws://${string}` | `wss://${string}`;
}

/**
 * Get the full API URL for a given path
 *
 * @param path - API endpoint path (e.g., "/api/pulse/schedules")
 * @returns Full API URL
 */
export function getApiUrl(path: string): string {
    return getBackendUrl() + path;
}
