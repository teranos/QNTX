/**
 * System Status Section - Daemon control
 */

/**
 * Render System Status section
 */
export function renderSystemStatus(daemonStatus: any): string {
    const running = daemonStatus?.running || false;

    return `
        <div class="pulse-daemon-status">
            <span class="pulse-daemon-badge ${running ? 'running' : 'stopped'}">
                ${running ? '꩜ Running' : '꩜ Stopped'}
            </span>
            <button class="pulse-btn pulse-btn-sm" data-action="${running ? 'stop' : 'start'}-daemon">
                ${running ? 'Stop' : 'Start'}
            </button>
        </div>
    `;
}

/**
 * Handle system status actions (start/stop daemon)
 */
export async function handleSystemStatusAction(action: string, value?: any): Promise<void> {
    const { sendMessage } = await import('../websocket.ts');

    switch (action) {
        case 'start-daemon':
        case 'stop-daemon':
            const daemonAction = action === 'start-daemon' ? 'start' : 'stop';
            sendMessage({
                type: 'daemon_control',
                action: daemonAction
            });
            break;
    }
}
