/**
 * Storage Eviction Handler
 *
 * Handles storage_eviction WebSocket messages when attestations are
 * deleted due to bounded storage limits. Feeds eviction data to the
 * database glyph for observability.
 */

import { log, SEG } from '../logger';
import type { StorageEvictionMessage } from '../../types/websocket';

/**
 * Handle storage eviction message
 */
export function handleStorageEviction(data: StorageEvictionMessage): void {
    log.debug(SEG.DB, 'Storage eviction:', data.message, 'Event type:', data.event_type);

    // Update database glyph with eviction data
    import('../default-glyphs.js').then(({ recordEviction }) => {
        recordEviction(data);
    });
}
