/**
 * Tests for backend-url.ts utility functions
 */

import { describe, expect, test } from 'bun:test';
import { validateBackendUrl } from './backend-url.ts';

describe('backend-url', () => {
    test('validates http/https URLs', () => {
        expect(validateBackendUrl('http://localhost:877')).toBe('http://localhost:877');
        expect(validateBackendUrl('https://api.example.com')).toBe('https://api.example.com');
    });

    test('rejects dangerous protocols', () => {
        expect(validateBackendUrl('javascript:alert(1)')).toBe(null);
        expect(validateBackendUrl('file:///etc/passwd')).toBe(null);
    });

    test('strips path from URL, returning only origin', () => {
        expect(validateBackendUrl('http://localhost:877/api/v1/data')).toBe('http://localhost:877');
    });
});
