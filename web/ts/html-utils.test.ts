/**
 * Tests for HTML Utilities
 */

import { describe, test, expect } from 'bun:test';
import { escapeHtml, formatRelativeTime, formatValue } from './html-utils';

describe('escapeHtml', () => {
    test('escapes HTML special characters', () => {
        expect(escapeHtml('<script>alert("xss")</script>'))
            .toBe('&lt;script&gt;alert("xss")&lt;/script&gt;');
    });

    test('escapes ampersands', () => {
        expect(escapeHtml('foo & bar')).toBe('foo &amp; bar');
    });

    test('escapes quotes', () => {
        expect(escapeHtml('Say "hello"')).toBe('Say "hello"');
    });

    test('escapes single quotes', () => {
        expect(escapeHtml("It's working")).toBe("It's working");
    });

    test('handles empty string', () => {
        expect(escapeHtml('')).toBe('');
    });

    test('handles plain text without special characters', () => {
        expect(escapeHtml('hello world')).toBe('hello world');
    });

    test('escapes multiple special characters', () => {
        expect(escapeHtml('<div class="test">foo & bar</div>'))
            .toBe('&lt;div class="test"&gt;foo &amp; bar&lt;/div&gt;');
    });

    test('prevents XSS with javascript: protocol', () => {
        expect(escapeHtml('javascript:alert(1)')).toBe('javascript:alert(1)');
    });

    test('escapes HTML entities', () => {
        expect(escapeHtml('&lt;already&gt;')).toBe('&amp;lt;already&amp;gt;');
    });
});

describe('formatRelativeTime', () => {
    test('formats seconds ago', () => {
        const now = Date.now();
        const fiveSecondsAgo = new Date(now - 5000).toISOString();
        expect(formatRelativeTime(fiveSecondsAgo)).toBe('5s ago');
    });

    test('formats minutes ago', () => {
        const now = Date.now();
        const fiveMinutesAgo = new Date(now - 5 * 60 * 1000).toISOString();
        expect(formatRelativeTime(fiveMinutesAgo)).toBe('5m ago');
    });

    test('formats hours ago', () => {
        const now = Date.now();
        const twoHoursAgo = new Date(now - 2 * 60 * 60 * 1000).toISOString();
        expect(formatRelativeTime(twoHoursAgo)).toBe('2h ago');
    });

    test('formats days ago', () => {
        const now = Date.now();
        const threeDaysAgo = new Date(now - 3 * 24 * 60 * 60 * 1000).toISOString();
        expect(formatRelativeTime(threeDaysAgo)).toBe('3d ago');
    });

    test('formats future time (seconds from now)', () => {
        const now = Date.now();
        const fiveSecondsFromNow = new Date(now + 5000).toISOString();
        expect(formatRelativeTime(fiveSecondsFromNow)).toBe('5s from now');
    });

    test('formats future time (minutes from now)', () => {
        const now = Date.now();
        const tenMinutesFromNow = new Date(now + 10 * 60 * 1000).toISOString();
        expect(formatRelativeTime(tenMinutesFromNow)).toBe('10m from now');
    });

    test('formats future time (hours from now)', () => {
        const now = Date.now();
        const threeHoursFromNow = new Date(now + 3 * 60 * 60 * 1000).toISOString();
        expect(formatRelativeTime(threeHoursFromNow)).toBe('3h from now');
    });

    test('prefers larger units (90 minutes -> 1h)', () => {
        const now = Date.now();
        const ninetyMinutesAgo = new Date(now - 90 * 60 * 1000).toISOString();
        expect(formatRelativeTime(ninetyMinutesAgo)).toBe('1h ago');
    });

    test('prefers larger units (25 hours -> 1d)', () => {
        const now = Date.now();
        const twentyFiveHoursAgo = new Date(now - 25 * 60 * 60 * 1000).toISOString();
        expect(formatRelativeTime(twentyFiveHoursAgo)).toBe('1d ago');
    });

    test('handles RFC3339 format', () => {
        const now = Date.now();
        const fiveMinutesAgo = new Date(now - 5 * 60 * 1000).toISOString();
        expect(formatRelativeTime(fiveMinutesAgo)).toBe('5m ago');
    });

    test('handles just now (0 seconds)', () => {
        const now = new Date().toISOString();
        const result = formatRelativeTime(now);
        // Should be 0s ago or "0s from now" depending on microsecond timing
        expect(result).toMatch(/^0s (ago|from now)$/);
    });
});

describe('formatValue', () => {
    test('formats null', () => {
        expect(formatValue(null)).toBe('<span class="config-value-null">null</span>');
    });

    test('formats undefined', () => {
        expect(formatValue(undefined)).toBe('<span class="config-value-null">null</span>');
    });

    test('formats boolean true', () => {
        expect(formatValue(true)).toBe('<span class="config-value-bool">true</span>');
    });

    test('formats boolean false', () => {
        expect(formatValue(false)).toBe('<span class="config-value-bool">false</span>');
    });

    test('formats number', () => {
        expect(formatValue(42)).toBe('<span class="config-value-number">42</span>');
    });

    test('formats zero', () => {
        expect(formatValue(0)).toBe('<span class="config-value-number">0</span>');
    });

    test('formats negative number', () => {
        expect(formatValue(-123)).toBe('<span class="config-value-number">-123</span>');
    });

    test('formats float', () => {
        expect(formatValue(3.14)).toBe('<span class="config-value-number">3.14</span>');
    });

    test('formats plain string', () => {
        expect(formatValue('hello')).toBe('<span class="config-value-string">hello</span>');
    });

    test('escapes HTML in string', () => {
        expect(formatValue('<script>alert("xss")</script>')).toBe(
            '<span class="config-value-string">&lt;script&gt;alert("xss")&lt;/script&gt;</span>'
        );
    });

    test('formats empty string', () => {
        expect(formatValue('')).toBe('<span class="config-value-string"></span>');
    });

    test('masks secrets when maskSecrets=true and string contains "key"', () => {
        const apiKey = 'my_api_key_abc123def456ghi789';
        expect(formatValue(apiKey, true)).toBe('<span class="config-value-secret">********</span>');
    });

    test('masks secrets with "token" in value', () => {
        const token = 'auth_token_abc123def456ghi789';
        expect(formatValue(token, true)).toBe('<span class="config-value-secret">********</span>');
    });

    test('masks secrets with "password" in value', () => {
        const password = 'mypassword123456789abc';
        expect(formatValue(password, true)).toBe('<span class="config-value-secret">********</span>');
    });

    test('masks short strings with "key" substring', () => {
        const shortKey = 'key123'; // Simple keyword detection, no length requirement
        expect(formatValue(shortKey, true)).toBe('<span class="config-value-secret">********</span>');
    });

    test('does not mask long strings without secret patterns', () => {
        const longString = 'this is a very long string with no problematic terms at all';
        expect(formatValue(longString, true)).toBe(
            `<span class="config-value-string">${longString}</span>`
        );
    });

    test('does not mask secrets when maskSecrets=false', () => {
        const apiKey = 'sk_test_abc123def456ghi789jkl012mno';
        expect(formatValue(apiKey, false)).toContain('sk_test_abc123def456ghi789jkl012mno');
        expect(formatValue(apiKey, false)).not.toContain('********');
    });

    test('masks secrets that contain "secret" substring', () => {
        const secret = 'my_secret_value_with_enough_chars_123';
        expect(formatValue(secret, true)).toBe('<span class="config-value-secret">********</span>');
    });

    test('masks secrets that contain "apikey" substring', () => {
        const apiKey = 'my_apikey_value_with_enough_chars_123';
        expect(formatValue(apiKey, true)).toBe('<span class="config-value-secret">********</span>');
    });

    test('handles objects by JSON stringifying', () => {
        const obj = { foo: 'bar' };
        expect(formatValue(obj)).toBe('<span class="config-value-object">{"foo":"bar"}</span>');
    });

    test('handles arrays by JSON stringifying', () => {
        const arr = [1, 2, 3];
        expect(formatValue(arr)).toBe('<span class="config-value-object">[1,2,3]</span>');
    });
});

describe('formatValue secret detection heuristics', () => {
    test('simple keyword detection without length/complexity requirements', () => {
        // Any string with "key" is masked
        const lettersOnly = 'my_secret_key_only_letters_here';
        expect(formatValue(lettersOnly, true)).toContain('********');

        // Any string with "key" is masked, even all numbers
        const numbersOnly = 'key1234567890123456789';
        expect(formatValue(numbersOnly, true)).toContain('********');

        // Mixed letters and numbers with "key" is also masked
        const mixed = 'my_secret_key_abc123def456';
        expect(formatValue(mixed, true)).toContain('********');
    });

    test('no minimum length requirement', () => {
        // Even short strings with keywords are masked
        const short = 'key123';
        expect(formatValue(short, true)).toContain('********');

        // Long strings with keywords are masked
        const long = 'my_very_long_secret_key_value_here';
        expect(formatValue(long, true)).toContain('********');
    });

    test('case-insensitive pattern matching', () => {
        const upperKey = 'MY_SECRET_KEY';
        const mixedKey = 'My_SeCrEt_KeY';
        const lowerKey = 'my_secret_key';

        expect(formatValue(upperKey, true)).toContain('********');
        expect(formatValue(mixedKey, true)).toContain('********');
        expect(formatValue(lowerKey, true)).toContain('********');
    });

    test('masks on partial keyword match', () => {
        // "key" appears as substring
        const hasKey = 'hockey'; // contains "key"
        expect(formatValue(hasKey, true)).toContain('********');

        // "token" appears as substring
        const hasToken = 'tokenizer'; // contains "token"
        expect(formatValue(hasToken, true)).toContain('********');
    });
});
