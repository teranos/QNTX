/**
 * Tests for session state management
 */

import { describe, test, expect, beforeEach, spyOn } from 'bun:test';
import { saveSession, restoreSession, clearSession } from './state-manager';

// Storage key must match state-manager.ts
const STORAGE_KEY = 'qntx-graph-session';
const MAX_AGE_MS = 7 * 24 * 60 * 60 * 1000; // 7 days

describe('State Manager', () => {
    beforeEach(() => {
        // Clear localStorage before each test
        localStorage.clear();
    });

    describe('saveSession', () => {
        test('saves session with timestamp to localStorage', () => {
            const now = Date.now();
            saveSession({ query: 'test query', verbosity: 2 });

            const stored = localStorage.getItem(STORAGE_KEY);
            expect(stored).not.toBeNull();

            const parsed = JSON.parse(stored!);
            expect(parsed.query).toBe('test query');
            expect(parsed.verbosity).toBe(2);
            expect(parsed.timestamp).toBeGreaterThanOrEqual(now);
        });

        test('saves empty session with only timestamp', () => {
            saveSession({});

            const stored = localStorage.getItem(STORAGE_KEY);
            expect(stored).not.toBeNull();

            const parsed = JSON.parse(stored!);
            expect(typeof parsed.timestamp).toBe('number');
        });

        test('overwrites existing session', () => {
            saveSession({ query: 'first' });
            saveSession({ query: 'second' });

            const stored = localStorage.getItem(STORAGE_KEY);
            const parsed = JSON.parse(stored!);
            expect(parsed.query).toBe('second');
        });

        test('handles save with various data types', () => {
            // Test with undefined values
            saveSession({ query: undefined, verbosity: undefined });
            const stored = localStorage.getItem(STORAGE_KEY);
            expect(stored).not.toBeNull();

            const parsed = JSON.parse(stored!);
            expect(typeof parsed.timestamp).toBe('number');
        });
    });

    describe('restoreSession', () => {
        test('returns null when no session exists', () => {
            const result = restoreSession();
            expect(result).toBeNull();
        });

        test('restores valid session', () => {
            const session = {
                query: 'test query',
                verbosity: 3,
                timestamp: Date.now()
            };
            localStorage.setItem(STORAGE_KEY, JSON.stringify(session));

            const result = restoreSession();
            expect(result).not.toBeNull();
            expect(result!.query).toBe('test query');
            expect(result!.verbosity).toBe(3);
        });

        test('returns null and clears expired session', () => {
            const expiredSession = {
                query: 'old query',
                timestamp: Date.now() - MAX_AGE_MS - 1000 // Expired
            };
            localStorage.setItem(STORAGE_KEY, JSON.stringify(expiredSession));

            const result = restoreSession();
            expect(result).toBeNull();
            expect(localStorage.getItem(STORAGE_KEY)).toBeNull();
        });

        test('returns session just within expiry window', () => {
            const validSession = {
                query: 'recent query',
                timestamp: Date.now() - MAX_AGE_MS + 60000 // 1 minute before expiry
            };
            localStorage.setItem(STORAGE_KEY, JSON.stringify(validSession));

            const result = restoreSession();
            expect(result).not.toBeNull();
            expect(result!.query).toBe('recent query');
        });

        test('returns null and clears invalid session (missing timestamp)', () => {
            const warnSpy = spyOn(console, 'warn').mockImplementation(() => {});

            localStorage.setItem(STORAGE_KEY, JSON.stringify({ query: 'no timestamp' }));

            const result = restoreSession();
            expect(result).toBeNull();
            expect(localStorage.getItem(STORAGE_KEY)).toBeNull();
            expect(warnSpy).toHaveBeenCalled();

            warnSpy.mockRestore();
        });

        test('returns null and clears invalid session (wrong timestamp type)', () => {
            const warnSpy = spyOn(console, 'warn').mockImplementation(() => {});

            localStorage.setItem(STORAGE_KEY, JSON.stringify({
                query: 'test',
                timestamp: 'not a number'
            }));

            const result = restoreSession();
            expect(result).toBeNull();
            expect(localStorage.getItem(STORAGE_KEY)).toBeNull();

            warnSpy.mockRestore();
        });

        test('returns null and clears invalid session (wrong query type)', () => {
            const warnSpy = spyOn(console, 'warn').mockImplementation(() => {});

            localStorage.setItem(STORAGE_KEY, JSON.stringify({
                query: 123, // Should be string
                timestamp: Date.now()
            }));

            const result = restoreSession();
            expect(result).toBeNull();

            warnSpy.mockRestore();
        });

        test('returns null and clears invalid session (wrong verbosity type)', () => {
            const warnSpy = spyOn(console, 'warn').mockImplementation(() => {});

            localStorage.setItem(STORAGE_KEY, JSON.stringify({
                verbosity: 'high', // Should be number
                timestamp: Date.now()
            }));

            const result = restoreSession();
            expect(result).toBeNull();

            warnSpy.mockRestore();
        });

        test('returns null for null stored value', () => {
            localStorage.setItem(STORAGE_KEY, JSON.stringify(null));

            const result = restoreSession();
            expect(result).toBeNull();
        });

        test('handles malformed JSON gracefully', () => {
            const errorSpy = spyOn(console, 'error').mockImplementation(() => {});

            localStorage.setItem(STORAGE_KEY, 'not valid json {{{');

            const result = restoreSession();
            expect(result).toBeNull();
            expect(errorSpy).toHaveBeenCalled();

            errorSpy.mockRestore();
        });

        test('returns null for empty string stored value', () => {
            localStorage.setItem(STORAGE_KEY, '');

            // Empty string causes JSON.parse to throw, which is caught
            const result = restoreSession();
            expect(result).toBeNull();
        });
    });

    describe('clearSession', () => {
        test('removes session from localStorage', () => {
            localStorage.setItem(STORAGE_KEY, JSON.stringify({ timestamp: Date.now() }));
            expect(localStorage.getItem(STORAGE_KEY)).not.toBeNull();

            clearSession();
            expect(localStorage.getItem(STORAGE_KEY)).toBeNull();
        });

        test('does nothing when no session exists', () => {
            expect(() => clearSession()).not.toThrow();
            expect(localStorage.getItem(STORAGE_KEY)).toBeNull();
        });

        test('can be called multiple times safely', () => {
            saveSession({ query: 'test' });
            clearSession();
            clearSession(); // Should not throw on second call
            clearSession(); // Or third

            expect(localStorage.getItem(STORAGE_KEY)).toBeNull();
        });
    });

    describe('round-trip', () => {
        test('save and restore preserves all fields', () => {
            const original = {
                query: 'complex query with special chars: @#$%',
                verbosity: 5
            };

            saveSession(original);
            const restored = restoreSession();

            expect(restored).not.toBeNull();
            expect(restored!.query).toBe(original.query);
            expect(restored!.verbosity).toBe(original.verbosity);
            expect(typeof restored!.timestamp).toBe('number');
        });

        test('save, clear, restore returns null', () => {
            saveSession({ query: 'test' });
            clearSession();
            const restored = restoreSession();

            expect(restored).toBeNull();
        });
    });
});
