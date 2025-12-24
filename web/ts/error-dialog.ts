/**
 * Error dialog with build information
 *
 * Shows errors with server build details to help debugging
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

/**
 * Show error dialog with build information
 */
export function showErrorDialog(title: string, message: string): void {
    const dialog = document.createElement('div');
    dialog.className = 'error-dialog-overlay';

    const commitShort = cachedBuildInfo?.commit.substring(0, 7) || 'unknown';
    const buildTime = cachedBuildInfo?.build_time
        ? new Date(cachedBuildInfo.build_time).toLocaleString('en-US', {
            month: 'short',
            day: 'numeric',
            year: 'numeric',
            hour: '2-digit',
            minute: '2-digit',
            hour12: false
        })
        : 'unknown';

    dialog.innerHTML = `
        <div class="error-dialog">
            <div class="error-dialog-header">
                <span class="error-dialog-icon">⚠</span>
                <h3>${title}</h3>
            </div>
            <div class="error-dialog-body">
                <p>${message}</p>
            </div>
            <div class="error-dialog-footer">
                <div class="error-dialog-build-info">
                    <span class="build-info-label">Server:</span>
                    <span class="build-info-value">${commitShort} · ${buildTime}</span>
                </div>
                <button class="error-dialog-btn-ok">OK</button>
            </div>
        </div>
    `;

    document.body.appendChild(dialog);

    // Close on button click
    const okBtn = dialog.querySelector('.error-dialog-btn-ok');
    okBtn?.addEventListener('click', () => {
        dialog.remove();
    });

    // Close on overlay click
    dialog.addEventListener('click', (e) => {
        if (e.target === dialog) {
            dialog.remove();
        }
    });

    // Close on Escape key
    const handleEscape = (e: KeyboardEvent) => {
        if (e.key === 'Escape') {
            dialog.remove();
            document.removeEventListener('keydown', handleEscape);
        }
    };
    document.addEventListener('keydown', handleEscape);
}
