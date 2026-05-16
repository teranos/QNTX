/**
 * Pulse Panel Event Handlers
 *
 * Manages all DOM event listeners for the pulse panel using event delegation.
 * Uses a single delegated listener to prevent memory leaks from re-attaching
 * listeners on every render.
 */

export interface PanelEventHandlers {
    onToggleExpansion: (jobId: string) => Promise<void>;
    onLoadMore: (jobId: string) => Promise<void>;
    onRetryExecutions: (jobId: string) => Promise<void>;
    onProseLocation: (docId: string) => Promise<void>;
    onToggleExecution: (executionId: string, asyncJobId: string) => Promise<void>;
    onToggleChild: (childId: string) => Promise<void>;
    onAutoLoadTaskLogs: (jobId: string, taskId: string) => Promise<void>;
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

        // Handle toggle expand buttons (job row)
        const toggleBtn = target.closest('[data-action="toggle-expand"]') as HTMLElement | null;
        if (toggleBtn) {
            e.stopPropagation();
            const row = toggleBtn.closest('[data-job-id]') as HTMLElement;
            const jobId = row?.dataset.jobId;
            if (jobId) {
                await handlers.onToggleExpansion(jobId);
            }
            return;
        }

        // Handle toggle execution expand (execution card)
        const execHeader = target.closest('[data-action="toggle-execution"]') as HTMLElement | null;
        if (execHeader) {
            e.stopPropagation();
            const card = execHeader.closest('[data-execution-id]') as HTMLElement;
            const executionId = card?.dataset.executionId;
            const asyncJobId = card?.dataset.asyncJobId || '';
            if (executionId) {
                await handlers.onToggleExecution(executionId, asyncJobId);
            }
            return;
        }

        // Handle toggle child expand
        const childHeader = target.closest('[data-action="toggle-child"]') as HTMLElement | null;
        if (childHeader) {
            e.stopPropagation();
            const childEl = childHeader.closest('[data-child-id]') as HTMLElement;
            const childId = childEl?.dataset.childId;
            if (childId) {
                await handlers.onToggleChild(childId);
            }
            return;
        }

        // Handle "Load more" buttons
        const loadMoreBtn = target.closest('[data-action="load-more"]') as HTMLElement | null;
        if (loadMoreBtn) {
            e.stopPropagation();
            const row = loadMoreBtn.closest('[data-job-id]') as HTMLElement;
            const jobId = row?.dataset.jobId;
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

    // MutationObserver to auto-load task logs when they appear in the DOM
    const observer = new MutationObserver((mutations) => {
        for (const mutation of mutations) {
            for (const node of mutation.addedNodes) {
                if (!(node instanceof HTMLElement)) continue;

                // Check the node itself and its descendants for task loading placeholders
                const loadingElements = node.matches?.('.pulse-task-loading')
                    ? [node]
                    : Array.from(node.querySelectorAll('.pulse-task-loading'));

                for (const el of loadingElements) {
                    const jobId = (el as HTMLElement).dataset.jobId;
                    const taskId = (el as HTMLElement).dataset.taskId;
                    if (jobId && taskId) {
                        handlers.onAutoLoadTaskLogs(jobId, taskId);
                    }
                }
            }
        }
    });

    observer.observe(panel, { childList: true, subtree: true });

    // Attach single delegated listener
    panel.addEventListener('click', clickHandler);

    // Return cleanup function
    return () => {
        panel.removeEventListener('click', clickHandler);
        observer.disconnect();
    };
}
