/**
 * Tests for backend URL resolution.
 */

import { describe, test, expect, afterEach } from 'bun:test';
import { backendUrl } from './url';

describe('Tim: backendUrl', () => {
    afterEach(() => {
        delete (window as any).__BACKEND_URL__;
    });

    test('returns __BACKEND_URL__ when set', () => {
        (window as any).__BACKEND_URL__ = 'http://custom-backend:9999';
        expect(backendUrl()).toBe('http://custom-backend:9999');
    });

    test('falls back to window.location.origin when not set', () => {
        delete (window as any).__BACKEND_URL__;
        expect(backendUrl()).toBe(window.location.origin);
    });
});
