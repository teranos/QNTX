/**
 * System Status Section - Daemon status + budget bars
 *
 * Pulse starts because the node starts, so this section shows and does not set.
 * Budget bars show stacked local (solid) + peer (translucent) spend against
 * limits — a budget is am.toml's to name.
 */

import { Pulse } from '../sym';
import type { DaemonStatusMessage } from '../../types/websocket';

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
        </div>
        ${budgetSection}
    `;
}
