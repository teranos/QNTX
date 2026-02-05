// Main entry point for graph viewer

import { listen } from '@tauri-apps/api/event';
import { connectWebSocket } from './websocket.ts';
import { handleLogBatch, initSystemDrawer } from './system-drawer.ts';
import { initCodeMirrorEditor } from './codemirror-editor.ts';
import { CSS } from './css-classes.ts';
import { formatDateTime } from './html-utils.ts';
import { updateGraph, initGraphResize } from './graph/index.ts';
import { initTypeAttestations } from './components/type-attestations.ts';
import { handleImportProgress, handleImportStats, handleImportComplete, initQueryFileDrop } from './file-upload.ts';
import { uiState } from './state/ui.ts';
import { appState } from './state/app.ts';
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
import { Window } from './components/window.ts';
import './ai-provider-window.ts';
import './command-explorer-panel.ts';
// Note: Panel toggle functions are dynamically imported in Tauri event listeners below
// to avoid unused import warnings. Menu items use "show" events with dynamic imports,
// while keyboard shortcuts in individual panels use the toggle functions directly.
import './prose/panel.ts';
import './plugin-panel.ts';
import { initDebugInterceptor } from './dev-debug-interceptor.ts';
import { glyphRun } from './components/glyph/run.ts';
import { registerTestGlyphs } from './test-glyphs.ts';
import { initialize as initQntxWasm } from './qntx-wasm.ts';
import { initStorage } from './indexeddb-storage.ts';
import { showToast } from './toast.ts';

import type { MessageHandlers, VersionMessage, BaseMessage } from '../types/websocket';
import type { GraphData } from '../types/core';

// Type guard to check if data is graph data (has nodes and links arrays)
// TODO(#209): Remove this type guard once backend sends explicit 'graph_data' message type
function isGraphData(data: GraphData | BaseMessage): data is GraphData {
    return 'nodes' in data && 'links' in data && Array.isArray((data as GraphData).nodes);
}

// Wrapper for default handler that type-guards graph data
// TODO(#209): Replace _default handler with explicit 'graph_data' handler
function handleDefaultMessage(data: GraphData | BaseMessage): void {
    if (isGraphData(data)) {
        updateGraph(data);
    } else {
        console.warn('Received non-graph message without handler:', data);
    }
}

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
            buildTimeText = ` · ${formatDateTime(data.build_time)}`;
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

    // Also add subtle version to system drawer
    const logVersion = document.getElementById('system-version');
    if (logVersion && data.commit) {
        logVersion.textContent = data.commit.substring(0, 7);
    }

    // Update Self diagnostic window if loaded
    import('./self-window.js').then(({ selfWindow }) => {
        selfWindow.updateVersion(data);
    });

    console.log('Server version:', data);
}


// Initialize the application
// Avoid Sin #4: Callback Hell - Use async/await for sequential async operations
async function init(): Promise<void> {
    // TIMING: Track when init() is called
    console.log('[TIMING] init() called:', Date.now() - navStart, 'ms');
    if (window.logLoaderStep) window.logLoaderStep('Initializing application...');

    // Initialize debug interceptor (dev mode only)
    // Avoid Sin #7: Silent Failures - Log errors even for non-critical components
    try {
        await initDebugInterceptor();
    } catch (error: unknown) {
        console.error('[Init] Failed to initialize debug interceptor:', error);
        // Continue anyway - debug interception is not critical to app function
    }

    // Initialize IndexedDB storage for UI state (canvas layouts, preferences)
    // CRITICAL: Must complete before UI state operations
    try {
        if (window.logLoaderStep) window.logLoaderStep('Initializing storage...', false, true);
        await initStorage();
    } catch (error: unknown) {
        console.error('[Init] Failed to initialize IndexedDB storage:', error);
        // BLOCK: Canvas state persistence unavailable
        // TODO: Show user notification that canvas state won't persist
        throw error; // Stop initialization - storage is critical
    }

    // Load persisted UI state from IndexedDB (must happen after initStorage())
    uiState.loadPersistedState();

    // Initialize QNTX WASM module with IndexedDB storage
    try {
        if (window.logLoaderStep) window.logLoaderStep('Initializing WASM + IndexedDB...', false, true);
        await initQntxWasm();
    } catch (error: unknown) {
        console.error('[Init] Failed to initialize QNTX WASM:', error);
        showToast('WASM storage unavailable - local attestation caching disabled', {
            type: 'warning',
            duration: 6000
        });
        // Continue anyway - WASM storage is not critical for basic graph viewing
    }

    // Restore previous session if exists
    const graphSession = uiState.getGraphSession();
    if (graphSession.query || graphSession.verbosity !== undefined) {
        if (window.logLoaderStep) window.logLoaderStep('Restoring session...', false, true);
        // Restore verbosity
        if (graphSession.verbosity !== undefined) {
            appState.currentVerbosity = graphSession.verbosity;
            const verbositySelect = document.getElementById('verbosity-select') as HTMLSelectElement | null;
            if (verbositySelect) {
                verbositySelect.value = graphSession.verbosity.toString();
            }
        }

        // Restore query (will be re-run to get fresh graph data)
        if (graphSession.query) {
            appState.currentQuery = graphSession.query;
        }
    }

    // Set up WebSocket with message handlers
    console.log('[TIMING] Calling connectWebSocket():', Date.now() - navStart, 'ms');
    if (window.logLoaderStep) window.logLoaderStep('Connecting to server...');

    // Message handlers are now properly typed to match their WebSocket message types
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
        '_default': handleDefaultMessage
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

    // Initialize glyph run FIRST (before any glyphs are created)
    // This ensures the run is ready to receive glyphs
    glyphRun.init();

    // Register test glyphs to demonstrate the morphing behavior
    registerTestGlyphs();

    if (window.logLoaderStep) window.logLoaderStep('Setting up file upload...');
    initQueryFileDrop();

    if (window.logLoaderStep) window.logLoaderStep('Initializing UI controls...');
    initTypeAttestations(updateGraph);  // Pass renderGraph function for type attestation callbacks
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
                    query: appState.currentQuery || 'i'
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
// Virtue #8: Progressive Enhancement - Core init works immediately, enhanced features layer on
if (document.readyState === 'loading') {
    document.addEventListener('DOMContentLoaded', () => {
        init();
        // Hide loading screen once app is initialized
        if (window.hideLoadingScreen) window.hideLoadingScreen();
        // Restore window visibility after loading screen completes
        Window.finishWindowRestore();
    });
} else {
    init();
    // Hide loading screen once app is initialized
    if (window.hideLoadingScreen) window.hideLoadingScreen();
    // Restore window visibility after loading screen completes
    Window.finishWindowRestore();
}

// Make this a module
export {};