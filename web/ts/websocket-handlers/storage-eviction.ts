/**
 * Storage Eviction Handler
 *
 * Handles storage_eviction WebSocket messages and displays toast notifications
 * when attestations are deleted due to bounded storage limits.
 */

import { showToast } from '../components/toast';
import type { StorageEvictionMessage } from '../../types/websocket';

/**
 * Handle storage eviction message - display as toast
 */
export function handleStorageEviction(data: StorageEvictionMessage): void {
    console.warn('⊔ Storage eviction:', data.message, 'Event type:', data.event_type);

    // Show toast notification with longer duration (8s) since it's important data loss info
    showToast(data.message, { type: 'warning', duration: 8000 });
}
