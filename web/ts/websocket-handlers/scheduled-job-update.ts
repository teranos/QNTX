/**
 * Scheduled Job Update Handler
 *
 * Handles scheduled_job_update WebSocket messages and updates button state
 * when jobs are paused, resumed, or deleted.
 */

import { log, SEG } from '../logger';
import type { ScheduledJobUpdateMessage } from '../../types/websocket';
import { getButton } from '../components/button';

/**
 * Handle scheduled job update message - update button state based on action
 */
export function handleScheduledJobUpdate(data: ScheduledJobUpdateMessage): void {
    log.debug(SEG.PULSE, 'Scheduled job update:', data.job_id, data.action, data.state);

    // Update the relevant button based on action
    switch (data.action) {
        case 'paused': {
            // Pause completed - clear loading on pause/toggle button
            const toggleBtn = getButton(`toggle-state-${data.job_id}`);
            if (toggleBtn) {
                toggleBtn.setLoading(false);
            }
            break;
        }
        case 'resumed': {
            // Resume completed - clear loading on toggle button
            const toggleBtn = getButton(`toggle-state-${data.job_id}`);
            if (toggleBtn) {
                toggleBtn.setLoading(false);
            }
            break;
        }
        case 'deleted': {
            // Delete completed - clear loading on delete button
            // Note: Button may already be removed from DOM after panel re-render
            const deleteBtn = getButton(`delete-${data.job_id}`);
            if (deleteBtn) {
                deleteBtn.setLoading(false);
            }
            break;
        }
    }
}
