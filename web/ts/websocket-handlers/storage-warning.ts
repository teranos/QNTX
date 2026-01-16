/**
 * Storage Warning Handler
 *
 * Handles storage_warning WebSocket messages and updates the bounded storage window.
 * Storage warnings are NOT shown as toasts - they're tracked in the dedicated
 * bounded storage window for persistent monitoring.
 */

import { boundedStorageWindow } from '../bounded-storage-window';
import { log, SEG } from '../logger';
import type { StorageWarningMessage } from '../../types/websocket';

/**
 * Handle storage warning message - update bounded storage window
 */
export function handleStorageWarning(data: StorageWarningMessage): void {
    const percentFull = Math.round(data.fill_percent * 100);

    log.warn(SEG.DB, `Storage ${percentFull}% full for ${data.actor}/${data.context} (${data.current}/${data.limit})`);

    // Update bounded storage window (updates indicator and window if visible)
    boundedStorageWindow.handleWarning(data);
}
