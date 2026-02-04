/**
 * Tests for backend-url.ts utility functions
 *
 * These tests verify the critical functionality that consumers depend on:
 * 1. Backend URL resolution with fallback
 * 2. WebSocket URL protocol conversion
 * 3. API URL construction
 */

import { describe, expect, test, beforeEach, afterEach } from 'bun:test';
import { getBackendUrl, getWebSocketUrl, getApiUrl } from './backend-url.ts';

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
        // When configured, use it
        (window as any).__BACKEND_URL__ = 'http://localhost:877';
        expect(getBackendUrl()).toBe('http://localhost:877');

        // When not configured, fall back to window origin
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
});
