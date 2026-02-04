/**
 * API utilities - provides backend URL resolution
 */

import { getApiUrl } from './backend-url.ts';

/**
 * Fetch wrapper that uses backend URL
 */
export function apiFetch(path: string, init?: RequestInit): Promise<Response> {
    return fetch(getApiUrl(path), init);
}
