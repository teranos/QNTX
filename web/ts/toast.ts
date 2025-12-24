/**
 * Toast notification system
 *
 * Lightweight notifications for non-critical errors and status updates
 */

interface BuildInfo {
    version: string;
    commit: string;
    build_time?: string;
}

let cachedBuildInfo: BuildInfo | null = null;

/**
 * Cache build info from version WebSocket message
 */
export function cacheBuildInfo(info: BuildInfo): void {
    cachedBuildInfo = info;
}

export type ToastType = 'error' | 'warning' | 'success' | 'info';

export interface ToastOptions {
    type?: ToastType;
    duration?: number;
    showBuildInfo?: boolean;
}

/**
 * Show a toast notification
 */
export function showToast(message: string, options: ToastOptions = {}): void {
    const {
        type = 'info',
        duration = 5000,
        showBuildInfo = false
    } = options;

    // Get or create toast container
    let container = document.getElementById('toast-container');
    if (!container) {
        container = document.createElement('div');
        container.id = 'toast-container';
        document.body.appendChild(container);
    }

    // Create toast element
    const toast = document.createElement('div');
    toast.className = `toast toast-${type}`;

    // Icon for toast type
    const icons = {
        error: '⚠',
        warning: '⚠',
        success: '✓',
        info: 'ℹ'
    };

    let content = `
        <div class="toast-header">
            <div class="toast-icon">${icons[type]}</div>
            <div class="toast-message">${message}</div>
        </div>
    `;

    // Add build info if requested and available
    if (showBuildInfo && cachedBuildInfo) {
        const commitShort = cachedBuildInfo.commit.substring(0, 7);

        // Format build time
        let buildTime = 'unknown';
        if (cachedBuildInfo.build_time) {
            try {
                const buildDate = new Date(cachedBuildInfo.build_time);
                const now = new Date();
                const diffMs = now.getTime() - buildDate.getTime();
                const diffSecs = Math.floor(diffMs / 1000);
                const diffMins = Math.floor(diffMs / 60000);

                if (diffSecs < 60) {
                    buildTime = `${diffSecs}s ago`;
                } else if (diffMins < 60) {
                    buildTime = `${diffMins}m ago`;
                } else if (diffMins < 1440) {
                    buildTime = `${Math.floor(diffMins / 60)}h ago`;
                } else {
                    buildTime = buildDate.toLocaleDateString('en-US', {
                        month: 'short',
                        day: 'numeric',
                        hour: '2-digit',
                        minute: '2-digit',
                        hour12: false
                    });
                }
            } catch (e) {
                buildTime = 'parse error';
            }
        }

        content += `
            <div class="toast-build-info">
                <span class="toast-build-label">SERVER BUILD</span>
                <span class="toast-build-value">${commitShort} · ${buildTime}</span>
            </div>
        `;
    }

    // Add close button
    content += `<button class="toast-close">×</button>`;

    toast.innerHTML = content;
    container.appendChild(toast);

    // Trigger animation
    setTimeout(() => toast.classList.add('toast-visible'), 10);

    // Auto-dismiss after duration
    const dismissToast = () => {
        toast.classList.add('toast-dismissing');
        toast.classList.remove('toast-visible');
        setTimeout(() => toast.remove(), 400); // Slower fade out
    };

    const timeoutId = setTimeout(dismissToast, duration);

    // Manual dismiss on close button
    const closeBtn = toast.querySelector('.toast-close');
    closeBtn?.addEventListener('click', () => {
        clearTimeout(timeoutId);
        dismissToast();
    });
}

/**
 * Convenience methods for common toast types
 */
export const toast = {
    error: (message: string, showBuildInfo = false) =>
        showToast(message, { type: 'error', showBuildInfo }),

    warning: (message: string) =>
        showToast(message, { type: 'warning' }),

    success: (message: string) =>
        showToast(message, { type: 'success', duration: 3000 }),

    info: (message: string) =>
        showToast(message, { type: 'info', duration: 4000 }),
};
