/**
 * Backend URL resolution — single source of truth.
 *
 * Leaf module with no imports from client/. Safe to import from any submodule
 * without risk of circular dependencies.
 */

import { stripProtocol } from '../http-utils';

/** Backend base URL from injected global or current origin */
export function backendUrl(): string {
    return (window as any).__BACKEND_URL__ || window.location.origin;
}

/** WebSocket URL (ws[s]://host) — no regex, uses stripProtocol */
export function backendWsUrl(): string {
    const url = backendUrl();
    const host = stripProtocol(url);
    const protocol = url.startsWith('https') ? 'wss:' : 'ws:';
    return `${protocol}//${host}`;
}

/** Full URL for EventSource, img src, etc. */
export function backendPath(path: string): string {
    return backendUrl() + path;
}
