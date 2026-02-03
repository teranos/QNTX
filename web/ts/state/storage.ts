/**
 * Storage - IndexedDB utility layer
 *
 * Provides consistent error handling, expiry, versioning, and validation
 * for all storage operations across the application.
 *
 * Backend: IndexedDB (via indexeddb-storage.ts in-memory cache)
 * Replaces: localStorage (migrated for multi-device sync foundation)
 *
 * Used by:
 * - state/ui.ts (high-level state management)
 * - Any module needing persistent storage with robust handling
 */

import { handleErrorSilent } from '../error-handler.ts';
import { log, SEG } from '../logger.ts';
import { getStorageItem, setStorageItem, removeStorageItem } from '../indexeddb-storage.ts';

// ============================================================================
// Types
// ============================================================================

/**
 * Options for storage operations
 */
export interface StorageOptions<T> {
    /** Auto-expire after this many milliseconds */
    maxAge?: number;
    /** Version number for migration support */
    version?: number;
    /** Custom validation function */
    validate?: (data: unknown) => data is T;
}

/**
 * Internal wrapper for stored data
 */
interface StorageEnvelope<T> {
    data: T;
    timestamp: number;
    version?: number;
}

// ============================================================================
// Core Functions
// ============================================================================

/**
 * Get an item from localStorage with error handling, expiry, and validation
 *
 * @param key - Storage key
 * @param options - Optional expiry, version, and validation settings
 * @returns The stored value or null if not found/invalid/expired
 */
export function getItem<T>(key: string, options?: StorageOptions<T>): T | null {
    try {
        const raw = getStorageItem(key);
        if (!raw) return null;

        const envelope = JSON.parse(raw) as StorageEnvelope<T>;

        // Validate envelope structure
        if (!isValidEnvelope(envelope)) {
            log.warn(SEG.UI, `Invalid envelope for key "${key}", removing`);
            removeItem(key);
            return null;
        }

        // Check version mismatch
        if (options?.version !== undefined && envelope.version !== options.version) {
            log.warn(SEG.UI, `Version mismatch for key "${key}" (stored: ${envelope.version}, expected: ${options.version}), removing`);
            removeItem(key);
            return null;
        }

        // Check expiry
        if (options?.maxAge !== undefined) {
            const age = Date.now() - envelope.timestamp;
            if (age > options.maxAge) {
                log.debug(SEG.UI, `Key "${key}" expired (age: ${age}ms, maxAge: ${options.maxAge}ms)`);
                removeItem(key);
                return null;
            }
        }

        // Custom validation
        if (options?.validate && !options.validate(envelope.data)) {
            log.warn(SEG.UI, `Validation failed for key "${key}", removing`);
            removeItem(key);
            return null;
        }

        return envelope.data;
    } catch (error: unknown) {
        handleErrorSilent(error, `Failed to get storage key "${key}"`, SEG.UI);
        return null;
    }
}

/**
 * Set an item in localStorage with automatic timestamping
 *
 * @param key - Storage key
 * @param value - Value to store
 * @param options - Optional version to include
 */
export function setItem<T>(key: string, value: T, options?: Pick<StorageOptions<T>, 'version'>): void {
    try {
        const envelope: StorageEnvelope<T> = {
            data: value,
            timestamp: Date.now(),
            version: options?.version,
        };
        setStorageItem(key, JSON.stringify(envelope));
    } catch (error: unknown) {
        handleErrorSilent(error, `Failed to set storage key "${key}"`, SEG.UI);
    }
}

/**
 * Remove an item from localStorage
 *
 * @param key - Storage key to remove
 */
export function removeItem(key: string): void {
    try {
        removeStorageItem(key);
    } catch (error: unknown) {
        handleErrorSilent(error, `Failed to remove storage key "${key}"`, SEG.UI);
    }
}

/**
 * Check if an item exists and is valid (not expired)
 *
 * @param key - Storage key
 * @param options - Optional expiry and version settings
 * @returns true if item exists and is valid
 */
export function hasItem(key: string, options?: Pick<StorageOptions<unknown>, 'maxAge' | 'version'>): boolean {
    return getItem(key, options) !== null;
}

/**
 * Get the timestamp of when an item was stored
 *
 * @param key - Storage key
 * @returns Timestamp in milliseconds, or null if not found
 */
export function getTimestamp(key: string): number | null {
    try {
        const raw = getStorageItem(key);
        if (!raw) return null;

        const envelope = JSON.parse(raw) as StorageEnvelope<unknown>;
        return envelope.timestamp ?? null;
    } catch {
        return null;
    }
}

// ============================================================================
// Store Factory
// ============================================================================

/**
 * Create a typed store for a specific key with pre-configured options
 *
 * @example
 * const sessionStore = createStore<SessionData>('qntx-session', {
 *     maxAge: 7 * 24 * 60 * 60 * 1000, // 7 days
 *     version: 1,
 * });
 *
 * sessionStore.set({ query: 'test' });
 * const session = sessionStore.get();
 */
export function createStore<T>(key: string, options?: StorageOptions<T>) {
    return {
        get: (): T | null => getItem<T>(key, options),
        set: (value: T): void => setItem(key, value, options),
        remove: (): void => removeItem(key),
        exists: (): boolean => hasItem(key, options),
        getTimestamp: (): number | null => getTimestamp(key),
    };
}

// ============================================================================
// Internal Helpers
// ============================================================================

/**
 * Validate that parsed data is a valid storage envelope
 */
function isValidEnvelope(data: unknown): data is StorageEnvelope<unknown> {
    if (!data || typeof data !== 'object') return false;
    const obj = data as Record<string, unknown>;
    return typeof obj.timestamp === 'number' && 'data' in obj;
}
