/**
 * WebSocket handler for daemon_status messages
 * Routes daemon status updates to all components that need them
 */

import type { DaemonStatusMessage } from '../../types/websocket';

/**
 * Handle daemon_status WebSocket messages
 * Called by main.ts when daemon status updates arrive
 */
export async function handleDaemonStatus(data: DaemonStatusMessage): Promise<void> {
    // Update pulse panel if it exists
    const { updatePulsePanelDaemonStatus } = await import('../pulse-panel.ts');
    updatePulsePanelDaemonStatus(data);

    // Future: Add other components that need daemon status
    // updateHeader(data);
    // updateCostTracker(data);
}
