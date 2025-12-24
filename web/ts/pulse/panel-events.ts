/**
 * Pulse Panel Event Handlers
 *
 * Manages all DOM event listeners for the pulse panel using event delegation.
 * Separated from PulsePanel class to maintain clean separation between
 * event handling infrastructure and business logic.
 */

export interface PanelEventHandlers {
    onToggleExpansion: (jobId: string) => Promise<void>;
    onForceTrigger: (jobId: string) => Promise<void>;
    onJobAction: (jobId: string, action: string) => Promise<void>;
    onLoadMore: (jobId: string) => Promise<void>;
    onRetryExecutions: (jobId: string) => Promise<void>;
    onViewDetailed: (jobId: string) => Promise<void>;
    onProseLocation: (docId: string) => Promise<void>;
}

/**
 * Attach all event listeners to the pulse panel using event delegation.
 * Called after each render to wire up interactions.
 */
export function attachPanelEventListeners(
    panel: HTMLElement,
    handlers: PanelEventHandlers
): void {
    // Expand/collapse toggle buttons
    panel.querySelectorAll('[data-action="toggle-expand"]').forEach(btn => {
        btn.addEventListener('click', async (e) => {
            e.stopPropagation();
            const card = (e.currentTarget as HTMLElement).closest('.pulse-job-card') as HTMLElement;
            const jobId = card?.dataset.jobId;
            if (jobId) {
                await handlers.onToggleExpansion(jobId);
            }
        });
    });

    // Job action buttons (pause, resume, delete, force-trigger)
    panel.querySelectorAll('.pulse-btn').forEach(btn => {
        btn.addEventListener('click', async (e) => {
            e.stopPropagation();
            const button = e.currentTarget as HTMLElement;
            const card = button.closest('.pulse-job-card') as HTMLElement;
            const jobId = card?.dataset.jobId;
            const action = button.dataset.action;

            if (!jobId || !action) return;

            if (action === 'force-trigger') {
                await handlers.onForceTrigger(jobId);
            } else {
                await handlers.onJobAction(jobId, action);
            }
        });
    });

    // "View detailed history" links
    panel.querySelectorAll('[data-action="view-detailed"]').forEach(link => {
        link.addEventListener('click', async (e) => {
            e.preventDefault();
            e.stopPropagation();
            const card = (e.currentTarget as HTMLElement).closest('.pulse-job-card') as HTMLElement;
            const jobId = card?.dataset.jobId;
            if (jobId) {
                await handlers.onViewDetailed(jobId);
            }
        });
    });

    // "Load more" buttons
    panel.querySelectorAll('[data-action="load-more"]').forEach(btn => {
        btn.addEventListener('click', async (e) => {
            e.stopPropagation();
            const card = (e.currentTarget as HTMLElement).closest('.pulse-job-card') as HTMLElement;
            const jobId = card?.dataset.jobId;
            if (jobId) {
                await handlers.onLoadMore(jobId);
            }
        });
    });

    // "Retry" buttons (for execution fetch errors)
    panel.querySelectorAll('[data-action="retry-executions"]').forEach(btn => {
        btn.addEventListener('click', async (e) => {
            e.stopPropagation();
            const jobId = (e.currentTarget as HTMLElement).dataset.jobId;
            if (jobId) {
                await handlers.onRetryExecutions(jobId);
            }
        });
    });

    // Prose location links
    panel.querySelectorAll('.pulse-prose-link').forEach(link => {
        link.addEventListener('click', async (e) => {
            e.preventDefault();
            e.stopPropagation();
            const anchor = e.currentTarget as HTMLElement;
            const docId = anchor.dataset.docId;

            if (docId) {
                await handlers.onProseLocation(docId);
            }
        });
    });
}
