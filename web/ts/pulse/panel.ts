/**
 * Pulse Panel Rendering Functions
 *
 * Pure rendering functions extracted from PulsePanel class.
 * All functions are stateless and accept state as parameters.
 */

import type { ScheduledJob } from './types';
import type { PulseExecution } from './execution-types';
import type { PulsePanelState } from './panel-state';
import { formatInterval } from './types';

/**
 * Render the main panel template (header + content wrapper)
 */
export function renderPanelTemplate(): string {
    return `
        <div class="panel-header pulse-panel-header">
            <h2 class="panel-title"><span class="pulse-icon">꩜</span> Pulse</h2>
            <button class="panel-close pulse-close-btn" onclick="window.pulsePanel.hide()">✕</button>
        </div>
        <div class="panel-content pulse-panel-content">
            <!-- System Status Section -->
            <div class="pulse-section pulse-system-status">
                <h3 class="pulse-section-title">System Status</h3>
                <div id="pulse-system-status-content" class="pulse-section-content">
                    <div class="panel-loading">Loading system status...</div>
                </div>
            </div>

            <!-- Active Queue Section -->
            <div class="pulse-section pulse-active-queue">
                <h3 class="pulse-section-title">Active Queue</h3>
                <div id="pulse-active-queue-content" class="pulse-section-content">
                    <div class="panel-loading">Loading active jobs...</div>
                </div>
            </div>

            <!-- Schedules Section -->
            <div class="pulse-section pulse-schedules">
                <h3 class="pulse-section-title">Schedules</h3>
                <div id="pulse-schedules-content" class="pulse-section-content">
                    <div class="panel-loading">Loading scheduled jobs...</div>
                </div>
            </div>
        </div>
    `;
}

/**
 * Render empty state when no jobs exist
 */
export function renderEmptyState(): string {
    return `
        <div class="panel-empty pulse-empty">
            <p>No scheduled jobs yet</p>
            <p class="pulse-hint">Add schedules to ATS code blocks to see them here</p>
        </div>
    `;
}

/**
 * Render a single job card
 */
export function renderJobCard(job: ScheduledJob, state: PulsePanelState): string {
    const isActive = job.state === 'active';
    const nextRun = formatRelativeTime(job.next_run_at);
    const lastRun = job.last_run_at ? formatRelativeTime(job.last_run_at) : 'Never';
    const isExpanded = state.expandedJobs.has(job.id);
    const expandIcon = isExpanded ? '▼' : '▶';

    // Prose location link if job was created from a prose document
    const proseLocationHtml = job.created_from_doc ? `
        <div class="pulse-meta-row pulse-prose-location">
            <span class="pulse-meta-label">Source:</span>
            <a href="#" class="pulse-prose-link" data-doc-id="${escapeHtml(job.created_from_doc)}" title="Open in prose editor">
                ▣ ${escapeHtml(job.created_from_doc)}
            </a>
        </div>
    ` : '';

    // Inline execution history (when expanded)
    const executionHistoryHtml = isExpanded ? renderExecutionHistory(job, state) : '';

    return `
        <div class="panel-card pulse-job-card ${isExpanded ? 'expanded' : ''}" data-job-id="${job.id}">
            <div class="panel-flex-between pulse-job-header">
                <button class="pulse-expand-toggle" data-action="toggle-expand" title="${isExpanded ? 'Collapse' : 'Expand'}">
                    ${expandIcon}
                </button>
                <div class="panel-badge-icon pulse-job-badge pulse-badge-${job.state}">
                    <span class="pulse-icon">꩜</span>
                    <span class="pulse-state">${job.state}</span>
                </div>
                <div class="pulse-job-interval">${formatInterval(job.interval_seconds)}</div>
            </div>

            <div class="panel-code pulse-job-code">
                <code>${escapeHtml(job.ats_code)}</code>
            </div>

            <div class="pulse-job-meta">
                <div class="pulse-meta-row">
                    <span class="pulse-meta-label">Next run:</span>
                    <span class="pulse-meta-value">${nextRun}</span>
                </div>
                <div class="pulse-meta-row">
                    <span class="pulse-meta-label">Last run:</span>
                    <span class="pulse-meta-value">${lastRun}</span>
                </div>
                ${proseLocationHtml}
            </div>

            <div class="pulse-job-actions">
                <button class="pulse-btn pulse-btn-force"
                        data-action="force-trigger"
                        title="Force immediate execution (bypasses deduplication)">
                    Force Trigger
                </button>
                <button class="pulse-btn pulse-btn-${isActive ? 'pause' : 'resume'}"
                        data-action="${isActive ? 'pause' : 'resume'}"
                        title="${isActive ? 'Pause job' : 'Resume job'}">
                    ${isActive ? 'Pause' : 'Resume'}
                </button>
                <button class="pulse-btn pulse-btn-delete"
                        data-action="delete"
                        title="Delete job">
                    Delete
                </button>
            </div>

            ${executionHistoryHtml}
        </div>
    `;
}

/**
 * Render execution history section for an expanded job
 */
export function renderExecutionHistory(job: ScheduledJob, state: PulsePanelState): string {
    const executions = state.jobExecutions.get(job.id) || [];
    const isLoading = state.loadingExecutions.has(job.id);
    const error = state.executionErrors.get(job.id);
    const limit = state.executionLimits.get(job.id) || 5;

    if (isLoading) {
        return `
            <div class="pulse-execution-history">
                <div class="pulse-execution-loading">Loading execution history...</div>
            </div>
        `;
    }

    // Show error state if fetch failed
    if (error) {
        return `
            <div class="pulse-execution-history">
                <div class="pulse-execution-error">
                    <strong>Failed to load execution history:</strong> ${error}
                    <button class="pulse-retry-button" data-action="retry-executions" data-job-id="${job.id}">
                        Retry
                    </button>
                </div>
                <a href="#" class="pulse-detailed-link" data-action="view-detailed">View detailed history →</a>
            </div>
        `;
    }

    if (executions.length === 0) {
        return `
            <div class="pulse-execution-history">
                <div class="pulse-execution-empty">
                    No executions yet. Click "Force Trigger" to run immediately.
                </div>
                <a href="#" class="pulse-detailed-link" data-action="view-detailed">View detailed history →</a>
            </div>
        `;
    }

    const executionsToShow = executions.slice(0, limit);
    const hasMore = executions.length > limit;

    return `
        <div class="pulse-execution-history">
            <div class="pulse-execution-header">
                <h4>Recent Executions (${executions.length} total)</h4>
                <a href="#" class="pulse-detailed-link" data-action="view-detailed">View detailed history →</a>
            </div>
            <div class="pulse-execution-list">
                ${executionsToShow.map(exec => renderExecutionCard(exec)).join('')}
            </div>
            ${hasMore ? `
                <button class="pulse-load-more" data-action="load-more">
                    Load 10 more executions
                </button>
            ` : ''}
        </div>
    `;
}

/**
 * Render a single execution card
 */
export function renderExecutionCard(exec: PulseExecution): string {
    const statusClass = exec.status === 'completed' ? 'success' : exec.status === 'failed' ? 'error' : 'running';
    const duration = exec.duration_ms ? formatDuration(exec.duration_ms) : '—';
    const timeAgo = formatRelativeTime(exec.started_at);

    return `
        <div class="pulse-execution-card pulse-exec-${statusClass}">
            <div class="pulse-exec-status">${exec.status.toUpperCase()}</div>
            <div class="pulse-exec-time">${timeAgo}</div>
            <div class="pulse-exec-duration">${duration}</div>
            ${exec.result_summary ? `
                <div class="pulse-exec-summary">${escapeHtml(exec.result_summary)}</div>
            ` : ''}
            ${exec.error_message ? `
                <div class="pulse-exec-error">Error: ${escapeHtml(exec.error_message)}</div>
            ` : ''}
        </div>
    `;
}

/**
 * Format duration in milliseconds to human-readable string
 */
export function formatDuration(durationMs: number): string {
    if (durationMs < 1000) return `${durationMs}ms`;
    const seconds = Math.floor(durationMs / 1000);
    if (seconds < 60) return `${seconds}s`;
    const minutes = Math.floor(seconds / 60);
    const remainingSeconds = seconds % 60;
    return `${minutes}m ${remainingSeconds}s`;
}

/**
 * Format timestamp to relative time string (e.g., "5m ago", "2h from now")
 */
export function formatRelativeTime(timestamp: string): string {
    const date = new Date(timestamp);
    const now = new Date();
    const diffMs = date.getTime() - now.getTime();
    const diffSecs = Math.floor(Math.abs(diffMs) / 1000);
    const diffMins = Math.floor(diffSecs / 60);
    const diffHours = Math.floor(diffMins / 60);
    const diffDays = Math.floor(diffHours / 24);

    const isPast = diffMs < 0;
    const suffix = isPast ? 'ago' : 'from now';

    if (diffSecs < 60) {
        return `${diffSecs}s ${suffix}`;
    } else if (diffMins < 60) {
        return `${diffMins}m ${suffix}`;
    } else if (diffHours < 24) {
        return `${diffHours}h ${suffix}`;
    } else {
        return `${diffDays}d ${suffix}`;
    }
}

/**
 * Escape HTML special characters
 */
export function escapeHtml(text: string): string {
    const div = document.createElement('div');
    div.textContent = text;
    return div.innerHTML;
}

/**
 * Render System Status section
 */
export function renderSystemStatus(daemonStatus: any): string {
    const running = daemonStatus?.running || false;
    const workers = daemonStatus?.worker_count || 0;
    const dailySpend = daemonStatus?.budget_daily || 0;
    const dailyLimit = daemonStatus?.budget_daily_limit || 1.0;
    const weeklySpend = daemonStatus?.budget_weekly || 0;
    const weeklyLimit = daemonStatus?.budget_weekly_limit || 7.0;
    const monthlySpend = daemonStatus?.budget_monthly || 0;
    const monthlyLimit = daemonStatus?.budget_monthly_limit || 30.0;

    const dailyPercent = dailyLimit > 0 ? Math.min((dailySpend / dailyLimit) * 100, 100) : 0;
    const weeklyPercent = weeklyLimit > 0 ? Math.min((weeklySpend / weeklyLimit) * 100, 100) : 0;
    const monthlyPercent = monthlyLimit > 0 ? Math.min((monthlySpend / monthlyLimit) * 100, 100) : 0;

    return `
        <div class="pulse-system-grid">
            <!-- Daemon Status -->
            <div class="pulse-status-item">
                <div class="pulse-status-label">Daemon</div>
                <div class="pulse-status-value">
                    <span class="pulse-daemon-badge ${running ? 'running' : 'stopped'}">${running ? 'Running' : 'Stopped'}</span>
                </div>
                <div class="pulse-status-actions">
                    <button class="pulse-btn pulse-btn-sm" data-action="${running ? 'stop' : 'start'}-daemon">
                        ${running ? 'Stop' : 'Start'}
                    </button>
                </div>
            </div>

            <!-- Workers -->
            <div class="pulse-status-item">
                <div class="pulse-status-label">Workers</div>
                <div class="pulse-status-value">
                    <input type="number" id="pulse-worker-count" class="pulse-worker-input"
                           value="${workers}" min="1" max="10" ${!running ? 'disabled' : ''}>
                </div>
                <div class="pulse-status-actions">
                    <button class="pulse-btn pulse-btn-sm" data-action="update-workers" ${!running ? 'disabled' : ''}>
                        Update
                    </button>
                </div>
            </div>

            <!-- Daily Budget -->
            <div class="pulse-status-item pulse-budget-item">
                <div class="pulse-status-label">Daily Budget</div>
                <div class="pulse-budget-bar">
                    <div class="pulse-budget-fill" style="width: ${dailyPercent}%"></div>
                </div>
                <div class="pulse-budget-text">
                    $${dailySpend.toFixed(2)} / $${dailyLimit.toFixed(2)}
                </div>
            </div>

            <!-- Weekly Budget -->
            <div class="pulse-status-item pulse-budget-item">
                <div class="pulse-status-label">Weekly Budget</div>
                <div class="pulse-budget-bar">
                    <div class="pulse-budget-fill" style="width: ${weeklyPercent}%"></div>
                </div>
                <div class="pulse-budget-text">
                    $${weeklySpend.toFixed(2)} / $${weeklyLimit.toFixed(2)}
                </div>
            </div>

            <!-- Monthly Budget -->
            <div class="pulse-status-item pulse-budget-item">
                <div class="pulse-status-label">Monthly Budget</div>
                <div class="pulse-budget-bar">
                    <div class="pulse-budget-fill" style="width: ${monthlyPercent}%"></div>
                </div>
                <div class="pulse-budget-text">
                    $${monthlySpend.toFixed(2)} / $${monthlyLimit.toFixed(2)}
                </div>
            </div>

            <!-- Budget Controls -->
            <div class="pulse-status-item pulse-budget-controls">
                <button class="pulse-btn pulse-btn-sm" data-action="edit-budget">
                    Edit Budget Limits
                </button>
            </div>
        </div>
    `;
}

/**
 * Render Active Queue section
 */
export function renderActiveQueue(activeJobs: any[]): string {
    if (!activeJobs || activeJobs.length === 0) {
        return `
            <div class="pulse-empty-state">
                <p>No active jobs</p>
            </div>
        `;
    }

    return `
        <div class="pulse-active-jobs">
            ${activeJobs.map(job => `
                <div class="pulse-active-job" data-job-id="${job.id}">
                    <div class="pulse-job-header">
                        <span class="pulse-job-status ${job.status}">${job.status}</span>
                        <span class="pulse-job-time">${formatRelativeTime(job.created_at)}</span>
                    </div>
                    <div class="pulse-job-command">${escapeHtml(job.metadata?.command || job.type || 'Unknown')}</div>
                    ${job.metadata?.total_operations ? `
                        <div class="pulse-job-progress">
                            <div class="pulse-progress-bar">
                                <div class="pulse-progress-fill" style="width: ${Math.round((job.metadata.completed_operations || 0) / job.metadata.total_operations * 100)}%"></div>
                            </div>
                            <div class="pulse-progress-text">
                                ${job.metadata.completed_operations || 0} / ${job.metadata.total_operations}
                            </div>
                        </div>
                    ` : ''}
                    ${job.cost_usd ? `
                        <div class="pulse-job-cost">Cost: $${job.cost_usd.toFixed(3)}</div>
                    ` : ''}
                </div>
            `).join('')}
        </div>
    `;
}
