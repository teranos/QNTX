// Log panel for system logs

import { MAX_LOGS, state } from './config.ts';
import { sendMessage } from './websocket.ts';
import type { LogMessage, LogBatchData } from '../types/core';

// Make this a module
export {};

// Log handling
export function handleLogBatch(data: LogBatchData): void {
    console.log('📋 handleLogBatch called:', data);

    if (!data.data || !data.data.messages) {
        console.warn('⚠️  No data.data.messages in log batch:', data);
        return;
    }

    console.log(`📝 Processing ${data.data.messages.length} log messages`);

    data.data.messages.forEach(msg => {
        appendLog(msg);

        // Show toast for errors at verbosity 0
        if (state.currentVerbosity === 0 && (msg.level === 'ERROR' || msg.level === 'WARN')) {
            showToast(msg);
        }
    });

    updateLogCount();
}

function appendLog(msg: LogMessage): void {
    const logContent = document.getElementById('log-content') as HTMLElement | null;
    if (!logContent) return;

    const logLine = document.createElement('div');
    logLine.className = 'log-line log-' + msg.level.toLowerCase();

    // Format timestamp
    const timestamp = new Date(msg.timestamp).toLocaleTimeString('en-US', {
        hour12: false,
        hour: '2-digit',
        minute: '2-digit',
        second: '2-digit',
        fractionalSecondDigits: 3
    } as Intl.DateTimeFormatOptions);

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
    state.logBuffer.push(logLine);

    // Maintain circular buffer
    if (state.logBuffer.length > MAX_LOGS) {
        state.logBuffer.shift();
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
        count.textContent = '(' + state.logBuffer.length + ')';
    }
}

export function clearLogs(): void {
    const logContent = document.getElementById('log-content') as HTMLElement | null;
    if (logContent) {
        logContent.innerHTML = '';
    }
    state.logBuffer = [];
    updateLogCount();
}

// Toast notifications
function showToast(msg: LogMessage): void {
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
        toast.style.animation = 'fadeOut 0.3s ease-out';
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

    if (state.currentVerbosity < 2) {
        downloadBtn.disabled = true;
        downloadBtn.title = 'File logging disabled (verbosity < 2)';
    } else {
        downloadBtn.disabled = false;
        downloadBtn.title = 'Download log file (tmp/graph-debug.log)';
    }
}

// Initialize log panel event listeners
export function initLogPanel(): void {
    // Verbosity selector
    const verbositySelect = document.getElementById('verbosity-select') as HTMLSelectElement | null;
    if (verbositySelect) {
        verbositySelect.addEventListener('change', function(e: Event) {
            const target = e.target as HTMLSelectElement;
            const verbosity = parseInt(target.value);
            state.currentVerbosity = verbosity;

            sendMessage({
                type: 'set_verbosity',
                verbosity: verbosity
            });

            // Update download button state
            updateDownloadButton();
        });
    }

    // Log panel toggle
    const logHeader = document.getElementById('log-header') as HTMLElement | null;
    if (logHeader) {
        logHeader.addEventListener('click', function(e: Event) {
            const target = e.target as HTMLElement;
            // Don't toggle if clicking on buttons
            if (target.tagName === 'BUTTON' || target.tagName === 'SELECT') return;

            const panel = document.getElementById('log-panel') as HTMLElement | null;
            const toggleBtn = document.getElementById('toggle-logs') as HTMLElement | null;

            if (panel && toggleBtn) {
                panel.classList.toggle('collapsed');
                toggleBtn.textContent = panel.classList.contains('collapsed') ? '▲' : '▼';
            }
        });
    }
}