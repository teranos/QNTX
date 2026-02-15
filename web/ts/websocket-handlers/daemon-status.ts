/**
 * WebSocket handler for daemon_status messages
 * Routes daemon status updates to all components that need them
 */

import type { DaemonStatusMessage } from '../../types/websocket';
import { toast } from '../toast';
import { uiState } from '../state/ui.ts';

const BUDGET_WARNING_THRESHOLD = 0.80; // Warn at 80% of budget

/**
 * Handle daemon_status WebSocket messages
 * Called by main.ts when daemon status updates arrive
 */
export async function handleDaemonStatus(data: DaemonStatusMessage): Promise<void> {
    // Update pulse panel if it exists
    const { updatePulsePanelDaemonStatus } = await import('../pulse-panel.ts');
    updatePulsePanelDaemonStatus(data);

    // Update Pulse daemon status indicator
    const { statusIndicators } = await import('../status-indicators.ts');
    statusIndicators.handlePulseDaemonStatus(data);

    // Check for budget warnings (only warn once per threshold crossing)
    checkBudgetWarnings(data);
}

/**
 * Check a single budget window against both node and cluster limits.
 * Uses aggregate spend (local + peers) which matches what CheckBudget() enforces.
 * Returns true if a warning was fired.
 */
function checkWindow(
    period: 'daily' | 'weekly' | 'monthly',
    aggregate: number,
    nodeLimit: number,
    clusterLimit: number,
    alreadyWarned: boolean,
): boolean {
    // Determine which limit is binding (closest to being exceeded)
    let bindingLimit = 0;
    let isCluster = false;

    if (nodeLimit > 0 && clusterLimit > 0) {
        // Both configured — the lower effective limit is binding
        if (clusterLimit <= nodeLimit) {
            bindingLimit = clusterLimit;
            isCluster = true;
        } else {
            bindingLimit = nodeLimit;
        }
    } else if (clusterLimit > 0) {
        bindingLimit = clusterLimit;
        isCluster = true;
    } else if (nodeLimit > 0) {
        bindingLimit = nodeLimit;
    }

    if (bindingLimit <= 0) return false;

    const percent = aggregate / bindingLimit;
    if (percent >= BUDGET_WARNING_THRESHOLD && !alreadyWarned) {
        const suffix = isCluster ? ' (cluster)' : '';
        toast.warning(`${period.charAt(0).toUpperCase() + period.slice(1)} budget ${Math.round(percent * 100)}% used ($${aggregate.toFixed(2)}/$${bindingLimit.toFixed(2)})${suffix}`);
        uiState.setBudgetWarning(period, true);
        return true;
    } else if (percent < BUDGET_WARNING_THRESHOLD) {
        uiState.setBudgetWarning(period, false);
    }
    return false;
}

/**
 * Check if budget limits are approaching and show warning toasts.
 * Uses aggregate spend (local + non-stale peers) against both node and cluster limits,
 * matching the enforcement logic in CheckBudget().
 */
function checkBudgetWarnings(data: DaemonStatusMessage): void {
    const warnings = uiState.getBudgetWarnings();

    checkWindow('daily',
        data.budget_daily_aggregate ?? data.budget_daily ?? 0,
        data.budget_daily_limit ?? 0,
        data.cluster_daily_limit ?? 0,
        warnings.daily,
    );

    checkWindow('weekly',
        data.budget_weekly_aggregate ?? data.budget_weekly ?? 0,
        data.budget_weekly_limit ?? 0,
        data.cluster_weekly_limit ?? 0,
        warnings.weekly,
    );

    checkWindow('monthly',
        data.budget_monthly_aggregate ?? data.budget_monthly ?? 0,
        data.budget_monthly_limit ?? 0,
        data.cluster_monthly_limit ?? 0,
        warnings.monthly,
    );
}
