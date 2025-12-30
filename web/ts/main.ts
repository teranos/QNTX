// Main entry point for graph viewer

import { connectWebSocket } from './websocket.ts';
import { handleLogBatch, initLogPanel } from './log-panel.ts';
import { initCodeMirrorEditor } from './codemirror-editor.ts';
import { updateGraph, initGraphResize, setTransform } from './graph-renderer.ts';
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
import './symbol-palette.ts';
import './config-panel.ts';
import './ai-provider-panel.ts';
import './command-explorer-panel.ts';
import './hixtory-panel.ts';
import './prose/panel.ts';
import './theme.ts';

import type { MessageHandlers } from '../types/websocket';
import type { GraphData } from '../types/core';

// Extend window interface for global functions
declare global {
    interface Window {
        logLoaderStep?: (message: string, isLoading?: boolean, isSubStep?: boolean) => void;
        hideLoadingScreen?: () => void;
    }
}

// Version info interface
interface VersionInfo {
    version: string;
    commit: string;
    build_time?: string;
}

// TIMING: Track when main.js module starts executing
console.log('[TIMING] main.js module start:', Date.now() - performance.timing.navigationStart, 'ms');
if (window.logLoaderStep) window.logLoaderStep('Loading core modules...');

if (window.logLoaderStep) window.logLoaderStep('Core modules loaded');

// Handle version info from server
function handleVersion(data: VersionInfo): void {
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
        commitLink.href = `https://github.com/sbvh-nl/expgraph/commit/${data.commit}`;
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
function init(): void {
    // TIMING: Track when init() is called
    console.log('[TIMING] init() called:', Date.now() - performance.timing.navigationStart, 'ms');
    if (window.logLoaderStep) window.logLoaderStep('Initializing application...');

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
    console.log('[TIMING] Calling connectWebSocket():', Date.now() - performance.timing.navigationStart, 'ms');
    if (window.logLoaderStep) window.logLoaderStep('Connecting to server...');

    const handlers: MessageHandlers = {
        'version': handleVersion as any,
        'logs': handleLogBatch as any,
        'import_progress': handleImportProgress as any,
        'import_stats': handleImportStats as any,
        'import_complete': handleImportComplete as any,
        'usage_update': handleUsageUpdate as any,
        'parse_response': handleParseResponse as any,
        'daemon_status': handleDaemonStatus as any,
        'job_update': handleJobUpdate as any,
        'pulse_execution_started': handlePulseExecutionStarted as any,
        'pulse_execution_failed': handlePulseExecutionFailed as any,
        'pulse_execution_completed': handlePulseExecutionCompleted as any,
        'pulse_execution_log_stream': handlePulseExecutionLogStream as any,
        'storage_warning': handleStorageWarning as any,
        '_default': updateGraph as any  // Default handler for graph data
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