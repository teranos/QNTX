/**
 * Active Queue Section - Currently running async jobs
 */

import { formatRelativeTime, escapeHtml } from './panel.ts';
import { log, SEG } from '../logger';

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
            ${activeJobs.map(job => renderActiveJob(job)).join('')}
        </div>
    `;
}

/**
 * Render a single active job
 */
function renderActiveJob(job: any): string {
    const timeAgo = formatRelativeTime(job.created_at);
    const completed = job.metadata?.completed_operations || 0;
    const total = job.metadata?.total_operations || 0;

    return `
        <div class="pulse-active-job" data-job-id="${job.id}">
            <div class="pulse-job-header">
                <span class="pulse-job-status ${job.status}">${job.status}</span>
                <span class="pulse-job-time">${timeAgo}</span>
            </div>
            <div class="pulse-job-command">${escapeHtml(job.metadata?.command || job.type || 'Unknown')}</div>
            ${total > 0 ? `
                <div class="pulse-job-operations">
                    ${completed} / ${total} operations
                </div>
            ` : ''}
            ${job.cost_usd ? `
                <div class="pulse-job-cost">Cost: $${job.cost_usd.toFixed(3)}</div>
            ` : ''}
        </div>
    `;
}

/**
 * Fetch active jobs from API
 */
export async function fetchActiveJobs(): Promise<any[]> {
    try {
        const response = await fetch('/api/pulse/jobs?limit=50');
        if (!response.ok) {
            log.error(SEG.PULSE, 'Failed to fetch active jobs:', response.statusText);
            return [];
        }

        const data = await response.json();
        const jobs = data.jobs || [];

        // Filter to only active jobs (running, queued)
        return jobs.filter((job: any) =>
            job.status === 'running' ||
            job.status === 'queued' ||
            job.status === 'paused'
        );
    } catch (error) {
        log.error(SEG.PULSE, 'Error fetching active jobs:', error);
        return [];
    }
}
