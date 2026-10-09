/**
 * System Status Section - Daemon status
 *
 * Pulse starts because the node starts, so this section shows and does not set.
 */

import { Pulse } from '../sym';
import type { DaemonStatusMessage } from '../../types/websocket';

/**
 * Render System Status section
 */
export function renderSystemStatus(data: DaemonStatusMessage | null): string {
    const running = data?.running ?? false;

    return `
        <div class="pulse-daemon-status">
            <span class="pulse-daemon-badge ${running ? 'running' : 'stopped'} has-tooltip"
                  data-tooltip="Pulse daemon status\n${running ? 'Processing scheduled jobs' : 'Not running - jobs will not execute'}">
                ${running ? `${Pulse} Running` : `${Pulse} Stopped`}
            </span>
        </div>
    `;
}
