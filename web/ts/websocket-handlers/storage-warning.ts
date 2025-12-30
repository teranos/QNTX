/**
 * Storage Warning Handler
 *
 * Handles storage_warning WebSocket messages and displays toast notifications
 * when bounded storage limits are approaching.
 */

import { toast } from '../toast';
import type { StorageWarningMessage } from '../../types/websocket';

/**
 * Handle storage warning message - display as toast
 */
export function handleStorageWarning(data: StorageWarningMessage): void {
    const percentFull = Math.round(data.fill_percent * 100);

    // Format the warning message
    const message = `Storage ${percentFull}% full for ${data.actor}/${data.context} (${data.current}/${data.limit})`;

    console.warn('⊔ Storage warning:', message, 'Time until full:', data.time_until_full);

    // Show toast - use warning for 50-75%, more urgent styling could be added for >75%
    toast.warning(message);
}
