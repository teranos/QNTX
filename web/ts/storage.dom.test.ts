/**
 * Tests for storage utility layer
 */

import { describe, test, expect, beforeEach, spyOn } from 'bun:test';
import { getItem, setItem, removeItem, hasItem, getTimestamp, createStore } from './state/storage';

describe('Storage', () => {
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
            setItem('null-key', null);

            expect(getItem('string-key')).toBe('hello');
            expect(getItem('number-key')).toBe(123);
            expect(getItem('boolean-key')).toBe(true);
            expect(getItem('null-key')).toBeNull(); // null data is valid
        });

        test('stores arrays', () => {
            setItem('array-key', [1, 2, 3, 'four']);
            const result = getItem<(number | string)[]>('array-key');

            expect(result).toEqual([1, 2, 3, 'four']);
        });

        test('returns null for non-existent key', () => {
            expect(getItem('missing-key')).toBeNull();
        });

        test('overwrites existing value', () => {
            setItem('key', 'first');
            setItem('key', 'second');

            expect(getItem('key')).toBe('second');
        });

        test('includes timestamp in envelope', () => {
            const before = Date.now();
            setItem('key', 'value');
            const after = Date.now();

            const timestamp = getTimestamp('key');
            expect(timestamp).not.toBeNull();
            expect(timestamp).toBeGreaterThanOrEqual(before);
            expect(timestamp).toBeLessThanOrEqual(after);
        });
    });

    describe('expiry (maxAge)', () => {
        test('returns value within maxAge', () => {
            setItem('key', 'value');

            const result = getItem('key', { maxAge: 60000 }); // 1 minute
            expect(result).toBe('value');
        });

        test('returns null and removes expired value', () => {
            // Manually create an old envelope
            const oldEnvelope = {
                data: 'old value',
                timestamp: Date.now() - 10000, // 10 seconds ago
            };
            localStorage.setItem('old-key', JSON.stringify(oldEnvelope));

            const result = getItem('old-key', { maxAge: 5000 }); // 5 second max
            expect(result).toBeNull();
            expect(localStorage.getItem('old-key')).toBeNull(); // Should be removed
        });

        test('respects maxAge of 0 (immediate expiry)', () => {
            // Create envelope with timestamp 1ms ago
            const envelope = {
                data: 'value',
                timestamp: Date.now() - 1,
            };
            localStorage.setItem('key', JSON.stringify(envelope));

            expect(getItem('key', { maxAge: 0 })).toBeNull();
        });
    });

    describe('versioning', () => {
        test('returns value with matching version', () => {
            setItem('key', 'value', { version: 2 });

            const result = getItem('key', { version: 2 });
            expect(result).toBe('value');
        });

        test('returns null and removes mismatched version', () => {
            const warnSpy = spyOn(console, 'warn').mockImplementation(() => {});

            setItem('key', 'value', { version: 1 });

            const result = getItem('key', { version: 2 });
            expect(result).toBeNull();
            expect(localStorage.getItem('key')).toBeNull();
            expect(warnSpy).toHaveBeenCalled();

            warnSpy.mockRestore();
        });

        test('returns value when no version check requested', () => {
            setItem('key', 'value', { version: 5 });

            const result = getItem('key'); // No version check
            expect(result).toBe('value');
        });
    });

    describe('validation', () => {
        interface User {
            name: string;
            age: number;
        }

        const isUser = (data: unknown): data is User => {
            if (!data || typeof data !== 'object') return false;
            const obj = data as Record<string, unknown>;
            return typeof obj.name === 'string' && typeof obj.age === 'number';
        };

        test('returns value that passes validation', () => {
            setItem('user', { name: 'Alice', age: 30 });

            const result = getItem<User>('user', { validate: isUser });
            expect(result).toEqual({ name: 'Alice', age: 30 });
        });

        test('returns null and removes value that fails validation', () => {
            const warnSpy = spyOn(console, 'warn').mockImplementation(() => {});

            setItem('user', { name: 'Bob' }); // Missing age

            const result = getItem<User>('user', { validate: isUser });
            expect(result).toBeNull();
            expect(localStorage.getItem('user')).toBeNull();

            warnSpy.mockRestore();
        });
    });

    describe('removeItem', () => {
        test('removes existing item', () => {
            setItem('key', 'value');
            expect(getItem('key')).toBe('value');

            removeItem('key');
            expect(getItem('key')).toBeNull();
        });

        test('does nothing for non-existent key', () => {
            expect(() => removeItem('missing')).not.toThrow();
        });
    });

    describe('hasItem', () => {
        test('returns true for existing valid item', () => {
            setItem('key', 'value');
            expect(hasItem('key')).toBe(true);
        });

        test('returns false for non-existent item', () => {
            expect(hasItem('missing')).toBe(false);
        });

        test('returns false for expired item', () => {
            const oldEnvelope = {
                data: 'value',
                timestamp: Date.now() - 10000,
            };
            localStorage.setItem('old', JSON.stringify(oldEnvelope));

            expect(hasItem('old', { maxAge: 5000 })).toBe(false);
        });
    });

    describe('getTimestamp', () => {
        test('returns timestamp for existing item', () => {
            const before = Date.now();
            setItem('key', 'value');

            const timestamp = getTimestamp('key');
            expect(timestamp).not.toBeNull();
            expect(timestamp).toBeGreaterThanOrEqual(before);
        });

        test('returns null for non-existent item', () => {
            expect(getTimestamp('missing')).toBeNull();
        });

        test('returns null for malformed data', () => {
            localStorage.setItem('bad', 'not json');
            expect(getTimestamp('bad')).toBeNull();
        });
    });

    describe('createStore', () => {
        test('creates a typed store with all operations', () => {
            interface Config {
                theme: string;
                fontSize: number;
            }

            const configStore = createStore<Config>('app-config');

            // Initially empty
            expect(configStore.get()).toBeNull();
            expect(configStore.exists()).toBe(false);

            // Set value
            configStore.set({ theme: 'dark', fontSize: 14 });
            expect(configStore.exists()).toBe(true);

            // Get value
            const config = configStore.get();
            expect(config).toEqual({ theme: 'dark', fontSize: 14 });

            // Has timestamp
            expect(configStore.getTimestamp()).not.toBeNull();

            // Remove
            configStore.remove();
            expect(configStore.get()).toBeNull();
        });

        test('store respects configured options', () => {
            const store = createStore<string>('test', {
                maxAge: 5000,
                version: 1,
            });

            store.set('value');
            expect(store.get()).toBe('value');

            // Manually expire it
            const oldEnvelope = {
                data: 'expired',
                timestamp: Date.now() - 10000,
                version: 1,
            };
            localStorage.setItem('test', JSON.stringify(oldEnvelope));
            expect(store.get()).toBeNull();
        });
    });

    describe('error handling', () => {
        test('handles malformed JSON gracefully', () => {
            const errorSpy = spyOn(console, 'error').mockImplementation(() => {});

            localStorage.setItem('bad', '{not valid json}}}');

            expect(getItem('bad')).toBeNull();
            expect(errorSpy).toHaveBeenCalled();

            errorSpy.mockRestore();
        });

        test('handles invalid envelope structure', () => {
            const warnSpy = spyOn(console, 'warn').mockImplementation(() => {});

            // Missing timestamp
            localStorage.setItem('bad', JSON.stringify({ data: 'value' }));
            expect(getItem('bad')).toBeNull();

            // Missing data
            localStorage.setItem('bad2', JSON.stringify({ timestamp: Date.now() }));
            expect(getItem('bad2')).toBeNull();

            warnSpy.mockRestore();
        });
    });
});
