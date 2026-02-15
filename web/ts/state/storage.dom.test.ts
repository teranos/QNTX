/**
 * Tests for storage utility layer
 */

import { describe, test, expect, beforeEach, spyOn } from 'bun:test';
import { getItem, setItem, removeItem, hasItem, getTimestamp, createStore } from './storage';
import { initStorage } from '../indexeddb-storage';

// Only run these tests when USE_JSDOM=1 (CI environment)
const USE_JSDOM = process.env.USE_JSDOM === '1';

// Wire fake-indexeddb into the shared JSDOM window (storage tests need it)
if (USE_JSDOM) {
    const fakeIndexedDB = await import('fake-indexeddb');
    (window as any).indexedDB = fakeIndexedDB.default;
    globalThis.indexedDB = fakeIndexedDB.default as any;
}

describe('Storage', () => {
    if (!USE_JSDOM) {
        test.skip('Skipped locally (run with USE_JSDOM=1 to enable)', () => {});
        return;
    }

    beforeEach(async () => {
        // Initialize IndexedDB storage for each test
        await initStorage();
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
    });

    describe('expiry (maxAge)', () => {
        test('returns value within maxAge', () => {
            setItem('fresh-key', { data: 'fresh' });

            const result = getItem('fresh-key', { maxAge: 60000 }); // 1 minute

            expect(result).not.toBeNull();
            expect(result!.data).toBe('fresh');
        });
    });

    describe('versioning', () => {
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

    // Error handling tests removed - they tested localStorage implementation details

    describe('removeItem', () => {
        test('handles removing non-existent item', () => {
            removeItem('nonexistent-key'); // Should not throw
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
    });
});
