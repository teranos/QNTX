/**
 * Toast notification system (CSS Modules version)
 *
 * Lightweight notifications for non-critical errors and status updates
 * Styles are scoped via CSS Modules - no global class name collisions
 */

import type { BuildInfo } from '../../../types/core';
import { formatRelativeTime } from '../../html-utils';
import styles from './toast.module.css';

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
        container.className = styles.container;
        document.body.appendChild(container);
    }

    // Create toast element
    const toast = document.createElement('div');
    toast.className = `${styles.toast} ${styles[type]}`;

    // Icon for toast type
    const icons: Record<ToastType, string> = {
        error: '⚠',
        warning: '⚠',
        success: '✓',
        info: 'ℹ'
    };

    // Build toast structure
    const header = document.createElement('div');
    header.className = styles.header;

    const iconEl = document.createElement('div');
    iconEl.className = styles.icon;
    iconEl.textContent = icons[type];

    const messageEl = document.createElement('div');
    messageEl.className = styles.message;
    messageEl.textContent = message;

    header.appendChild(iconEl);
    header.appendChild(messageEl);
    toast.appendChild(header);

    // Add build info if requested and available
    if (showBuildInfo && cachedBuildInfo) {
        const commitShort = cachedBuildInfo.commit.substring(0, 7);

        let buildTime = 'unknown';
        if (cachedBuildInfo.build_time) {
            try {
                buildTime = formatRelativeTime(cachedBuildInfo.build_time);
            } catch (e) {
                buildTime = 'parse error';
            }
        }

        const buildInfo = document.createElement('div');
        buildInfo.className = styles.buildInfo;

        const buildLabel = document.createElement('span');
        buildLabel.className = styles.buildLabel;
        buildLabel.textContent = 'SERVER BUILD';

        const buildValue = document.createElement('span');
        buildValue.className = styles.buildValue;
        buildValue.textContent = `${commitShort} · ${buildTime}`;

        buildInfo.appendChild(buildLabel);
        buildInfo.appendChild(buildValue);
        toast.appendChild(buildInfo);
    }

    // Add close button
    const closeBtn = document.createElement('button');
    closeBtn.className = styles.close;
    closeBtn.textContent = '×';
    toast.appendChild(closeBtn);

    container.appendChild(toast);

    // Trigger animation
    setTimeout(() => toast.classList.add(styles.visible), 10);

    // Auto-dismiss after duration
    const dismissToast = () => {
        toast.classList.add(styles.dismissing);
        toast.classList.remove(styles.visible);
        setTimeout(() => toast.remove(), 400);
    };

    const timeoutId = setTimeout(dismissToast, duration);

    // Manual dismiss on close button
    closeBtn.addEventListener('click', () => {
        clearTimeout(timeoutId);
        toast.remove();
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
