/**
 * Tests for storage utility layer
 */

import { describe, test, expect, beforeEach, spyOn } from 'bun:test';
import { getItem, setItem, removeItem, hasItem, getTimestamp, createStore } from './storage';

// Only run these tests when USE_JSDOM=1 (CI environment)
const USE_JSDOM = process.env.USE_JSDOM === '1';

// Setup jsdom if enabled
if (USE_JSDOM) {
    const { JSDOM } = await import('jsdom');
    const dom = new JSDOM('<!DOCTYPE html><html><body></body></html>', {
        url: 'http://localhost' // Required for localStorage
    });
    const { window } = dom;
    const { document } = window;

    // Replace global document/window with jsdom's
    globalThis.document = document as any;
    globalThis.window = window as any;
    globalThis.localStorage = window.localStorage as any;
}

describe('Storage', () => {
    if (!USE_JSDOM) {
        test.skip('Skipped locally (run with USE_JSDOM=1 to enable)', () => {});
        return;
    }

    beforeEach(() => {
        localStorage.clear();
    });

    describe('setItem / getItem', () => {
        test('stores and retrieves a value', () => {
            setItem('test-key', { name: 'test', count: 42 });
            const result = getItem<{ name: string; count: number }>('test-key');

            expect(result).not.toBeNull();
            expect(result!.name).toBe('test');
            expect(result!.count).toBe(42);
        });

        test('stores primitives', () => {
            setItem('string-key', 'hello');
            setItem('number-key', 123);
            setItem('boolean-key', true);

            expect(getItem('string-key')).toBe('hello');
            expect(getItem('number-key')).toBe(123);
            expect(getItem('boolean-key')).toBe(true);
        });

        test('returns null for non-existent key', () => {
            expect(getItem('nonexistent')).toBeNull();
        });

        test('includes timestamp in envelope', () => {
            const beforeTimestamp = Date.now();
            setItem('timestamped-key', { data: 'test' });
            const afterTimestamp = Date.now();

            const timestamp = getTimestamp('timestamped-key');
            expect(timestamp).not.toBeNull();
            expect(timestamp!).toBeGreaterThanOrEqual(beforeTimestamp);
            expect(timestamp!).toBeLessThanOrEqual(afterTimestamp);
        });

        test('stores version if provided', () => {
            setItem('versioned-key', { data: 'test' }, { version: 5 });
            const raw = localStorage.getItem('versioned-key');
            const envelope = JSON.parse(raw!);

            expect(envelope.version).toBe(5);
        });
    });

    describe('expiry (maxAge)', () => {
        test('returns null and removes expired value', () => {
            setItem('expired-key', { data: 'old' });

            // Manually set old timestamp
            const raw = localStorage.getItem('expired-key');
            const envelope = JSON.parse(raw!);
            envelope.timestamp = Date.now() - 10000; // 10 seconds ago
            localStorage.setItem('expired-key', JSON.stringify(envelope));

            // maxAge of 5 seconds should expire it
            const result = getItem('expired-key', { maxAge: 5000 });

            expect(result).toBeNull();
            expect(localStorage.getItem('expired-key')).toBeNull(); // Should be removed
        });

        test('returns value within maxAge', () => {
            setItem('fresh-key', { data: 'fresh' });

            const result = getItem('fresh-key', { maxAge: 60000 }); // 1 minute

            expect(result).not.toBeNull();
            expect(result!.data).toBe('fresh');
        });
    });

    describe('versioning', () => {
        test('returns null and removes mismatched version', () => {
            setItem('versioned-key', { data: 'v1' }, { version: 1 });

            // Try to retrieve with version 2
            const result = getItem('versioned-key', { version: 2 });

            expect(result).toBeNull();
            expect(localStorage.getItem('versioned-key')).toBeNull(); // Should be removed
        });

        test('returns value with matching version', () => {
            setItem('versioned-key', { data: 'v2' }, { version: 2 });

            const result = getItem('versioned-key', { version: 2 });

            expect(result).not.toBeNull();
            expect(result!.data).toBe('v2');
        });

        test('ignores version check if not specified', () => {
            setItem('versioned-key', { data: 'v3' }, { version: 3 });

            // Retrieve without version constraint
            const result = getItem('versioned-key');

            expect(result).not.toBeNull();
            expect(result!.data).toBe('v3');
        });
    });

    describe('validation', () => {
        test('returns null and removes value that fails validation', () => {
            setItem('invalid-key', { data: 'test' });

            const validator = (data: unknown): data is { data: string; required: boolean } => {
                return typeof data === 'object' && data !== null && 'required' in data;
            };

            const result = getItem('invalid-key', { validate: validator });

            expect(result).toBeNull();
            expect(localStorage.getItem('invalid-key')).toBeNull(); // Should be removed
        });

        test('returns value that passes validation', () => {
            setItem('valid-key', { data: 'test', required: true });

            const validator = (data: unknown): data is { data: string; required: boolean } => {
                return typeof data === 'object' && data !== null && 'required' in data;
            };

            const result = getItem('valid-key', { validate: validator });

            expect(result).not.toBeNull();
            expect(result!.data).toBe('test');
            expect(result!.required).toBe(true);
        });
    });

    describe('error handling', () => {
        test('handles invalid JSON gracefully', () => {
            localStorage.setItem('corrupt-key', 'not-json{');

            const result = getItem('corrupt-key');

            expect(result).toBeNull();
        });

        test('handles invalid envelope structure', () => {
            localStorage.setItem('bad-envelope', JSON.stringify({ wrong: 'structure' }));

            const result = getItem('bad-envelope');

            expect(result).toBeNull();
            expect(localStorage.getItem('bad-envelope')).toBeNull(); // Should be removed
        });

        test('handles localStorage errors silently', () => {
            const spy = spyOn(localStorage, 'getItem').mockImplementation(() => {
                throw new Error('Quota exceeded');
            });

            const result = getItem('any-key');

            expect(result).toBeNull();
            spy.mockRestore();
        });
    });

    describe('removeItem', () => {
        test('removes existing item', () => {
            setItem('remove-key', { data: 'test' });
            expect(localStorage.getItem('remove-key')).not.toBeNull();

            removeItem('remove-key');

            expect(localStorage.getItem('remove-key')).toBeNull();
        });

        test('handles removing non-existent item', () => {
            removeItem('nonexistent-key'); // Should not throw
        });

        test('handles localStorage errors silently', () => {
            const spy = spyOn(localStorage, 'removeItem').mockImplementation(() => {
                throw new Error('Quota exceeded');
            });

            removeItem('any-key'); // Should not throw
            spy.mockRestore();
        });
    });

    describe('hasItem', () => {
        test('returns true for existing valid item', () => {
            setItem('exists-key', { data: 'test' });

            expect(hasItem('exists-key')).toBe(true);
        });

        test('returns false for non-existent item', () => {
            expect(hasItem('nonexistent')).toBe(false);
        });

        test('returns false for expired item', () => {
            setItem('expired-key', { data: 'old' });

            // Manually set old timestamp
            const raw = localStorage.getItem('expired-key');
            const envelope = JSON.parse(raw!);
            envelope.timestamp = Date.now() - 10000; // 10 seconds ago
            localStorage.setItem('expired-key', JSON.stringify(envelope));

            expect(hasItem('expired-key', { maxAge: 5000 })).toBe(false);
        });

        test('returns false for version mismatch', () => {
            setItem('versioned-key', { data: 'test' }, { version: 1 });

            expect(hasItem('versioned-key', { version: 2 })).toBe(false);
        });
    });

    describe('getTimestamp', () => {
        test('returns timestamp for existing item', () => {
            const beforeTimestamp = Date.now();
            setItem('timestamped-key', { data: 'test' });
            const afterTimestamp = Date.now();

            const timestamp = getTimestamp('timestamped-key');

            expect(timestamp).not.toBeNull();
            expect(timestamp!).toBeGreaterThanOrEqual(beforeTimestamp);
            expect(timestamp!).toBeLessThanOrEqual(afterTimestamp);
        });

        test('returns null for non-existent item', () => {
            expect(getTimestamp('nonexistent')).toBeNull();
        });

        test('returns null for invalid envelope', () => {
            localStorage.setItem('bad-envelope', 'not-json');

            expect(getTimestamp('bad-envelope')).toBeNull();
        });
    });

    describe('createStore', () => {
        test('creates a typed store with all operations', () => {
            interface TestData {
                name: string;
                count: number;
            }

            const store = createStore<TestData>('store-key', { version: 1 });

            // set
            store.set({ name: 'test', count: 42 });
            expect(store.exists()).toBe(true);

            // get
            const data = store.get();
            expect(data).not.toBeNull();
            expect(data!.name).toBe('test');
            expect(data!.count).toBe(42);

            // getTimestamp
            const timestamp = store.getTimestamp();
            expect(timestamp).not.toBeNull();

            // remove
            store.remove();
            expect(store.exists()).toBe(false);
            expect(store.get()).toBeNull();
        });

        test('pre-configures maxAge', () => {
            const store = createStore('expiring-store', { maxAge: 5000 });

            store.set({ data: 'test' });

            // Manually expire it
            const raw = localStorage.getItem('expiring-store');
            const envelope = JSON.parse(raw!);
            envelope.timestamp = Date.now() - 10000; // 10 seconds ago
            localStorage.setItem('expiring-store', JSON.stringify(envelope));

            expect(store.get()).toBeNull();
            expect(store.exists()).toBe(false);
        });

        test('pre-configures version', () => {
            const store = createStore('versioned-store', { version: 3 });

            store.set({ data: 'v3' });

            // Manually change version
            const raw = localStorage.getItem('versioned-store');
            const envelope = JSON.parse(raw!);
            envelope.version = 2;
            localStorage.setItem('versioned-store', JSON.stringify(envelope));

            // Version mismatch should return null
            expect(store.get()).toBeNull();
        });

        test('pre-configures validation', () => {
            const validator = (data: unknown): data is { required: boolean } => {
                return typeof data === 'object' && data !== null && 'required' in data;
            };

            const store = createStore('validated-store', { validate: validator });

            // Set invalid data
            localStorage.setItem('validated-store', JSON.stringify({
                data: { wrong: 'structure' },
                timestamp: Date.now()
            }));

            expect(store.get()).toBeNull();
        });
    });
});
