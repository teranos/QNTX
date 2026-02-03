/**
 * IndexedDB Storage - Key-value storage for UI state
 *
 * Provides localStorage-compatible API backed by IndexedDB with in-memory cache.
 * Separate from attestation storage (qntx database) - uses "qntx-ui-state" database.
 *
 * Architecture:
 * - In-memory cache for synchronous reads
 * - Background IndexedDB writes for persistence
 * - Blocks entirely if IndexedDB unavailable (no fallback)
 */

import { log, SEG } from './logger';

const DB_NAME = 'qntx-ui-state';
const DB_VERSION = 1;
const STORE_NAME = 'state';

/** In-memory cache for synchronous access */
const cache = new Map<string, string>();

/** IndexedDB database instance */
let db: IDBDatabase | null = null;

/** Whether storage is initialized and available */
let isInitialized = false;

/** Whether IndexedDB is available in this browser */
let isAvailable = false;

/**
 * Initialize IndexedDB storage and load into memory cache.
 * Must be called before any storage operations.
 *
 * Throws if IndexedDB is unavailable (private browsing, old browsers).
 */
export async function initStorage(): Promise<void> {
    // Check if IndexedDB is available
    if (!window.indexedDB) {
        throw new Error('IndexedDB not available - canvas state persistence disabled');
    }

    return new Promise((resolve, reject) => {
        const request = indexedDB.open(DB_NAME, DB_VERSION);

        request.onerror = () => {
            log.error(SEG.UI, 'Failed to open IndexedDB', request.error);
            reject(new Error('Failed to initialize storage'));
        };

        request.onsuccess = () => {
            db = request.result;
            isAvailable = true;

            // Load all entries into memory cache
            loadCacheFromDB()
                .then(() => {
                    isInitialized = true;
                    log.info(SEG.UI, `[Storage] Initialized with ${cache.size} cached entries`);
                    resolve();
                })
                .catch(reject);
        };

        request.onupgradeneeded = (event) => {
            const database = (event.target as IDBOpenDBRequest).result;

            // Create object store for key-value pairs
            if (!database.objectStoreNames.contains(STORE_NAME)) {
                database.createObjectStore(STORE_NAME);
                log.debug(SEG.UI, `[Storage] Created object store: ${STORE_NAME}`);
            }
        };
    });
}

/**
 * Load all IndexedDB entries into memory cache
 */
async function loadCacheFromDB(): Promise<void> {
    if (!db) throw new Error('Storage not initialized');

    return new Promise((resolve, reject) => {
        const transaction = db!.transaction(STORE_NAME, 'readonly');
        const store = transaction.objectStore(STORE_NAME);
        const request = store.openCursor();

        request.onsuccess = () => {
            const cursor = request.result;
            if (cursor) {
                cache.set(cursor.key as string, cursor.value as string);
                cursor.continue();
            } else {
                // Cursor exhausted - all entries loaded
                resolve();
            }
        };

        request.onerror = () => {
            log.error(SEG.UI, 'Failed to load cache from IndexedDB', request.error);
            reject(new Error('Failed to load storage cache'));
        };
    });
}

/**
 * Get an item from storage (synchronous - reads from memory cache)
 * Returns null if storage not initialized yet.
 */
export function getStorageItem(key: string): string | null {
    if (!isInitialized) {
        log.warn(SEG.UI, 'Storage not initialized - returning null for getItem');
        return null;
    }
    return cache.get(key) ?? null;
}

/**
 * Set an item in storage (synchronous - writes to cache, queues IndexedDB write)
 * No-op if storage not initialized yet.
 */
export function setStorageItem(key: string, value: string): void {
    if (!isInitialized) {
        log.warn(SEG.UI, 'Storage not initialized - skipping setItem');
        return;
    }

    // Update cache immediately (synchronous)
    cache.set(key, value);

    // Queue IndexedDB write (asynchronous, non-blocking)
    writeToIndexedDB(key, value).catch((error) => {
        log.error(SEG.UI, `Failed to persist "${key}" to IndexedDB`, error);
        // Keep cache entry even if IndexedDB write fails
        // This ensures UI continues working, data persists next page load from cache
    });
}

/**
 * Remove an item from storage (synchronous - removes from cache, queues IndexedDB delete)
 * No-op if storage not initialized yet.
 */
export function removeStorageItem(key: string): void {
    if (!isInitialized) {
        log.warn(SEG.UI, 'Storage not initialized - skipping removeItem');
        return;
    }

    // Remove from cache immediately (synchronous)
    cache.delete(key);

    // Queue IndexedDB delete (asynchronous, non-blocking)
    deleteFromIndexedDB(key).catch((error) => {
        log.error(SEG.UI, `Failed to delete "${key}" from IndexedDB`, error);
    });
}

/**
 * Write to IndexedDB (async, queued)
 */
async function writeToIndexedDB(key: string, value: string): Promise<void> {
    if (!db) throw new Error('Storage not initialized');

    return new Promise((resolve, reject) => {
        const transaction = db!.transaction(STORE_NAME, 'readwrite');
        const store = transaction.objectStore(STORE_NAME);
        const request = store.put(value, key);

        request.onsuccess = () => resolve();
        request.onerror = () => reject(request.error);
    });
}

/**
 * Delete from IndexedDB (async, queued)
 */
async function deleteFromIndexedDB(key: string): Promise<void> {
    if (!db) throw new Error('Storage not initialized');

    return new Promise((resolve, reject) => {
        const transaction = db!.transaction(STORE_NAME, 'readwrite');
        const store = transaction.objectStore(STORE_NAME);
        const request = store.delete(key);

        request.onsuccess = () => resolve();
        request.onerror = () => reject(request.error);
    });
}

/**
 * Check if storage is initialized
 */
export function isStorageInitialized(): boolean {
    return isInitialized;
}

/**
 * Check if IndexedDB is available in this browser
 */
export function isStorageAvailable(): boolean {
    return isAvailable;
}
