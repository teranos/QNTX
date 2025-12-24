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

    // Build dialog structure safely using createElement
    const errorDialog = document.createElement('div');
    errorDialog.className = 'error-dialog';

    // Header
    const header = document.createElement('div');
    header.className = 'error-dialog-header';

    const icon = document.createElement('span');
    icon.className = 'error-dialog-icon';
    icon.textContent = '⚠';

    const titleEl = document.createElement('h3');
    titleEl.textContent = title;  // Safe - auto-escapes HTML

    header.appendChild(icon);
    header.appendChild(titleEl);

    // Body
    const body = document.createElement('div');
    body.className = 'error-dialog-body';

    const messageEl = document.createElement('p');
    messageEl.textContent = message;  // Safe - auto-escapes HTML

    body.appendChild(messageEl);

    // Footer
    const footer = document.createElement('div');
    footer.className = 'error-dialog-footer';

    const buildInfo = document.createElement('div');
    buildInfo.className = 'error-dialog-build-info';

    const buildLabel = document.createElement('span');
    buildLabel.className = 'build-info-label';
    buildLabel.textContent = 'Server:';

    const buildValue = document.createElement('span');
    buildValue.className = 'build-info-value';
    buildValue.textContent = `${commitShort} · ${buildTime}`;

    buildInfo.appendChild(buildLabel);
    buildInfo.appendChild(buildValue);

    const okBtn = document.createElement('button');
    okBtn.className = 'error-dialog-btn-ok';
    okBtn.textContent = 'OK';

    footer.appendChild(buildInfo);
    footer.appendChild(okBtn);

    // Assemble dialog
    errorDialog.appendChild(header);
    errorDialog.appendChild(body);
    errorDialog.appendChild(footer);
    dialog.appendChild(errorDialog);

    document.body.appendChild(dialog);

    // Close on button click
    okBtn.addEventListener('click', () => {
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
