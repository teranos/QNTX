/**
 * WebSocket handler for daemon_status messages
 * Routes daemon status updates to all components that need them
 */

import type { DaemonStatusMessage } from '../../types/websocket';
import { toast } from '../components/toast';
import { uiState } from '../ui-state.ts';

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
 * Check if budget limits are approaching and show warning toasts
 * Uses centralized UIState to track warning state across the session
 */
function checkBudgetWarnings(data: DaemonStatusMessage): void {
    const warnings = uiState.getBudgetWarnings();

    // Daily budget
    const dailyUsage = data.budget_daily ?? 0;
    const dailyLimit = data.budget_daily_limit ?? 0;
    if (dailyLimit > 0) {
        const dailyPercent = dailyUsage / dailyLimit;
        if (dailyPercent >= BUDGET_WARNING_THRESHOLD && !warnings.daily) {
            toast.warning(`Daily budget ${Math.round(dailyPercent * 100)}% used ($${dailyUsage.toFixed(2)}/$${dailyLimit.toFixed(2)})`);
            uiState.setBudgetWarning('daily', true);
        } else if (dailyPercent < BUDGET_WARNING_THRESHOLD) {
            uiState.setBudgetWarning('daily', false);
        }
    }

    // Weekly budget
    const weeklyUsage = data.budget_weekly ?? 0;
    const weeklyLimit = data.budget_weekly_limit ?? 0;
    if (weeklyLimit > 0) {
        const weeklyPercent = weeklyUsage / weeklyLimit;
        if (weeklyPercent >= BUDGET_WARNING_THRESHOLD && !warnings.weekly) {
            toast.warning(`Weekly budget ${Math.round(weeklyPercent * 100)}% used ($${weeklyUsage.toFixed(2)}/$${weeklyLimit.toFixed(2)})`);
            uiState.setBudgetWarning('weekly', true);
        } else if (weeklyPercent < BUDGET_WARNING_THRESHOLD) {
            uiState.setBudgetWarning('weekly', false);
        }
    }

    // Monthly budget
    const monthlyUsage = data.budget_monthly ?? 0;
    const monthlyLimit = data.budget_monthly_limit ?? 0;
    if (monthlyLimit > 0) {
        const monthlyPercent = monthlyUsage / monthlyLimit;
        if (monthlyPercent >= BUDGET_WARNING_THRESHOLD && !warnings.monthly) {
            toast.warning(`Monthly budget ${Math.round(monthlyPercent * 100)}% used ($${monthlyUsage.toFixed(2)}/$${monthlyLimit.toFixed(2)})`);
            uiState.setBudgetWarning('monthly', true);
        } else if (monthlyPercent < BUDGET_WARNING_THRESHOLD) {
            uiState.setBudgetWarning('monthly', false);
        }
    }
}
