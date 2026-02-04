/**
 * Tests for script storage layer (IndexedDB)
 */

import { describe, test, expect, beforeEach } from 'bun:test';
import { IndexedDBScriptStorage } from './script-storage';
import { initStorage, isStorageInitialized } from '../indexeddb-storage';

// Only run these tests when USE_JSDOM=1 (CI environment)
const USE_JSDOM = process.env.USE_JSDOM === '1';

// Setup fake-indexeddb if enabled
if (USE_JSDOM) {
    const fakeIndexedDB = await import('fake-indexeddb');
    const FDBKeyRange = (await import('fake-indexeddb/lib/FDBKeyRange')).default;

    // Create a minimal window object with IndexedDB
    const mockWindow = {
        indexedDB: fakeIndexedDB.default,
    };

    globalThis.window = mockWindow as any;
    globalThis.indexedDB = fakeIndexedDB.default as any;
    globalThis.IDBKeyRange = FDBKeyRange as any;
}

describe('IndexedDBScriptStorage', () => {
    if (!USE_JSDOM) {
        test.skip('Skipped locally (run with USE_JSDOM=1 to enable)', () => {});
        return;
    }

    let storage: IndexedDBScriptStorage;

    beforeEach(async () => {
        // Initialize storage once before tests
        if (!isStorageInitialized()) {
            await initStorage();
        }
        storage = new IndexedDBScriptStorage();

        // Clear all scripts before each test
        await storage.clear();
    });

    describe('save', () => {
        test('persists script to IndexedDB', async () => {
            await storage.save('script-123', 'print("hello")');

            const loaded = await storage.load('script-123');
            expect(loaded).not.toBeNull();
            expect(loaded).toBe('print("hello")');
        });

        test('overwrites existing script', async () => {
            await storage.save('script-123', 'first version');
            await storage.save('script-123', 'second version');

            const loaded = await storage.load('script-123');
            expect(loaded).toBe('second version');
        });
    });

    describe('load', () => {
        test('retrieves script from localStorage', async () => {
            await storage.save('script-456', 'test code');
            const loaded = await storage.load('script-456');
            expect(loaded).toBe('test code');
        });

        test('returns null for non-existent script', async () => {
            const loaded = await storage.load('nonexistent');
            expect(loaded).toBeNull();
        });

        test('handles empty string scripts', async () => {
            await storage.save('empty-script', '');
            const loaded = await storage.load('empty-script');
            expect(loaded).toBe('');
        });
    });

    describe('list', () => {
        test('returns all script metadata', async () => {
            await storage.save('script-1', 'code1');
            await storage.save('script-2', 'code2');
            await storage.save('script-3', 'code3');

            const metadata = await storage.list();
            const ids = metadata.map(m => m.id).sort();
            expect(ids).toEqual(['script-1', 'script-2', 'script-3']);
        });

        test('returns empty array when no scripts exist', async () => {
            const metadata = await storage.list();
            expect(metadata).toEqual([]);
        });
    });

    describe('clear', () => {
        test('removes all scripts', async () => {
            await storage.save('script-1', 'code1');
            await storage.save('script-2', 'code2');

            await storage.clear();

            const metadata = await storage.list();
            expect(metadata).toEqual([]);
        });
    });

    describe('concurrent operations', () => {
        test('concurrent saves do not corrupt data', async () => {
            const promises = [
                storage.save('script-1', 'code1'),
                storage.save('script-2', 'code2'),
                storage.save('script-3', 'code3'),
            ];

            await Promise.all(promises);

            const [code1, code2, code3] = await Promise.all([
                storage.load('script-1'),
                storage.load('script-2'),
                storage.load('script-3'),
            ]);

            expect(code1).toBe('code1');
            expect(code2).toBe('code2');
            expect(code3).toBe('code3');
        });
    });
});
