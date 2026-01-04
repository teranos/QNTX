// Main entry point for graph viewer

import { listen } from '@tauri-apps/api/event';
import { connectWebSocket } from './websocket.ts';
import { handleLogBatch, initLogPanel } from './log-panel.ts';
import { initCodeMirrorEditor } from './codemirror-editor.ts';
import { updateGraph, initGraphResize } from './graph-renderer.ts';
import { initLegendaToggles } from './legenda.ts';
import { handleImportProgress, handleImportStats, handleImportComplete, initQueryFileDrop } from './file-upload.ts';
import { restoreSession } from './state-manager.ts';
import { state } from './config.ts';
import { initUsageBadge, handleUsageUpdate } from './usage-badge.ts';
import { handleParseResponse } from './ats-semantic-tokens-client.ts';
import { handleJobUpdate } from './hixtory-panel.ts';
import { handleDaemonStatus } from './websocket-handlers/daemon-status.ts';
import {
    handlePulseExecutionStarted,
    handlePulseExecutionFailed,
    handlePulseExecutionCompleted,
    handlePulseExecutionLogStream
} from './pulse/realtime-handlers.ts';
import { handleStorageWarning } from './websocket-handlers/storage-warning.ts';
import { handleStorageEviction } from './websocket-handlers/storage-eviction.ts';
import './symbol-palette.ts';
import { toggleConfig } from './config-panel.ts';
import './ai-provider-panel.ts';
import './command-explorer-panel.ts';
// Note: Panel toggle functions are dynamically imported in Tauri event listeners below
// to avoid unused import warnings. Menu items use "show" events with dynamic imports,
// while keyboard shortcuts in individual panels use the toggle functions directly.
import './prose/panel.ts';
import './theme.ts';
import { initConsoleReporter } from './console-reporter.ts';

import type {
    MessageHandlers,
    VersionMessage,
    LogsMessage,
    ImportProgressMessage,
    ImportStatsMessage,
    ImportCompleteMessage,
    UsageUpdateMessage,
    ParseResponseMessage,
    DaemonStatusMessage,
    JobUpdateMessage,
    PulseExecutionStartedMessage,
    PulseExecutionFailedMessage,
    PulseExecutionCompletedMessage,
    PulseExecutionLogStreamMessage,
    StorageWarningMessage,
    StorageEvictionMessage,
    GraphDataMessage
} from '../types/websocket';

// Extend window interface for global functions
declare global {
    interface Window {
        logLoaderStep?: (message: string, isLoading?: boolean, isSubStep?: boolean) => void;
        hideLoadingScreen?: () => void;
        __TAURI__?: unknown;
        commandExplorerPanel?: { toggle: (mode: string) => void };
    }
}

// TIMING: Track when main.js module starts executing
const navStart = performance.timeOrigin || Date.now();
console.log('[TIMING] main.js module start:', Date.now() - navStart, 'ms');
if (window.logLoaderStep) window.logLoaderStep('Loading core modules...');

if (window.logLoaderStep) window.logLoaderStep('Core modules loaded');

// Handle version info from server
function handleVersion(data: VersionMessage): void {
    // Cache build info for error toasts
    import('./toast').then(({ cacheBuildInfo }) => {
        cacheBuildInfo(data);
    });

    const buildHash = document.getElementById('build-hash');
    if (buildHash && data.commit) {
        // Create clickable commit hash link with version
        const commitShort = data.commit.substring(0, 7);
        const versionText = data.version === 'dev' ? 'development build' : data.version;

        // Format build time if available
        let buildTimeText = '';
        if (data.build_time) {
            const buildDate = new Date(data.build_time);
            const dateStr = buildDate.toLocaleDateString('en-US', { month: 'short', day: 'numeric', year: 'numeric' });
            const timeStr = buildDate.toLocaleTimeString('en-US', { hour: '2-digit', minute: '2-digit', hour12: false });
            buildTimeText = ` · ${dateStr} ${timeStr}`;
        }

        // Build version info using DOM API for security
        buildHash.textContent = `${versionText} · `;

        const commitLink = document.createElement('a');
        commitLink.href = `https://github.com/teranos/QNTX/commit/${data.commit}`;
        commitLink.target = '_blank';
        commitLink.style.color = 'inherit';
        commitLink.style.textDecoration = 'none';
        commitLink.textContent = commitShort;

        buildHash.appendChild(commitLink);

        if (buildTimeText) {
            buildHash.appendChild(document.createTextNode(buildTimeText));
        }
    }

    // Also add subtle version to log panel
    const logVersion = document.getElementById('log-version');
    if (logVersion && data.commit) {
        logVersion.textContent = data.commit.substring(0, 7);
    }

    console.log('Server version:', data);
}


// Initialize the application
async function init(): Promise<void> {
    // TIMING: Track when init() is called
    console.log('[TIMING] init() called:', Date.now() - navStart, 'ms');
    if (window.logLoaderStep) window.logLoaderStep('Initializing application...');

    // Initialize console reporter (dev mode only)
    try {
        await initConsoleReporter();
    } catch (err) {
        console.error('[Init] Failed to initialize console reporter:', err);
        // Continue anyway - console reporting is not critical to app function
    }

    // Restore previous session if exists
    const session = restoreSession();
    if (session) {
        if (window.logLoaderStep) window.logLoaderStep('Restoring session...', false, true);
        // Restore verbosity
        if (session.verbosity !== undefined) {
            state.currentVerbosity = session.verbosity;
            const verbositySelect = document.getElementById('verbosity-select') as HTMLSelectElement | null;
            if (verbositySelect) {
                verbositySelect.value = session.verbosity.toString();
            }
        }

        // Restore query (will be re-run to get fresh graph data)
        if (session.query) {
            state.currentQuery = session.query;
        }
    }

    // Set up WebSocket with message handlers
    console.log('[TIMING] Calling connectWebSocket():', Date.now() - navStart, 'ms');
    if (window.logLoaderStep) window.logLoaderStep('Connecting to server...');

    const handlers: MessageHandlers = {
        'version': handleVersion,
        'logs': handleLogBatch,
        'import_progress': handleImportProgress,
        'import_stats': handleImportStats,
        'import_complete': handleImportComplete,
        'usage_update': handleUsageUpdate,
        'parse_response': handleParseResponse,
        'daemon_status': handleDaemonStatus,
        'job_update': handleJobUpdate,
        'pulse_execution_started': handlePulseExecutionStarted,
        'pulse_execution_failed': handlePulseExecutionFailed,
        'pulse_execution_completed': handlePulseExecutionCompleted,
        'pulse_execution_log_stream': handlePulseExecutionLogStream,
        'storage_warning': handleStorageWarning,
        'storage_eviction': handleStorageEviction,
        '_default': updateGraph
    };

    connectWebSocket(handlers);

    // Initialize UI components
    if (window.logLoaderStep) window.logLoaderStep('Initializing log panel...');
    initLogPanel();

    // Initialize CodeMirror editor (replaces textarea)
    if (window.logLoaderStep) window.logLoaderStep('Setting up editor...', false, true);
    initCodeMirrorEditor();

    if (window.logLoaderStep) window.logLoaderStep('Initializing graph...');
    initGraphResize();

    if (window.logLoaderStep) window.logLoaderStep('Setting up file upload...');
    initQueryFileDrop();

    if (window.logLoaderStep) window.logLoaderStep('Initializing UI controls...');
    initLegendaToggles(updateGraph);  // Pass renderGraph function for legenda callbacks
    initUsageBadge();

    // Listen for Tauri events (menu actions)
    if (typeof window.__TAURI__ !== 'undefined') {
        // Menu items always show (never toggle/hide)
        listen('show-config-panel', () => {
            import('./config-panel.ts').then(({ showConfig }) => {
                showConfig();
            });
        });

        // Kept for backwards compatibility - not used by menu system
        // Keyboard shortcuts use toggleConfig() directly (see Cmd+, handler below)
        listen('toggle-config-panel', () => {
            toggleConfig();
        });

        listen('toggle-pulse-daemon', () => {
            // TODO: Track daemon state to toggle between start/stop
            // For now, always send stop (pause)
            import('./websocket.ts').then(({ sendMessage }) => {
                sendMessage({
                    type: 'daemon_control',
                    action: 'stop'
                });
            });
        });

        // Panel show events from menu bar (menu items always show, never toggle)
        listen('show-pulse-panel', () => {
            import('./pulse-panel.ts').then(({ showPulsePanel }) => {
                showPulsePanel();
            });
        });

        listen('show-prose-panel', () => {
            import('./prose/panel.ts').then(({ showProsePanel }) => {
                showProsePanel();
            });
        });

        listen('show-code-panel', () => {
            import('./code/panel.ts').then(({ showGoEditor }) => {
                showGoEditor();
            });
        });

        listen('show-hixtory-panel', () => {
            import('./hixtory-panel.ts').then(({ showJobList }) => {
                showJobList();
            });
        });

        listen('refresh-graph', () => {
            // Trigger graph refresh
            import('./websocket.ts').then(({ sendMessage }) => {
                sendMessage({
                    type: 'query',
                    query: state.currentQuery || 'i'
                });
            });
        });

        listen('toggle-logs', () => {
            // Toggle log panel visibility
            const logPanel = document.getElementById('log-panel');
            if (logPanel) {
                logPanel.classList.toggle('collapsed');
            }
        });

        listen('open-url', (event: any) => {
            // Open URL in default browser
            window.open(event.payload, '_blank');
        });
    }

    // Register Cmd+, keyboard shortcut (standard macOS preferences shortcut)
    // Note: Keyboard shortcuts toggle (show/hide), while menu items always show (macOS convention)
    document.addEventListener('keydown', (e: KeyboardEvent) => {
        // Cmd+, on Mac, Ctrl+, on Windows/Linux
        if ((e.metaKey || e.ctrlKey) && e.key === ',') {
            e.preventDefault();
            toggleConfig();
        }
    });

    if (window.logLoaderStep) window.logLoaderStep('Finalizing startup...');

    // NOTE: We don't restore cached graph data because D3 object references
    // don't serialize properly (causes isolated node detection bugs).
    // Instead, if there's a saved query, the user can re-run it manually.
}

// Start application when DOM is ready
if (document.readyState === 'loading') {
    document.addEventListener('DOMContentLoaded', () => {
        init();
        // Hide loading screen once app is initialized
        if (window.hideLoadingScreen) window.hideLoadingScreen();
    });
} else {
    init();
    // Hide loading screen once app is initialized
    if (window.hideLoadingScreen) window.hideLoadingScreen();
}

// Make this a module
export {};