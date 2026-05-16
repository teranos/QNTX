/**
 * Schedules Section - Recurring timer jobs
 */

import type { ScheduledJobResponse } from './types';
import type { Execution } from './execution-types';
import type { PulsePanelState } from './panel-state';
import { formatInterval } from './types';
import { formatRelativeTime, escapeHtml, formatDuration } from '../html-utils.ts';
import type { RichError } from '../base-panel-error.ts';
import { extractHttpStatus } from '../http-utils.ts';
import { buildTooltipText } from '../components/tooltip.ts';
import { buttonPlaceholder } from '../components/button.ts';

/**
 * Build a rich error for execution history failures
 */
function buildExecutionError(errorMessage: string, atsCode: string): RichError {
    const lowerError = errorMessage.toLowerCase();

    // Network/connection errors
    if (lowerError.includes('network') || lowerError.includes('failed to fetch') || lowerError.includes('connection')) {
        return {
            title: 'Network Error',
            message: 'Unable to load execution history',
            suggestion: 'Check your network connection and ensure the QNTX server is running',
            details: `ATS Code: ${atsCode}\nError: ${errorMessage}`
        };
    }

    // Timeout errors
    if (lowerError.includes('timeout')) {
        return {
            title: 'Request Timeout',
            message: 'The server took too long to respond',
            suggestion: 'The server may be under heavy load. Try again in a moment.',
            details: `ATS Code: ${atsCode}\nError: ${errorMessage}`
        };
    }

    // HTTP errors
    const status = extractHttpStatus(errorMessage);
    if (status !== null) {
        if (status >= 400 && status < 600) {
            const statusInfo: Record<number, { title: string; suggestion: string }> = {
                401: { title: 'Unauthorized', suggestion: 'Your session may have expired. Try refreshing the page.' },
                403: { title: 'Forbidden', suggestion: 'You may not have permission to view this job\'s history.' },
                404: { title: 'Not Found', suggestion: 'This scheduled job may have been deleted.' },
                500: { title: 'Server Error', suggestion: 'An internal error occurred. Try again later.' },
                503: { title: 'Service Unavailable', suggestion: 'The Pulse service may be restarting. Try again in a moment.' }
            };
            const info = statusInfo[status] || { title: `HTTP ${status}`, suggestion: 'An error occurred while loading execution history.' };
            return {
                title: info.title,
                message: errorMessage,
                status,
                suggestion: info.suggestion,
                details: `ATS Code: ${atsCode}`
            };
        }
    }

    // Generic error
    return {
        title: 'Failed to Load Executions',
        message: errorMessage,
        suggestion: 'Click Retry to try again, or check the detailed history view.',
        details: `ATS Code: ${atsCode}`
    };
}

/**
 * Render empty state when no scheduled jobs exist
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
 * Build a tooltip for the job status badge
 */
function buildStatusTooltip(job: ScheduledJobResponse): string {
    const entries: Record<string, unknown> = {
        'State': job.state,
        'Job ID': job.id.substring(0, 16) + '...'
    };

    if (job.state === 'active') {
        entries['Status'] = 'Running on schedule';
    } else if (job.state === 'paused') {
        entries['Status'] = 'Execution paused';
    } else if (job.state === 'stopping') {
        entries['Status'] = 'Being stopped';
    }

    if (job.created_at) {
        const createdDate = new Date(job.created_at);
        entries['Created'] = createdDate.toLocaleString();
    }

    return buildTooltipText(entries);
}

/**
 * Build a tooltip for the interval display
 */
function buildIntervalTooltip(job: ScheduledJobResponse): string {
    const seconds = job.interval_seconds ?? 0;
    const entries: Record<string, unknown> = {
        'Interval': `${seconds} seconds`,
        'Formatted': formatInterval(seconds)
    };

    if (job.next_run_at) {
        const nextDate = new Date(job.next_run_at);
        entries['Next run at'] = nextDate.toLocaleString();
    }

    return buildTooltipText(entries);
}

/**
 * Build a tooltip for time values with absolute date
 */
function buildTimeTooltip(isoTime: string | null | undefined, label: string): string {
    if (!isoTime) return `${label}: Never`;
    const date = new Date(isoTime);
    return `${label}: ${date.toLocaleString()}`;
}

/**
 * Render all scheduled jobs as a compact table
 */
export function renderJobTable(jobs: ScheduledJobResponse[], state: PulsePanelState): string {
    const rows = jobs.map(job => {
        const isActive = job.state === 'active';
        const nextRun = formatRelativeTime(job.next_run_at);
        const lastRun = job.last_run_at ? formatRelativeTime(job.last_run_at) : '—';
        const isExpanded = state.expandedJobs.has(job.id);
        const statusTooltip = escapeHtml(buildStatusTooltip(job));
        const intervalTooltip = escapeHtml(buildIntervalTooltip(job));
        const nextRunTooltip = escapeHtml(buildTimeTooltip(job.next_run_at, 'Next run'));
        const lastRunTooltip = escapeHtml(buildTimeTooltip(job.last_run_at, 'Last run'));
        const code = job.ats_code || job.handler_name || '';
        const shortId = !job.ats_code && job.id ? job.id.slice(-6) : '';

        const executionHistoryHtml = isExpanded ? renderExecutionHistory(job, state) : '';

        return `
            <tr class="pulse-table-row ${isExpanded ? 'expanded' : ''}" data-job-id="${job.id}">
                <td class="pulse-table-expand">
                    <button class="pulse-expand-toggle" data-action="toggle-expand">${isExpanded ? '▼' : '▶'}</button>
                </td>
                <td class="pulse-table-code"><code>${escapeHtml(code)}</code>${shortId ? ` <span class="pulse-schedule-label">${escapeHtml(shortId)}</span>` : ''}</td>
                <td class="pulse-table-state"><span class="pulse-badge-inline pulse-badge-${job.state} has-tooltip" data-tooltip="${statusTooltip}">${job.state}</span></td>
                <td class="pulse-table-interval has-tooltip" data-tooltip="${intervalTooltip}">${formatInterval(job.interval_seconds ?? 0)}</td>
                <td class="pulse-table-time has-tooltip" data-tooltip="${nextRunTooltip}">${nextRun}</td>
                <td class="pulse-table-time has-tooltip" data-tooltip="${lastRunTooltip}">${lastRun}</td>
                <td class="pulse-table-actions">
                    ${buttonPlaceholder(`force-trigger-${job.id}`, '▶')}
                    ${buttonPlaceholder(`toggle-state-${job.id}`, isActive ? '⏸' : '▶')}
                    ${buttonPlaceholder(`delete-${job.id}`, '✕')}
                </td>
            </tr>
            ${isExpanded ? `<tr class="pulse-table-expansion" data-job-id="${job.id}"><td colspan="7">${executionHistoryHtml}</td></tr>` : ''}
        `;
    }).join('');

    return `
        <table class="pulse-schedule-table">
            <thead>
                <tr>
                    <th></th>
                    <th>Job</th>
                    <th>State</th>
                    <th>Interval</th>
                    <th>Next</th>
                    <th>Last</th>
                    <th>Actions</th>
                </tr>
            </thead>
            <tbody>${rows}</tbody>
        </table>
    `;
}

/**
 * Render execution history section for an expanded job
 */
function renderExecutionHistory(job: ScheduledJobResponse, state: PulsePanelState): string {
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

    if (error) {
        const richError = buildExecutionError(error, job.ats_code);
        return `
            <div class="pulse-execution-history">
                <div class="pulse-execution-error pulse-rich-error">
                    <div class="pulse-error-title">${escapeHtml(richError.title)}</div>
                    <div class="pulse-error-message">${escapeHtml(richError.message)}</div>
                    ${richError.suggestion ? `<div class="pulse-error-suggestion">${escapeHtml(richError.suggestion)}</div>` : ''}
                    ${richError.details ? `
                        <details class="pulse-error-details">
                            <summary>Error Details</summary>
                            <pre class="pulse-error-details-content">${escapeHtml(richError.details)}</pre>
                        </details>
                    ` : ''}
                    <button class="pulse-retry-button" data-action="retry-executions" data-job-id="${job.id}">
                        Retry
                    </button>
                </div>
            </div>
        `;
    }

    if (executions.length === 0) {
        return `
            <div class="pulse-execution-history">
                <div class="pulse-execution-empty">
                    No executions yet. Click "Force Trigger" to run immediately.
                </div>
            </div>
        `;
    }

    const executionsToShow = executions.slice(0, limit);
    const hasMore = executions.length > limit;

    return `
        <div class="pulse-execution-history">
            <div class="pulse-execution-header">
                <h4>Executions (${executions.length})</h4>
            </div>
            <div class="pulse-execution-list">
                ${executionsToShow.map(exec => renderExecutionCard(exec, state)).join('')}
            </div>
            ${hasMore ? `
                <button class="pulse-load-more" data-action="load-more">
                    Load 10 more
                </button>
            ` : ''}
        </div>
    `;
}

/**
 * Render a single execution card — expandable to show stages/children/logs
 */
function renderExecutionCard(exec: Execution, state: PulsePanelState): string {
    const statusClass = exec.status === 'completed' ? 'success' : exec.status === 'failed' ? 'error' : 'running';
    const duration = exec.duration_ms ? formatDuration(exec.duration_ms) : '—';
    const timeAgo = formatRelativeTime(exec.started_at);
    const isExpanded = state.expandedExecutions.has(exec.id);
    const stages = state.executionStages.get(exec.id);
    const children = state.executionChildren.get(exec.id);

    return `
        <div class="pulse-execution-card pulse-exec-${statusClass} ${isExpanded ? 'pulse-exec-expanded' : ''}" data-execution-id="${exec.id}" data-async-job-id="${exec.async_job_id || ''}" data-action="toggle-execution">
            <div class="pulse-exec-header">
                <span class="pulse-exec-toggle">${isExpanded ? '▼' : '▶'}</span>
                <span class="pulse-exec-status">${exec.status.toUpperCase()}</span>
                <span class="pulse-exec-time">${timeAgo}</span>
                <span class="pulse-exec-duration">${duration}</span>
            </div>
            ${exec.result_summary ? `
                <div class="pulse-exec-summary">${escapeHtml(exec.result_summary)}</div>
            ` : ''}
            ${exec.error_message ? `
                <div class="pulse-exec-error-msg">${escapeHtml(exec.error_message)}</div>
            ` : ''}
            ${isExpanded ? renderExecutionDetail(stages, children, state) : ''}
        </div>
    `;
}

/**
 * Render expanded execution detail: stages, children, or loading state
 */
function renderExecutionDetail(
    stages: import('./execution-types').JobStagesResponse | undefined,
    children: import('./execution-types').JobChildrenResponse | undefined,
    state: PulsePanelState
): string {
    if (!stages) {
        return `<div class="pulse-exec-detail"><div class="pulse-execution-loading">Loading...</div></div>`;
    }

    // Show children if no stages but children exist
    if (stages.stages.length === 0 && children && children.children.length > 0) {
        return `<div class="pulse-exec-detail">${renderChildren(children, state)}</div>`;
    }

    if (stages.stages.length === 0) {
        return `<div class="pulse-exec-detail"><div class="pulse-exec-no-detail">No stages or logs available</div></div>`;
    }

    return `
        <div class="pulse-exec-detail">
            ${stages.stages.map(stage => `
                <div class="pulse-stage">
                    <div class="pulse-stage-header">${escapeHtml(stage.stage)}</div>
                    ${stage.tasks.map(task => renderTaskLogs(task, stages.job_id, state)).join('')}
                </div>
            `).join('')}
        </div>
    `;
}

/**
 * Render child jobs for an orchestrator execution
 */
function renderChildren(
    childrenResponse: import('./execution-types').JobChildrenResponse,
    state: PulsePanelState
): string {
    return `
        <div class="pulse-children">
            <div class="pulse-children-header">Child Jobs (${childrenResponse.children.length})</div>
            ${childrenResponse.children.map(child => {
                const isExpanded = state.expandedChildren.has(child.id);
                const childStagesData = state.childStages.get(child.id);

                return `
                    <div class="pulse-child ${isExpanded ? 'pulse-child-expanded' : ''}" data-child-id="${child.id}">
                        <div class="pulse-child-header" data-action="toggle-child">
                            <span class="pulse-exec-toggle">${isExpanded ? '▼' : '▶'}</span>
                            <span class="pulse-child-handler">${escapeHtml(child.handler_name)}</span>
                            <span class="pulse-child-status pulse-child-status-${escapeHtml(child.status)}">${escapeHtml(child.status)}</span>
                            <span class="pulse-child-progress">${Math.round(child.progress_pct ?? 0)}%</span>
                        </div>
                        ${child.error ? `<div class="pulse-child-error">${escapeHtml(child.error)}</div>` : ''}
                        ${isExpanded ? renderChildDetail(child.id, childStagesData, state) : ''}
                    </div>
                `;
            }).join('')}
        </div>
    `;
}

/**
 * Render expanded child job detail
 */
function renderChildDetail(
    childId: string,
    stages: import('./execution-types').JobStagesResponse | undefined,
    state: PulsePanelState
): string {
    if (!stages) {
        return `<div class="pulse-exec-detail"><div class="pulse-execution-loading">Loading...</div></div>`;
    }

    if (stages.stages.length === 0) {
        return `<div class="pulse-exec-detail"><div class="pulse-exec-no-detail">No stages</div></div>`;
    }

    return `
        <div class="pulse-exec-detail">
            ${stages.stages.map(stage => `
                <div class="pulse-stage">
                    <div class="pulse-stage-header">${escapeHtml(stage.stage)}</div>
                    ${stage.tasks.map(task => renderTaskLogs(task, childId, state)).join('')}
                </div>
            `).join('')}
        </div>
    `;
}

/**
 * Render task logs inline (auto-loaded)
 */
function renderTaskLogs(
    task: { task_id: string; log_count?: number },
    jobId: string,
    state: PulsePanelState
): string {
    const taskKey = `${jobId}:${task.task_id}`;
    const logs = state.taskLogs.get(taskKey);

    if (!logs) {
        return `<div class="pulse-task-loading" data-task-key="${taskKey}" data-job-id="${jobId}" data-task-id="${task.task_id}">Loading logs...</div>`;
    }

    if (logs.logs.length === 0) {
        return '';
    }

    return `
        <div class="pulse-task-logs">
            ${logs.logs.map(entry => {
                const time = formatLogTimestamp(entry.timestamp);
                return `
                    <div class="pulse-log-entry pulse-log-${escapeHtml(entry.level)}">
                        <span class="pulse-log-time">${time}</span>
                        <span class="pulse-log-level">${escapeHtml(entry.level.toUpperCase())}</span>
                        <span class="pulse-log-msg">${escapeHtml(entry.message)}</span>
                    </div>
                `;
            }).join('')}
        </div>
    `;
}

/**
 * Format log timestamp — show time only for today, relative otherwise
 */
function formatLogTimestamp(timestamp: string): string {
    const date = new Date(timestamp);
    const now = new Date();

    if (date.getDate() === now.getDate() && date.getMonth() === now.getMonth() && date.getFullYear() === now.getFullYear()) {
        return date.toLocaleTimeString('en-US', {
            hour: '2-digit',
            minute: '2-digit',
            second: '2-digit',
            hour12: false
        });
    }

    return formatRelativeTime(timestamp);
}

