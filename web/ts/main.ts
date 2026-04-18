// Main entry point for QNTX web UI

import { listen } from '@tauri-apps/api/event';
import { connectWebSocket } from './websocket.ts';
import { initSystemDrawer, focusDrawerSearch } from './system-drawer.ts';
import { initGlobalKeyboard } from './keyboard.ts';
import { initCodeMirrorEditor } from './codemirror-editor.ts';
import { formatDateTime } from './html-utils.ts';
import { handleImportProgress, handleImportStats, handleImportComplete, initQueryFileDrop } from './file-upload.ts';
import { uiState } from './state/ui.ts';
import { appState } from './state/app.ts';
import { initUsageBadge, handleUsageUpdate } from './usage-badge.ts';
import { initSyncBadge } from './sync-badge.ts';
import { handleParseResponse } from './ats-semantic-tokens-client.ts';
import { handleDaemonStatus } from './websocket-handlers/daemon-status.ts';
import { statusIndicators } from './status-indicators.ts';
import {
    handlePulseExecutionStarted,
    handlePulseExecutionFailed,
    handlePulseExecutionCompleted,
    handlePulseExecutionLogStream
} from './pulse/realtime-handlers.ts';
import { handleStorageEviction } from './websocket-handlers/storage-eviction.ts';
import './symbol-palette.ts';
import { toggleConfig } from './config-panel.ts';
import { Window } from './components/window.ts';
// ai-provider-window.ts removed — LLM provider is now a tray glyph (llm-provider-glyph.ts)
// Note: Panel toggle functions are dynamically imported in Tauri event listeners below
// to avoid unused import warnings. Menu items use "show" events with dynamic imports,
// while keyboard shortcuts in individual panels use the toggle functions directly.
import './prose/panel.ts';
// plugin-panel.ts is now a glyph module registered via default-glyphs.ts
import { initDebugInterceptor } from './dev-debug-interceptor.ts';
import { glyphRun } from '@qntx/glyphs';
import { configureGlyphs } from '@qntx/glyphs';
import { canvasToScreen, screenToCanvas, getTransform } from './components/glyph/canvas/canvas-pan.ts';
import { registerDefaultGlyphs } from './default-glyphs.ts';
import { initialize as initQntxWasm } from './qntx-wasm.ts';
import { initStorage } from './indexeddb-storage.ts';
import { initVisualMode } from './visual-mode.ts';
import { log, SEG } from './logger.ts';

import type { MessageHandlers, VersionMessage } from '../types/websocket';

// Extend window interface for global functions
declare global {
    interface Window {
        logLoaderStep?: (message: string, isLoading?: boolean, isSubStep?: boolean) => void;
        hideLoadingScreen?: () => void;
        __TAURI__?: unknown;
    }
}

const _t0 = performance.now();
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

    // Update Self diagnostic glyph
    import('./default-glyphs.js').then(({ updateSelfVersion }) => {
        updateSelfVersion(data);
    });

    console.log('Server version:', data);
}


// Initialize the application
// WebSocket connects immediately — storage, WASM, and canvas sync run in parallel.
async function init(): Promise<void> {
    console.log('[TIMING] init() called:', (performance.now() - _t0).toFixed(0), 'ms');
    if (window.logLoaderStep) window.logLoaderStep('Initializing application...');

    // Status indicators must exist before WebSocket connects — the WS open handler
    // updates the connection indicator, which silently no-ops if init() hasn't run.
    statusIndicators.init();

    // Connect WebSocket FIRST — this is the critical transport and must not wait
    // on storage, WASM, or canvas sync which can take seconds (or 30s on timeout).
    if (window.logLoaderStep) window.logLoaderStep('Connecting to server...');

    const handlers: MessageHandlers = {
        'version': handleVersion,
        'import_progress': handleImportProgress,
        'import_stats': handleImportStats,
        'import_complete': handleImportComplete,
        'usage_update': handleUsageUpdate,
        'parse_response': handleParseResponse,
        'daemon_status': handleDaemonStatus,
        'pulse_execution_started': handlePulseExecutionStarted,
        'pulse_execution_failed': handlePulseExecutionFailed,
        'pulse_execution_completed': handlePulseExecutionCompleted,
        'pulse_execution_log_stream': handlePulseExecutionLogStream,
        'storage_eviction': handleStorageEviction,
    };

    connectWebSocket(handlers);

    // Initialize debug interceptor (dev mode only)
    try {
        await initDebugInterceptor();
    } catch (error: unknown) {
        console.error('[Init] Failed to initialize debug interceptor:', error);
    }

    // Initialize IndexedDB storage for UI state (canvas layouts, preferences)
    // CRITICAL: Must complete before UI state operations
    try {
        if (window.logLoaderStep) window.logLoaderStep('Initializing storage...', false, true);
        await initStorage();
    } catch (error: unknown) {
        console.error('[Init] Failed to initialize IndexedDB storage:', error);
        throw error; // Stop initialization - storage is critical
    }

    // Load persisted UI state from IndexedDB (must happen after initStorage())
    uiState.loadPersistedState();
    console.log('[TIMING] storage ready, state loaded:', (performance.now() - _t0).toFixed(0), 'ms');

    // WASM and canvas sync run in the background — neither is needed before the
    // canvas opens.  WASM powers search/attestation (user-initiated); canvas sync
    // reconciles with the backend (local IndexedDB state is already loaded above).
    initQntxWasm().catch(err => log.error(SEG.WASM, '[Init] WASM init failed:', err));

    (async () => {
        const { loadCanvasState, mergeCanvasState, upsertCanvasGlyph, upsertComposition, addMinimizedWindow } = await import('./api/canvas.ts');

        let backendReachable = false;
        try {
            const backendState = await Promise.race([
                loadCanvasState(),
                new Promise<never>((_, reject) =>
                    setTimeout(() => reject(new Error('canvas state fetch timed out after 3s')), 3000)
                ),
            ]);
            backendReachable = true;
            const local = {
                glyphs: uiState.getCanvasGlyphs(),
                compositions: uiState.getCanvasCompositions(),
                minimizedWindows: uiState.getMinimizedWindows(),
            };
            const merged = mergeCanvasState(local, backendState);

            if (merged.mergedGlyphs > 0) uiState.setCanvasGlyphs(merged.glyphs);
            if (merged.mergedComps > 0) uiState.setCanvasCompositions(merged.compositions);
            if (merged.mergedMinimized > 0) uiState.setMinimizedWindows(merged.minimizedWindows);

            if (merged.mergedGlyphs > 0 || merged.mergedComps > 0 || merged.mergedMinimized > 0) {
                log.info(SEG.GLYPH, `[Init] Merged ${merged.mergedGlyphs} glyphs, ${merged.mergedComps} compositions, ${merged.mergedMinimized} minimized windows from backend`);
            }
        } catch (error: unknown) {
            log.warn(SEG.GLYPH, '[Init] Failed to load canvas state from backend, continuing with local state:', error);
        }

        if (!backendReachable) {
            const localGlyphs = uiState.getCanvasGlyphs();
            const localCompositions = uiState.getCanvasCompositions();
            const localMinimized = uiState.getMinimizedWindows();
            for (const glyph of localGlyphs) upsertCanvasGlyph(glyph);
            for (const comp of localCompositions) upsertComposition(comp);
            for (const id of localMinimized) addMinimizedWindow(id);

            if (localGlyphs.length > 0 || localCompositions.length > 0 || localMinimized.length > 0) {
                log.info(SEG.GLYPH, `[Init] Backend unreachable, enqueued ${localGlyphs.length} glyphs, ${localCompositions.length} compositions, ${localMinimized.length} minimized windows for sync`);
            }
        }
    })().catch(err => log.warn(SEG.GLYPH, '[Init] Canvas sync failed:', err));

    // Restore previous session if exists
    const graphSession = uiState.getGraphSession();
    if (graphSession.query || graphSession.verbosity !== undefined) {
        if (window.logLoaderStep) window.logLoaderStep('Restoring session...', false, true);
        if (graphSession.verbosity !== undefined) {
            appState.currentVerbosity = graphSession.verbosity;
        }

        if (graphSession.query) {
            appState.currentQuery = graphSession.query;
        }
    }

    // Initialize visual mode system (connectivity-based styling)
    initVisualMode();

    // Initialize UI components
    if (window.logLoaderStep) window.logLoaderStep('Initializing system drawer...');
    initSystemDrawer();

    // Initialize CodeMirror editor (replaces textarea)
    if (window.logLoaderStep) window.logLoaderStep('Setting up editor...', false, true);
    initCodeMirrorEditor();

    // Wire @qntx/glyphs with QNTX's logger, persistence, canvas bridge, and stripHtml
    configureGlyphs({
        logger: log,
        logSegment: SEG.GLYPH,
        persistence: {
            getMinimizedGlyphs: () => uiState.getMinimizedWindows(),
            addMinimizedGlyph: (id) => uiState.addMinimizedWindow(id),
            removeMinimizedGlyph: (id) => uiState.removeMinimizedWindow(id),
        },
        canvas: {
            toScreen: canvasToScreen,
            fromScreen: screenToCanvas,
            getScale: (canvasId) => getTransform(canvasId).scale,
        },
        removeCanvasGlyph: (glyphId) => uiState.removeCanvasGlyph(glyphId),
        stripHtml: (html) => {
            const doc = new DOMParser().parseFromString(html, 'text/html');
            return doc.body.textContent ?? '';
        },
    });


    // Initialize glyph run FIRST (before any glyphs are created)
    // This ensures the run is ready to receive glyphs
    glyphRun.init();

    registerDefaultGlyphs();

    // Restore minimized glyphs from persisted state
    const minimizedIds = uiState.getMinimizedWindows();
    if (minimizedIds.length > 0) {
        const canvasGlyphs = uiState.getCanvasGlyphs();
        for (const id of minimizedIds) {
            if (glyphRun.has(id)) continue;

            const glyph = canvasGlyphs.find(g => g.id === id);
            if (!glyph || !glyph.content) {
                log.warn(SEG.GLYPH, `[Init] Minimized glyph ${id} has no stored content, skipping tray restore`);
                continue;
            }
            try {
                const parsed = JSON.parse(glyph.content);
                const result = parsed.result ?? parsed;
                const promptConfig = parsed.promptConfig;
                const prompt = parsed.prompt;
                const { renderResultContent } = await import('./components/glyph/result-glyph.ts');
                glyphRun.add({
                    id: glyph.id,
                    title: prompt || 'Result',
                    symbol: glyph.symbol || 'result',
                    renderContent: () => renderResultContent(result, parsed.tokens ?? [], promptConfig, prompt),
                    onClose: () => {
                        uiState.removeMinimizedWindow(id);
                        uiState.removeCanvasGlyph(id);
                        log.debug(SEG.GLYPH, `[Init] Closed restored tray glyph ${id}`);
                    },
                });
                log.debug(SEG.GLYPH, `[Init] Restored minimized glyph ${id} to tray`);
            } catch (err) {
                log.warn(SEG.GLYPH, `[Init] Failed to restore minimized glyph ${id}:`, err);
            }
        }
    }


    // Canvas is the primary workspace — open it immediately.
    // Plugin glyphs load in background; unknown types show placeholders that
    // auto-replace when the plugin becomes available (see renderGlyph retry).
    console.log('[TIMING] canvas opening:', (performance.now() - _t0).toFixed(0), 'ms');
    glyphRun.openGlyph('canvas-workspace');

    // Load plugin glyphs in background — non-blocking
    import('./components/glyph/plugin-provided-glyphs.ts')
        .then(({ loadPluginGlyphs }) => loadPluginGlyphs())
        .catch(err => log.warn(SEG.UI, '[Init] Failed to load plugin glyphs:', err));

    if (window.logLoaderStep) window.logLoaderStep('Setting up file upload...');
    initQueryFileDrop();

    if (window.logLoaderStep) window.logLoaderStep('Initializing UI controls...');
    initUsageBadge();
    initSyncBadge();

    // Listen for Tauri events (menu actions)
    if (typeof window.__TAURI__ !== 'undefined') {
        // Menu items always show (never toggle/hide)
        listen('show-config-panel', () => {
            import('./config-panel.ts').then(({ showConfig }) => {
                showConfig();
            });
        });

        // Kept for backwards compatibility - not used by menu system
        // Keyboard shortcut (Cmd+,) is in keyboard.ts
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

        listen('show-plugin-panel', () => {
            glyphRun.openGlyph('plugin-glyph');
        });

        listen('toggle-logs', () => {
            focusDrawerSearch();
        });

        listen('open-url', (event: any) => {
            // Open URL in default browser
            window.open(event.payload, '_blank');
        });
    }

    // Global keyboard shortcuts (SPACE → search, Cmd+, → config)
    initGlobalKeyboard();

    if (window.logLoaderStep) window.logLoaderStep('Finalizing startup...');
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