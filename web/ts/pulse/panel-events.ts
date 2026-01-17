/**
 * Pulse Panel Event Handlers
 *
 * Manages all DOM event listeners for the pulse panel using event delegation.
 * Uses a single delegated listener to prevent memory leaks from re-attaching
 * listeners on every render.
 */

export interface PanelEventHandlers {
    onToggleExpansion: (jobId: string) => Promise<void>;
    // Note: onForceTrigger and onJobAction removed - now handled by hydrated Button components
    onLoadMore: (jobId: string) => Promise<void>;
    onRetryExecutions: (jobId: string) => Promise<void>;
    onViewDetailed: (jobId: string) => Promise<void>;
    onProseLocation: (docId: string) => Promise<void>;
}

/**
 * Attach event listeners using event delegation.
 * Should be called ONCE during panel initialization.
 * Returns cleanup function to remove the listener.
 */
export function attachPanelEventListeners(
    panel: HTMLElement,
    handlers: PanelEventHandlers
): () => void {
    // Single delegated click handler for all pulse panel interactions
    const clickHandler = async (e: Event) => {
        const target = e.target as HTMLElement;

        // Handle toggle expand buttons
        const toggleBtn = target.closest('[data-action="toggle-expand"]') as HTMLElement | null;
        if (toggleBtn) {
            e.stopPropagation();
            const card = toggleBtn.closest('.pulse-job-card') as HTMLElement;
            const jobId = card?.dataset.jobId;
            if (jobId) {
                await handlers.onToggleExpansion(jobId);
            }
            return;
        }

        // Note: Job action buttons (pause, resume, delete, force-trigger) are now
        // hydrated Button components that handle their own click events

        // Handle "View detailed history" links
        const detailedLink = target.closest('[data-action="view-detailed"]') as HTMLElement | null;
        if (detailedLink) {
            e.preventDefault();
            e.stopPropagation();
            const card = detailedLink.closest('.pulse-job-card') as HTMLElement;
            const jobId = card?.dataset.jobId;
            if (jobId) {
                await handlers.onViewDetailed(jobId);
            }
            return;
        }

        // Handle "Load more" buttons
        const loadMoreBtn = target.closest('[data-action="load-more"]') as HTMLElement | null;
        if (loadMoreBtn) {
            e.stopPropagation();
            const card = loadMoreBtn.closest('.pulse-job-card') as HTMLElement;
            const jobId = card?.dataset.jobId;
            if (jobId) {
                await handlers.onLoadMore(jobId);
            }
            return;
        }

        // Handle "Retry" buttons (for execution fetch errors)
        const retryBtn = target.closest('[data-action="retry-executions"]') as HTMLElement | null;
        if (retryBtn) {
            e.stopPropagation();
            const jobId = retryBtn.dataset.jobId;
            if (jobId) {
                await handlers.onRetryExecutions(jobId);
            }
            return;
        }

        // Handle prose location links
        const proseLink = target.closest('.pulse-prose-link') as HTMLElement | null;
        if (proseLink) {
            e.preventDefault();
            e.stopPropagation();
            const docId = proseLink.dataset.docId;
            if (docId) {
                await handlers.onProseLocation(docId);
            }
            return;
        }
    };

    // Attach single delegated listener
    panel.addEventListener('click', clickHandler);

    // Return cleanup function
    return () => {
        panel.removeEventListener('click', clickHandler);
    };
}
