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
    // Update pulse panel via custom event (panel listens when open)
    document.dispatchEvent(new CustomEvent('pulse-daemon-status', { detail: data }));

    // Update Pulse daemon status indicator
    const { statusIndicators } = await import('../status-indicators.ts');
    statusIndicators.handlePulseDaemonStatus(data);
}
