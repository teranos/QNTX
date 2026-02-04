/**
 * Tests for backend-url.ts utility functions
 */

import { describe, expect, test, beforeEach, afterEach } from 'bun:test';
import {
    validateBackendUrl,
    getBackendUrl,
    getValidatedBackendUrl,
    getWebSocketUrl,
    getTypedWebSocketUrl,
    getApiUrl
} from './backend-url.ts';

describe('backend-url', () => {
    // Store original window values for restoration
    let originalBackendUrl: any;
    let originalLocation: any;

    beforeEach(() => {
        // Save original values
        originalBackendUrl = (window as any).__BACKEND_URL__;
        originalLocation = window.location;

        // Mock window.location.origin
        Object.defineProperty(window, 'location', {
            value: { origin: 'http://localhost:8080' },
            writable: true
        });
    });

    afterEach(() => {
        // Restore original values
        (window as any).__BACKEND_URL__ = originalBackendUrl;
        Object.defineProperty(window, 'location', {
            value: originalLocation,
            writable: true
        });
    });

    describe('validateBackendUrl', () => {
        test('returns origin for valid http URL', () => {
            expect(validateBackendUrl('http://localhost:877')).toBe('http://localhost:877');
        });

        test('returns origin for valid https URL', () => {
            expect(validateBackendUrl('https://api.example.com')).toBe('https://api.example.com');
        });

        test('returns origin for URL with path (strips path)', () => {
            expect(validateBackendUrl('http://localhost:877/api/v1')).toBe('http://localhost:877');
        });

        test('returns null for invalid protocol (file:)', () => {
            expect(validateBackendUrl('file:///etc/passwd')).toBe(null);
        });

        test('returns null for invalid protocol (ftp:)', () => {
            expect(validateBackendUrl('ftp://example.com')).toBe(null);
        });

        test('returns null for invalid protocol (javascript:)', () => {
            expect(validateBackendUrl('javascript:alert(1)')).toBe(null);
        });

        test('resolves relative URL against window.location.origin', () => {
            // The URL constructor treats relative URLs as valid, resolving against base
            expect(validateBackendUrl('/api')).toBe('http://localhost:8080');
        });
    });

    describe('getBackendUrl', () => {
        test('returns __BACKEND_URL__ when set', () => {
            (window as any).__BACKEND_URL__ = 'http://localhost:877';
            expect(getBackendUrl()).toBe('http://localhost:877');
        });

        test('returns window.location.origin when __BACKEND_URL__ not set', () => {
            (window as any).__BACKEND_URL__ = undefined;
            expect(getBackendUrl()).toBe('http://localhost:8080');
        });

        test('returns window.location.origin when __BACKEND_URL__ is empty string', () => {
            (window as any).__BACKEND_URL__ = '';
            expect(getBackendUrl()).toBe('http://localhost:8080');
        });
    });

    describe('getValidatedBackendUrl', () => {
        test('returns validated __BACKEND_URL__ when valid', () => {
            (window as any).__BACKEND_URL__ = 'http://localhost:877';
            expect(getValidatedBackendUrl()).toBe('http://localhost:877');
        });

        test('falls back to origin when __BACKEND_URL__ is invalid', () => {
            (window as any).__BACKEND_URL__ = 'ftp://invalid';
            expect(getValidatedBackendUrl()).toBe('http://localhost:8080');
        });
    });

    describe('getWebSocketUrl', () => {
        test('converts http to ws protocol', () => {
            (window as any).__BACKEND_URL__ = 'http://localhost:877';
            expect(getWebSocketUrl('/ws')).toBe('ws://localhost:877/ws');
        });

        test('converts https to wss protocol', () => {
            (window as any).__BACKEND_URL__ = 'https://api.example.com';
            expect(getWebSocketUrl('/ws')).toBe('wss://api.example.com/ws');
        });

        test('works with different paths', () => {
            (window as any).__BACKEND_URL__ = 'http://localhost:877';
            expect(getWebSocketUrl('/lsp')).toBe('ws://localhost:877/lsp');
            expect(getWebSocketUrl('/gopls')).toBe('ws://localhost:877/gopls');
        });
    });

    describe('getTypedWebSocketUrl', () => {
        test('returns correctly typed WebSocket URL', () => {
            (window as any).__BACKEND_URL__ = 'http://localhost:877';
            const url = getTypedWebSocketUrl('/lsp');
            // TypeScript should accept this assignment
            const _typed: `ws://${string}` | `wss://${string}` = url;
            expect(url).toBe('ws://localhost:877/lsp');
        });
    });

    describe('getApiUrl', () => {
        test('returns full API URL with path', () => {
            (window as any).__BACKEND_URL__ = 'http://localhost:877';
            expect(getApiUrl('/api/pulse')).toBe('http://localhost:877/api/pulse');
        });

        test('handles paths without leading slash', () => {
            (window as any).__BACKEND_URL__ = 'http://localhost:877';
            expect(getApiUrl('api/pulse')).toBe('http://localhost:877api/pulse');
        });
    });
});
