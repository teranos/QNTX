/**
 * Toast notification system
 *
 * Lightweight notifications for non-critical errors and status updates
 */

import type { BuildInfo } from '../types/core';
import { formatRelativeTime } from './html-utils';

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
    /** Structured error details from the backend (displayed in expandable section) */
    details?: string[];
}

/**
 * Show a toast notification
 */
export function showToast(message: string, options: ToastOptions = {}): void {
    const {
        type = 'info',
        duration = 5000,
        showBuildInfo = false,
        details
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

    // Build toast structure safely using createElement
    const header = document.createElement('div');
    header.className = 'toast-header';

    const iconEl = document.createElement('div');
    iconEl.className = 'toast-icon';
    iconEl.textContent = icons[type];

    const messageEl = document.createElement('div');
    messageEl.className = 'toast-message';
    messageEl.textContent = message;  // Safe - auto-escapes HTML

    header.appendChild(iconEl);
    header.appendChild(messageEl);
    toast.appendChild(header);

    // Add build info if requested and available
    if (showBuildInfo && cachedBuildInfo) {
        const commitShort = cachedBuildInfo.commit.substring(0, 7);

        // Format build time using formatRelativeTime
        let buildTime = 'unknown';
        if (cachedBuildInfo.build_time) {
            try {
                buildTime = formatRelativeTime(cachedBuildInfo.build_time);
            } catch (error: unknown) {
                buildTime = 'parse error';
            }
        }

        const buildInfo = document.createElement('div');
        buildInfo.className = 'toast-build-info';

        const buildLabel = document.createElement('span');
        buildLabel.className = 'toast-build-label';
        buildLabel.textContent = 'SERVER BUILD';

        const buildValue = document.createElement('span');
        buildValue.className = 'toast-build-value';
        buildValue.textContent = `${commitShort} · ${buildTime}`;

        buildInfo.appendChild(buildLabel);
        buildInfo.appendChild(buildValue);
        toast.appendChild(buildInfo);
    }

    // Add expandable details section if present
    if (details && details.length > 0) {
        const detailsContainer = document.createElement('div');
        detailsContainer.className = 'toast-details';
        detailsContainer.style.display = 'none';
        detailsContainer.style.marginTop = '6px';
        detailsContainer.style.paddingTop = '6px';
        detailsContainer.style.borderTop = '1px solid rgba(255,255,255,0.1)';
        detailsContainer.style.fontSize = '11px';
        detailsContainer.style.fontFamily = 'monospace';
        detailsContainer.style.color = 'var(--text-secondary, #999)';
        detailsContainer.style.maxHeight = '120px';
        detailsContainer.style.overflowY = 'auto';

        for (const detail of details) {
            const line = document.createElement('div');
            line.textContent = detail;
            line.style.paddingLeft = '8px';
            line.style.marginBottom = '2px';
            detailsContainer.appendChild(line);
        }

        const toggleBtn = document.createElement('button');
        toggleBtn.className = 'toast-details-toggle';
        toggleBtn.textContent = 'Show details';
        toggleBtn.style.background = 'none';
        toggleBtn.style.border = 'none';
        toggleBtn.style.color = 'var(--text-secondary, #999)';
        toggleBtn.style.cursor = 'pointer';
        toggleBtn.style.fontSize = '11px';
        toggleBtn.style.padding = '4px 0 0 0';
        toggleBtn.style.textDecoration = 'underline';

        toggleBtn.addEventListener('click', (e) => {
            e.stopPropagation();
            const isHidden = detailsContainer.style.display === 'none';
            detailsContainer.style.display = isHidden ? 'block' : 'none';
            toggleBtn.textContent = isHidden ? 'Hide details' : 'Show details';
        });

        toast.appendChild(toggleBtn);
        toast.appendChild(detailsContainer);
    }

    // Add close button
    const closeBtn = document.createElement('button');
    closeBtn.className = 'toast-close';
    closeBtn.textContent = '×';
    toast.appendChild(closeBtn);

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

    // Manual dismiss on close button - instant removal
    closeBtn.addEventListener('click', () => {
        clearTimeout(timeoutId);
        toast.remove(); // Instant removal when clicking close
    });
}

/**
 * Convenience methods for common toast types
 */
export const toast = {
    error: (message: string, showBuildInfo = false, details?: string[]) =>
        showToast(message, { type: 'error', showBuildInfo, details }),

    warning: (message: string) =>
        showToast(message, { type: 'warning' }),

    success: (message: string) =>
        showToast(message, { type: 'success', duration: 3000 }),

    info: (message: string) =>
        showToast(message, { type: 'info', duration: 4000 }),
};
