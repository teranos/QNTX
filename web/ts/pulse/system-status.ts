/**
 * System Status Section - Pulse's status
 *
 * Pulse starts because the node starts, so this section shows and does not set.
 * Not running here means the node has not said yet.
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
                  data-tooltip="Pulse runs inside the node\n${running ? 'Processing jobs and schedules' : 'The node has not said yet'}">
                ${running ? `${Pulse} Running` : `${Pulse} Waiting`}
            </span>
        </div>
    `;
}
