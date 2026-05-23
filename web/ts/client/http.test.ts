/**
 * Tests for apiFetch HTTP transport.
 *
 * Controls backendUrl via window.__BACKEND_URL__ (no module mock needed).
 * Mocks connectivity to verify reporting calls.
 */

import { describe, test, expect, mock, beforeEach, afterEach } from 'bun:test';

// Mock connectivity (prevents real singleton with timers/listeners)
const mockReportHttpSuccess = mock(() => {});
const mockReportHttpFailure = mock(() => {});
const mockReportUnauthenticated = mock(() => {});
const mockReportAuthenticated = mock(() => {});

mock.module('./connectivity', () => ({
    connectivity: {
        reportHttpSuccess: mockReportHttpSuccess,
        reportHttpFailure: mockReportHttpFailure,
        reportUnauthenticated: mockReportUnauthenticated,
        reportAuthenticated: mockReportAuthenticated,
    },
}));

const { apiFetch } = await import('./http');

const originalFetch = globalThis.fetch;

beforeEach(() => {
    (window as any).__BACKEND_URL__ = 'http://backend:8771';
    mockReportHttpSuccess.mockClear();
    mockReportHttpFailure.mockClear();
    mockReportUnauthenticated.mockClear();
    mockReportAuthenticated.mockClear();
});

afterEach(() => {
    globalThis.fetch = originalFetch;
    delete (window as any).__BACKEND_URL__;
});

describe('Tim: apiFetch prepends backendUrl', () => {
    test('prepends backend URL to path', async () => {
        let capturedUrl = '';
        globalThis.fetch = mock((url: any, _init: any) => {
            capturedUrl = url;
            return Promise.resolve(new Response('ok', { status: 200 }));
        }) as any;

        await apiFetch('/api/canvas/glyphs');

        expect(capturedUrl).toBe('http://backend:8771/api/canvas/glyphs');
    });

    test('includes credentials: include', async () => {
        let capturedInit: RequestInit = {};
        globalThis.fetch = mock((_url: any, init: any) => {
            capturedInit = init;
            return Promise.resolve(new Response('ok', { status: 200 }));
        }) as any;

        await apiFetch('/api/test');

        expect(capturedInit.credentials).toBe('include');
    });
});
