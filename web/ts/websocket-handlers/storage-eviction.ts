/**
 * Storage Eviction Handler
 *
 * Handles storage_eviction WebSocket messages when attestations are
 * deleted due to bounded storage limits.
 */

import { log, SEG } from '../logger';
import type { StorageEvictionMessage } from '../../types/websocket';

/**
 * Handle storage eviction message
 */
export function handleStorageEviction(data: StorageEvictionMessage): void {
    log.warn(SEG.DB, 'Storage eviction:', data.message, 'Event type:', data.event_type);

    // TODO: figure out how to make evictions observable without toast spam —
    // candidates: database status indicator flash, system drawer log entry, or dedicated eviction counter
}
