/**
 * System Status Section - Daemon control
 *
 * Uses two-click confirmation pattern for daemon start/stop actions.
 */

import { Pulse } from '@generated/sym.js';
import { log, SEG } from '../logger';

/**
 * Two-click confirmation state for daemon actions
 */
interface DaemonConfirmState {
    needsConfirmation: boolean;
    timeout: number | null;
}

let daemonConfirmState: DaemonConfirmState | null = null;

/**
 * Reset daemon confirmation state
 */
function resetDaemonConfirmation(): void {
    if (daemonConfirmState?.timeout) {
        clearTimeout(daemonConfirmState.timeout);
    }
    daemonConfirmState = null;
}

/**
 * Check if daemon action needs confirmation
 */
export function isDaemonConfirmationPending(): boolean {
    return daemonConfirmState?.needsConfirmation ?? false;
}

/**
 * Render System Status section
 */
export function renderSystemStatus(daemonStatus: any): string {
    const running = daemonStatus?.running || false;
    const action = running ? 'stop' : 'start';
    const isConfirming = daemonConfirmState?.needsConfirmation ?? false;

    const buttonText = isConfirming
        ? `Confirm ${running ? 'Stop' : 'Start'}`
        : (running ? 'Stop' : 'Start');

    const confirmingClass = isConfirming ? 'pulse-btn-confirming' : '';

    return `
        <div class="pulse-daemon-status">
            <span class="pulse-daemon-badge ${running ? 'running' : 'stopped'} has-tooltip"
                  data-tooltip="Pulse daemon status\n${running ? 'Processing scheduled jobs' : 'Not running - jobs will not execute'}">
                ${running ? `${Pulse} Running` : `${Pulse} Stopped`}
            </span>
            <button class="pulse-btn pulse-btn-sm pulse-btn-daemon-${action} ${confirmingClass} has-tooltip"
                    data-action="${action}-daemon"
                    data-tooltip="${running ? 'Stop the Pulse daemon\nScheduled jobs will not execute while stopped' : 'Start the Pulse daemon\nBegin processing scheduled jobs'}">
                ${buttonText}
            </button>
        </div>
    `;
}

/**
 * Handle system status actions (start/stop daemon, edit budget)
 * Uses two-click confirmation pattern for daemon control
 *
 * @returns true if action was executed, false if waiting for confirmation
 */
export async function handleSystemStatusAction(action: string): Promise<boolean> {
    const { sendMessage } = await import('../websocket.ts');

    switch (action) {
        case 'start-daemon':
        case 'stop-daemon':
            // Check if we're in confirmation state
            if (!daemonConfirmState?.needsConfirmation) {
                // First click: enter confirmation state
                daemonConfirmState = {
                    needsConfirmation: true,
                    timeout: window.setTimeout(() => {
                        resetDaemonConfirmation();
                        // Re-render to update button text
                        const container = document.getElementById('pulse-system-status-content');
                        if (container) {
                            // Trigger a re-render by dispatching a custom event
                            container.dispatchEvent(new CustomEvent('daemon-confirm-reset'));
                        }
                    }, 5000)
                };
                return false; // Signal that we need to re-render
            }

            // Second click: execute action
            resetDaemonConfirmation();
            const daemonAction = action === 'start-daemon' ? 'start' : 'stop';
            sendMessage({
                type: 'daemon_control',
                action: daemonAction
            });
            return true;

        case 'edit-budget':
            openBudgetConfigPanel();
            return true;

        default:
            return true;
    }
}

/**
 * Open the budget configuration panel and populate current values
 */
function openBudgetConfigPanel(): void {
    const overlay = document.getElementById('pulse-config-overlay');
    const form = document.getElementById('pulse-config-form') as HTMLFormElement;
    const dailyInput = document.getElementById('daily-budget') as HTMLInputElement;
    const weeklyInput = document.getElementById('weekly-budget') as HTMLInputElement;
    const monthlyInput = document.getElementById('monthly-budget') as HTMLInputElement;
    const closeBtn = document.getElementById('pulse-config-close');

    if (!overlay || !form || !dailyInput || !weeklyInput || !monthlyInput) {
        log.error(SEG.PULSE, 'Budget config panel elements not found');
        return;
    }

    // Fetch current config from server
    fetch('/api/pulse/config')
        .then(res => res.json())
        .then(config => {
            dailyInput.value = (config.daily_budget_usd ?? 1.0).toString();
            weeklyInput.value = (config.weekly_budget_usd ?? 7.0).toString();
            monthlyInput.value = (config.monthly_budget_usd ?? 30.0).toString();
        })
        .catch((error: unknown) => {
            log.error(SEG.PULSE, 'Failed to fetch pulse config:', error);
            // Use defaults
            dailyInput.value = '1.0';
            weeklyInput.value = '7.0';
            monthlyInput.value = '30.0';
        });

    // Show overlay
    overlay.classList.remove('u-hidden');
    overlay.classList.add('u-flex');

    // Handle close
    const closeHandler = () => {
        overlay.classList.remove('u-flex');
        overlay.classList.add('u-hidden');
    };

    closeBtn?.addEventListener('click', closeHandler, { once: true });
    overlay.addEventListener('click', (e) => {
        if (e.target === overlay) closeHandler();
    }, { once: true });

    // Handle form submit
    form.onsubmit = async (e) => {
        e.preventDefault();
        const { sendMessage } = await import('../websocket.ts');

        sendMessage({
            type: 'pulse_config_update',
            daily_budget: parseFloat(dailyInput.value) || 1.0,
            weekly_budget: parseFloat(weeklyInput.value) || 7.0,
            monthly_budget: parseFloat(monthlyInput.value) || 30.0
        });

        overlay.classList.remove('u-flex');
        overlay.classList.add('u-hidden');
    };
}
