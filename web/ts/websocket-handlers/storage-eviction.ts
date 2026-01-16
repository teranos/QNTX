/**
 * Storage Eviction Handler
 *
 * Handles storage_eviction WebSocket messages and updates the bounded storage window.
 * Evictions are NOT shown as toasts - they're tracked in the dedicated
 * bounded storage window for persistent monitoring and history.
 */

import { boundedStorageWindow } from '../bounded-storage-window';
import { log, SEG } from '../logger';
import type { StorageEvictionMessage } from '../../types/websocket';

/**
 * Handle storage eviction message - update bounded storage window
 */
export function handleStorageEviction(data: StorageEvictionMessage): void {
    log.warn(SEG.DB, `Storage eviction: ${data.message} (${data.event_type})`);

    // Update bounded storage window (adds to history, updates indicator)
    boundedStorageWindow.handleEviction(data);
}
