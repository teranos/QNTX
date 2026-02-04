/**
 * Minimal critical path tests for IndexedDB storage
 */

import { describe, test, expect } from 'bun:test';
import { initStorage, getStorageItem, setStorageItem, isStorageInitialized } from './indexeddb-storage';

const USE_JSDOM = process.env.USE_JSDOM === '1';

if (USE_JSDOM) {
    const { JSDOM } = await import('jsdom');
    const dom = new JSDOM('<!DOCTYPE html><html><body></body></html>');
    globalThis.document = dom.window.document as any;
    globalThis.window = dom.window as any;
}

describe('IndexedDB Storage', () => {
    if (!USE_JSDOM) {
        test.skip('Skipped locally (run with USE_JSDOM=1 to enable)', () => {});
        return;
    }

    test('blocks when IndexedDB unavailable', async () => {
        await expect(initStorage()).rejects.toThrow('IndexedDB not available');
    });

    test('returns null when storage not initialized', () => {
        expect(isStorageInitialized()).toBe(false);
        expect(getStorageItem('any-key')).toBeNull();
    });

    test('write and read operations are synchronous', () => {
        // When not initialized, operations should not throw (graceful degradation)
        expect(() => setStorageItem('key', 'value')).not.toThrow();
        expect(() => getStorageItem('key')).not.toThrow();
        // Returns null when not initialized
        expect(getStorageItem('key')).toBeNull();
    });
});
