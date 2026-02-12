/**
 * Storage Warning Handler
 *
 * Handles storage_warning WebSocket messages when bounded storage
 * limits are approaching.
 */

import { log, SEG } from '../logger';
import type { StorageWarningMessage } from '../../types/websocket';

/**
 * Handle storage warning message
 */
export function handleStorageWarning(data: StorageWarningMessage): void {
    const percentFull = Math.round(data.fill_percent * 100);
    const message = `Storage ${percentFull}% full for ${data.actor}/${data.context} (${data.current}/${data.limit})`;

    log.warn(SEG.DB, 'Storage warning:', message, 'Time until full:', data.time_until_full);

    // TODO: figure out how to make evictions observable without toast spam —
    // candidates: database status indicator flash, system drawer log entry, or dedicated eviction counter
}
