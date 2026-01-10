// System drawer for logs, progress, and system output

import { MAX_LOGS, appState } from './config.ts';
import { sendMessage } from './websocket.ts';
import { CSS } from './css-classes.ts';
import { formatTimestamp } from './html-utils.ts';
import { log, SEG } from './logger.ts';
import type { LogsMessage, LogEntry } from '../types/websocket';

// Make this a module
export {};

// Type-safe log level to CSS class mapping
const LOG_LEVEL_MAP: Record<string, string> = {
    ERROR: CSS.LOG.ERROR,
    WARN: CSS.LOG.WARN,
    INFO: CSS.LOG.INFO,
    DEBUG: CSS.LOG.DEBUG,
} as const;

// Log handling - accepts the full WebSocket message type
export function handleLogBatch(data: LogsMessage): void {
    log.info(SEG.WS, '📋 handleLogBatch called:', data);

    if (!data.data || !data.data.messages) {
        log.warn(SEG.WS, '⚠️  No data.data.messages in log batch:', data);
        return;
    }

    log.info(SEG.WS, `📝 Processing ${data.data.messages.length} log messages`);

    data.data.messages.forEach(msg => {
        appendLog(msg);

        // Show toast for errors at verbosity 0
        if (appState.currentVerbosity === 0 && (msg.level === 'ERROR' || msg.level === 'WARN')) {
            showToast(msg);
        }
    });

    updateLogCount();
}

function appendLog(msg: LogEntry): void {
    const logContent = document.getElementById('log-content') as HTMLElement | null;
    if (!logContent) return;

    const logLine = document.createElement('div');
    logLine.className = `${CSS.LOG.LINE} ${LOG_LEVEL_MAP[msg.level] || CSS.LOG.INFO}`;

    // Format timestamp
    const timestamp = formatTimestamp(msg.timestamp);

    // Build log line safely using DOM API
    const timestampEl = document.createElement('span');
    timestampEl.className = 'log-timestamp';
    timestampEl.textContent = timestamp;

    const loggerEl = document.createElement('span');
    loggerEl.className = 'log-logger';
    loggerEl.textContent = `[${msg.logger}]`;

    const messageEl = document.createElement('span');
    messageEl.className = 'log-message';
    messageEl.textContent = msg.message;  // Safe - auto-escapes HTML

    logLine.appendChild(timestampEl);
    logLine.appendChild(loggerEl);
    logLine.appendChild(messageEl);

    // Add fields if present
    if (msg.fields && Object.keys(msg.fields).length > 0) {
        const fieldsEl = document.createElement('span');
        fieldsEl.className = 'log-fields';
        fieldsEl.textContent = JSON.stringify(msg.fields);
        logLine.appendChild(fieldsEl);
    }

    // Add to buffer
    appState.logBuffer.push(logLine);

    // Maintain circular buffer
    if (appState.logBuffer.length > MAX_LOGS) {
        appState.logBuffer.shift();
    }

    // Append to DOM
    logContent.appendChild(logLine);

    // Remove old lines from DOM
    while (logContent.children.length > MAX_LOGS) {
        logContent.removeChild(logContent.firstChild!);
    }

    // Auto-scroll if panel is expanded and user is at bottom
    const panel = document.getElementById('log-panel') as HTMLElement | null;
    if (panel && !panel.classList.contains('collapsed')) {
        const isAtBottom = logContent.scrollHeight - logContent.scrollTop <= logContent.clientHeight + 50;
        if (isAtBottom) {
            logContent.scrollTop = logContent.scrollHeight;
        }
    }
}

function updateLogCount(): void {
    const count = document.getElementById('log-count') as HTMLElement | null;
    if (count) {
        count.textContent = '(' + appState.logBuffer.length + ')';
    }
}

export function clearLogs(): void {
    const logContent = document.getElementById('log-content') as HTMLElement | null;
    if (logContent) {
        logContent.innerHTML = '';
    }
    appState.logBuffer = [];
    updateLogCount();
}

// Toast notifications
function showToast(msg: LogEntry): void {
    const container = document.getElementById('toast-container') as HTMLElement | null;
    if (!container) return;

    const toast = document.createElement('div');
    toast.className = 'toast ' + msg.level.toLowerCase();

    const title = document.createElement('div');
    title.className = 'toast-title';
    title.textContent = msg.level === 'ERROR' ? 'Error' : 'Warning';

    const message = document.createElement('div');
    message.textContent = msg.message;

    toast.appendChild(title);
    toast.appendChild(message);
    container.appendChild(toast);

    // Auto-remove after 5 seconds
    setTimeout(() => {
        toast.classList.add('u-animate-fadeout');
        setTimeout(() => {
            if (container.contains(toast)) {
                container.removeChild(toast);
            }
        }, 300);
    }, 5000);
}

// Update download button state based on verbosity
function updateDownloadButton(): void {
    const downloadBtn = document.getElementById('download-logs') as HTMLButtonElement | null;
    if (!downloadBtn) return;

    if (appState.currentVerbosity < 2) {
        downloadBtn.disabled = true;
        downloadBtn.title = 'File logging disabled (verbosity < 2)';
    } else {
        downloadBtn.disabled = false;
        downloadBtn.title = 'Download log file (tmp/graph-debug.log)';
    }
}

// Initialize log panel event listeners
export function initSystemDrawer(): void {
    // Verbosity selector
    const verbositySelect = document.getElementById('verbosity-select') as HTMLSelectElement | null;
    if (verbositySelect) {
        verbositySelect.addEventListener('change', function(e: Event) {
            const target = e.target as HTMLSelectElement;
            const verbosity = parseInt(target.value);
            appState.currentVerbosity = verbosity;

            sendMessage({
                type: 'set_verbosity',
                verbosity: verbosity
            });

            // Update download button state
            updateDownloadButton();
        });
    }

    // Log panel toggle
    const logHeader = document.getElementById('system-drawer-header') as HTMLElement | null;
    if (logHeader) {
        logHeader.addEventListener('click', function(e: Event) {
            const target = e.target as HTMLElement;
            // Don't toggle if clicking on buttons
            if (target.tagName === 'BUTTON' || target.tagName === 'SELECT') return;

            const panel = document.getElementById('system-drawer') as HTMLElement | null;
            const toggleBtn = document.getElementById('toggle-logs') as HTMLElement | null;

            if (panel && toggleBtn) {
                const isCollapsed = panel.classList.contains(CSS.STATE.COLLAPSED);
                if (isCollapsed) {
                    panel.classList.remove(CSS.STATE.COLLAPSED);
                } else {
                    panel.classList.add(CSS.STATE.COLLAPSED);
                }
                toggleBtn.textContent = panel.classList.contains(CSS.STATE.COLLAPSED) ? '▲' : '▼';
            }
        });
    }
}
