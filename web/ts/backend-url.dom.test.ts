/**
 * Tests for backend-url.ts utility functions
 */

import { describe, expect, test, beforeEach, afterEach } from 'bun:test';
import { getBackendUrl, getWebSocketUrl, getApiUrl, validateBackendUrl } from './backend-url.ts';

describe('backend-url', () => {
    let originalBackendUrl: any;
    let originalLocation: any;

    beforeEach(() => {
        originalBackendUrl = (window as any).__BACKEND_URL__;
        originalLocation = window.location;
        Object.defineProperty(window, 'location', {
            value: { origin: 'http://localhost:8080' },
            writable: true
        });
    });

    afterEach(() => {
        (window as any).__BACKEND_URL__ = originalBackendUrl;
        Object.defineProperty(window, 'location', {
            value: originalLocation,
            writable: true
        });
    });

    test('uses configured backend URL when available, falls back to origin otherwise', () => {
        (window as any).__BACKEND_URL__ = 'http://localhost:877';
        expect(getBackendUrl()).toBe('http://localhost:877');

        (window as any).__BACKEND_URL__ = undefined;
        expect(getBackendUrl()).toBe('http://localhost:8080');
    });

    test('WebSocket URLs use ws:// for http and wss:// for https', () => {
        (window as any).__BACKEND_URL__ = 'http://localhost:877';
        expect(getWebSocketUrl('/ws')).toBe('ws://localhost:877/ws');

        (window as any).__BACKEND_URL__ = 'https://api.example.com';
        expect(getWebSocketUrl('/ws')).toBe('wss://api.example.com/ws');
    });

    test('API URLs combine backend URL with path', () => {
        (window as any).__BACKEND_URL__ = 'http://localhost:877';
        expect(getApiUrl('/api/pulse')).toBe('http://localhost:877/api/pulse');
    });

    test('validates http/https URLs and rejects dangerous protocols', () => {
        expect(validateBackendUrl('http://localhost:877')).toBe('http://localhost:877');
        expect(validateBackendUrl('https://api.example.com')).toBe('https://api.example.com');
        expect(validateBackendUrl('javascript:alert(1)')).toBe(null);
        expect(validateBackendUrl('file:///etc/passwd')).toBe(null);
    });

    test('strips path from URL, returning only origin', () => {
        expect(validateBackendUrl('http://localhost:877/api/v1/data')).toBe('http://localhost:877');
    });
});
