/**
 * System Status Section - Daemon control + budget bars
 *
 * Uses two-click confirmation pattern for daemon start/stop actions.
 * Budget bars show stacked local (solid) + peer (translucent) spend against
 * limits. They show and do not set: a budget is am.toml's to name.
 */

import { Pulse } from '@generated/sym.js';
import type { DaemonStatusMessage } from '../../types/websocket';

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
 * Render a single budget bar with stacked local + peer segments.
 * When cluster limit is tighter than node limit, shows cluster limit marker.
 */
function renderBudgetBar(label: string, local: number, aggregate: number, nodeLimit: number, clusterLimit: number): string {
    if (nodeLimit <= 0 && clusterLimit <= 0) return '';

    // The effective limit shown is whichever is configured; prefer node limit as the bar max
    const barMax = nodeLimit > 0 ? nodeLimit : clusterLimit;
    const peerSpend = aggregate - local;
    const localPct = Math.min((local / barMax) * 100, 100);
    const peerPct = Math.min((peerSpend / barMax) * 100, 100 - localPct);
    const totalPct = localPct + peerPct;

    // Color: green < 60%, amber 60-80%, red > 80%
    const color = totalPct > 80 ? '#f87171' : totalPct > 60 ? '#fbbf24' : '#4ade80';
    const peerColor = totalPct > 80 ? 'rgba(248,113,113,0.4)' : totalPct > 60 ? 'rgba(251,191,36,0.4)' : 'rgba(74,222,128,0.4)';

    // Cluster limit marker (vertical line) when cluster < node
    let clusterMarker = '';
    if (clusterLimit > 0 && nodeLimit > 0 && clusterLimit < nodeLimit) {
        const clusterPct = (clusterLimit / nodeLimit) * 100;
        clusterMarker = `<div style="position:absolute;left:${clusterPct}%;top:0;bottom:0;width:2px;background:#60a5fa;z-index:2;" title="Cluster limit $${clusterLimit.toFixed(2)}"></div>`;
    }

    const spendLabel = peerSpend > 0
        ? `$${local.toFixed(2)} + $${peerSpend.toFixed(2)} peers`
        : `$${local.toFixed(2)}`;
    const limitLabel = clusterLimit > 0 && (nodeLimit <= 0 || clusterLimit < nodeLimit)
        ? `$${(clusterLimit).toFixed(2)} cluster`
        : `$${barMax.toFixed(2)}`;

    return `
        <div class="budget-bar-row" style="margin-bottom:8px;">
            <div style="display:flex;justify-content:space-between;font-size:11px;margin-bottom:2px;color:#9ca3af;">
                <span>${label}: ${spendLabel}</span>
                <span>${limitLabel}</span>
            </div>
            <div style="position:relative;height:6px;background:#1e293b;border-radius:3px;overflow:hidden;">
                <div style="position:absolute;left:0;top:0;bottom:0;width:${localPct}%;background:${color};border-radius:3px 0 0 3px;z-index:1;"></div>
                <div style="position:absolute;left:${localPct}%;top:0;bottom:0;width:${peerPct}%;background:${peerColor};z-index:1;"></div>
                ${clusterMarker}
            </div>
        </div>
    `;
}

/**
 * Render System Status section
 */
export function renderSystemStatus(data: DaemonStatusMessage | null): string {
    const running = data?.running ?? false;
    const action = running ? 'stop' : 'start';
    const isConfirming = daemonConfirmState?.needsConfirmation ?? false;

    const buttonText = isConfirming
        ? `Confirm ${running ? 'Stop' : 'Start'}`
        : (running ? 'Stop' : 'Start');

    const confirmingClass = isConfirming ? 'pulse-btn-confirming' : '';

    // Budget bars
    const dailyBar = renderBudgetBar('Daily',
        data?.budget_daily ?? 0,
        data?.budget_daily_aggregate ?? data?.budget_daily ?? 0,
        data?.budget_daily_limit ?? 0,
        data?.cluster_daily_limit ?? 0,
    );
    const weeklyBar = renderBudgetBar('Weekly',
        data?.budget_weekly ?? 0,
        data?.budget_weekly_aggregate ?? data?.budget_weekly ?? 0,
        data?.budget_weekly_limit ?? 0,
        data?.cluster_weekly_limit ?? 0,
    );
    const monthlyBar = renderBudgetBar('Monthly',
        data?.budget_monthly ?? 0,
        data?.budget_monthly_aggregate ?? data?.budget_monthly ?? 0,
        data?.budget_monthly_limit ?? 0,
        data?.cluster_monthly_limit ?? 0,
    );

    const budgetSection = (dailyBar || weeklyBar || monthlyBar)
        ? `<div style="margin-top:10px;">${dailyBar}${weeklyBar}${monthlyBar}</div>`
        : '';

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
        ${budgetSection}
    `;
}

/**
 * Handle system status actions (start/stop daemon, edit budget)
 * Uses two-click confirmation pattern for daemon control
 *
 * @returns true if action was executed, false if waiting for confirmation
 */
export async function handleSystemStatusAction(action: string): Promise<boolean> {
    const { sendMessage } = await import('../client');

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

        default:
            return true;
    }
}
