// Main entry point for graph viewer

import { listen } from '@tauri-apps/api/event';
import { connectWebSocket } from './websocket.ts';
import { handleLogBatch, initSystemDrawer } from './system-drawer.ts';
import { initCodeMirrorEditor } from './codemirror-editor.ts';
import { CSS } from './css-classes.ts';
import { updateGraph, initGraphResize } from './graph-renderer.ts';
import { initLegendaToggles } from './legenda.ts';
import { handleImportProgress, handleImportStats, handleImportComplete, initQueryFileDrop } from './file-upload.ts';
import { restoreSession } from './state-manager.ts';
import { state } from './config.ts';
import { initUsageBadge, handleUsageUpdate } from './usage-badge.ts';
import { handleParseResponse } from './ats-semantic-tokens-client.ts';
import { handleJobUpdate } from './hixtory-panel.ts';
import { handleDaemonStatus } from './websocket-handlers/daemon-status.ts';
import { statusIndicators } from './status-indicators.ts';
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
import './plugin-panel.ts';
import './webscraper-panel.ts';
import { initConsoleReporter } from './console-reporter.ts';

import type { MessageHandlers, VersionMessage } from '../types/websocket';

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

// Handle webscraper response from server
async function handleWebscraperResponse(data: any): Promise<void> {
    const { webscraperPanel } = await import('./webscraper-panel.js');
    webscraperPanel.handleScraperResponse(data);
}

// Handle webscraper progress updates
async function handleWebscraperProgress(data: any): Promise<void> {
    const { webscraperPanel } = await import('./webscraper-panel.js');
    webscraperPanel.handleScraperProgress(data);
}

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
        commitLink.classList.add('u-color-inherit', 'u-no-underline');
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

    // Note: Some handlers use internal types that differ from websocket message types.
    // Using 'unknown' intermediate cast where types don't overlap sufficiently.
    // TODO: Align handler signatures with MessageHandlers interface.
    const handlers: MessageHandlers = {
        'version': handleVersion,
        'logs': handleLogBatch as unknown as MessageHandlers['logs'],
        'import_progress': handleImportProgress as MessageHandlers['import_progress'],
        'import_stats': handleImportStats as MessageHandlers['import_stats'],
        'import_complete': handleImportComplete as MessageHandlers['import_complete'],
        'usage_update': handleUsageUpdate as unknown as MessageHandlers['usage_update'],
        'parse_response': handleParseResponse as MessageHandlers['parse_response'],
        'daemon_status': handleDaemonStatus as MessageHandlers['daemon_status'],
        'job_update': handleJobUpdate as MessageHandlers['job_update'],
        'pulse_execution_started': handlePulseExecutionStarted as MessageHandlers['pulse_execution_started'],
        'pulse_execution_failed': handlePulseExecutionFailed as MessageHandlers['pulse_execution_failed'],
        'pulse_execution_completed': handlePulseExecutionCompleted as MessageHandlers['pulse_execution_completed'],
        'pulse_execution_log_stream': handlePulseExecutionLogStream as MessageHandlers['pulse_execution_log_stream'],
        'storage_warning': handleStorageWarning as MessageHandlers['storage_warning'],
        'storage_eviction': handleStorageEviction as MessageHandlers['storage_eviction'],
        'webscraper_response': handleWebscraperResponse as MessageHandlers['webscraper_response'],
        'webscraper_progress': handleWebscraperProgress as MessageHandlers['webscraper_progress'],
        '_default': updateGraph as unknown as MessageHandlers['_default']
    };

    connectWebSocket(handlers);

    // Initialize UI components
    if (window.logLoaderStep) window.logLoaderStep('Initializing system drawer...');
    initSystemDrawer();

    // Initialize status indicators (connection, pulse daemon, etc.)
    statusIndicators.init();

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

        listen('show-plugin-panel', () => {
            import('./plugin-panel.ts').then(({ showPluginPanel }) => {
                showPluginPanel();
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
            // Toggle system drawer visibility
            const systemDrawer = document.getElementById('system-drawer');
            if (systemDrawer) {
                const isCollapsed = systemDrawer.classList.contains(CSS.STATE.COLLAPSED);
                if (isCollapsed) {
                    systemDrawer.classList.remove(CSS.STATE.COLLAPSED);
                } else {
                    systemDrawer.classList.add(CSS.STATE.COLLAPSED);
                }
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