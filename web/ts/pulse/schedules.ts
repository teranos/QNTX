/**
 * Schedules Section - Recurring timer jobs
 */

import type { ScheduledJobResponse } from './types';
import type { Execution } from './execution-types';
import type { PulsePanelState } from './panel-state';
import { formatInterval } from './types';
import { formatRelativeTime, escapeHtml, formatDuration } from './panel.ts';
import { Pulse } from '@generated/sym.js';
import type { RichError } from '../base-panel-error.ts';
import { buildTooltipText } from '../components/tooltip';

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
    const httpMatch = errorMessage.match(/(\d{3})/);
    if (httpMatch) {
        const status = parseInt(httpMatch[1], 10);
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
 * Render a single scheduled job card
 */
export function renderJobCard(job: ScheduledJobResponse, state: PulsePanelState): string {
    const isActive = job.state === 'active';
    const nextRun = formatRelativeTime(job.next_run_at);
    const lastRun = job.last_run_at ? formatRelativeTime(job.last_run_at) : 'Never';
    const isExpanded = state.expandedJobs.has(job.id);
    const expandIcon = isExpanded ? '▼' : '▶';

    // Build tooltips
    const statusTooltip = escapeHtml(buildStatusTooltip(job));
    const intervalTooltip = escapeHtml(buildIntervalTooltip(job));
    const nextRunTooltip = escapeHtml(buildTimeTooltip(job.next_run_at, 'Next run'));
    const lastRunTooltip = escapeHtml(buildTimeTooltip(job.last_run_at, 'Last run'));

    // Prose location link if job was created from a prose document
    const proseLocationHtml = job.created_from_doc ? `
        <div class="pulse-meta-row pulse-prose-location">
            <span class="pulse-meta-label">Source:</span>
            <a href="#" class="pulse-prose-link has-tooltip" data-doc-id="${escapeHtml(job.created_from_doc)}" data-tooltip="Open in prose editor">
                ▣ ${escapeHtml(job.created_from_doc)}
            </a>
        </div>
    ` : '';

    // Inline execution history (when expanded)
    const executionHistoryHtml = isExpanded ? renderExecutionHistory(job, state) : '';

    return `
        <div class="panel-card pulse-job-card ${isExpanded ? 'expanded' : ''}" data-job-id="${job.id}">
            <div class="panel-flex-between pulse-job-header">
                <button class="pulse-expand-toggle has-tooltip" data-action="toggle-expand" data-tooltip="${isExpanded ? 'Collapse' : 'Expand'}">
                    ${expandIcon}
                </button>
                <div class="panel-badge-icon pulse-job-badge pulse-badge-${job.state} has-tooltip"
                     data-tooltip="${statusTooltip}">
                    <span class="pulse-icon">${Pulse}</span>
                    <span class="pulse-state">${job.state}</span>
                </div>
                <div class="pulse-job-interval has-tooltip" data-tooltip="${intervalTooltip}">${formatInterval(job.interval_seconds ?? 0)}</div>
            </div>

            <div class="pulse-job-code">
                <code>${escapeHtml(job.ats_code)}</code>
            </div>

            <div class="pulse-job-meta">
                <div class="pulse-meta-row">
                    <span class="pulse-meta-label">Next run:</span>
                    <span class="pulse-meta-value has-tooltip" data-tooltip="${nextRunTooltip}">${nextRun}</span>
                </div>
                <div class="pulse-meta-row">
                    <span class="pulse-meta-label">Last run:</span>
                    <span class="pulse-meta-value has-tooltip" data-tooltip="${lastRunTooltip}">${lastRun}</span>
                </div>
                ${proseLocationHtml}
            </div>

            <div class="pulse-job-actions">
                <button class="pulse-btn pulse-btn-force has-tooltip"
                        data-action="force-trigger"
                        data-tooltip="Execute immediately\nBypasses scheduling and deduplication\nCreates a one-time execution">
                    Force Trigger
                </button>
                <button class="pulse-btn pulse-btn-${isActive ? 'pause' : 'resume'} has-tooltip"
                        data-action="${isActive ? 'pause' : 'resume'}"
                        data-tooltip="${isActive ? 'Pause scheduled executions\nJob will not run until resumed' : 'Resume scheduled executions\nJob will run on next interval'}">
                    ${isActive ? 'Pause' : 'Resume'}
                </button>
                <button class="pulse-btn pulse-btn-delete has-tooltip"
                        data-action="delete"
                        data-tooltip="Permanently delete this job\nThis action cannot be undone">
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

    // Show error state if fetch failed
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
function renderExecutionCard(exec: Execution): string {
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

