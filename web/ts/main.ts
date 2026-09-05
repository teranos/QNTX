// Main entry point for QNTX web UI

// Before anything else can fail: whether what the logger writes leaves the
// tab, and under which release. Off when the build was handed no DSN.
import { leave } from './leave';
const leaving = leave(window as unknown as Parameters<typeof leave>[0]);

import { listen } from '@tauri-apps/api/event';

/**
 * Tauri listeners live for the app's lifetime — the unlisten fn is
 * deliberately dropped; a registration failure is not.
 */
function listenOrSay(...args: Parameters<typeof listen>): void {
    listen(...args).catch((err: unknown) => {
        log.error(SEG.UI, `[Init] Tauri listener '${String(args[0])}' failed to register:`, err);
    });
}
import { connectWebSocket, backendUrl } from './client';
import { askHealth, isLive, statedPlainly } from './liveness';
import { setupState, claimNode } from './setup.ts';
import { signedIn, openDoor } from './signin.ts';
import { relayed, doorStand, showDoor, stricken, say } from './door.ts';
import { initSystemDrawer, focusDrawerSearch } from './system-drawer.ts';
import { initNamespacesBar } from './namespaces-bar.ts';
import { initGlobalKeyboard } from './keyboard.ts';
import { formatDateTime } from './html-utils.ts';
import { handleImportProgress, handleImportStats, handleImportComplete, initQueryFileDrop } from './file-upload.ts';
import { uiState } from './state/ui.ts';
import { appState } from './state/app.ts';
import { initUsageBadge, handleUsageUpdate } from './usage-badge.ts';
import { initSyncBadge } from './sync-badge.ts';
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
// plugin-panel.ts is now a glyph module registered via default-glyphs.ts
import { initDebugInterceptor } from './dev-debug-interceptor.ts';
import { glyphRun } from '@qntx/glyphs';
import { configureGlyphs } from '@qntx/glyphs';
import { canvasToScreen, screenToCanvas, getTransform } from './components/glyph/canvas/canvas-pan.ts';
import { isGlyphSelected, getSelectedGlyphIds } from './components/glyph/canvas/selection.ts';
import { addComposition, removeComposition, findCompositionByGlyph } from './state/compositions.ts';
import { canvasSyncQueue } from './api/canvas-sync.ts';
import { registerDefaultGlyphs } from './default-glyphs.ts';
import { initialize as initQntxWasm } from './ats-wasm.ts';
import { initialize as initLaye } from './laye.ts';
import { installCopyable } from './copyable.ts';
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
    import('./toast')
        .then(({ cacheBuildInfo }) => {
            cacheBuildInfo(data);
        })
        .catch((err: unknown) => log.error(SEG.UI, 'Build info never reached the toast module:', err));

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
    import('./default-glyphs.js')
        .then(({ updateSelfVersion }) => {
            updateSelfVersion(data);
        })
        .catch((err: unknown) => log.error(SEG.UI, 'Server version never reached the Self glyph:', err));

    console.log('Server version:', data);
}


/**
 * Resting dot size for this device.
 *
 * Tablet dots are the largest: browsing the tray is a thumb slide, and the dot
 * must be hittable. Phones sit between tablet and desktop. Breakpoints match the
 * ones in web/css/glyph/states/dot.css.
 */
function restingDotSize(): { minWidth: number; minHeight: number } {
    if (window.matchMedia('(max-width: 768px)').matches) return { minWidth: 13, minHeight: 13 };
    if (window.matchMedia('(max-width: 900px)').matches) return { minWidth: 15, minHeight: 15 };
    return { minWidth: 10, minHeight: 10 };
}

// Initialize the application
// WebSocket connects immediately — storage, WASM, and canvas sync run in parallel.
async function init(): Promise<void> {
    console.log('[TIMING] init() called:', (performance.now() - _t0).toFixed(0), 'ms');
    // Said on the first line, as the node says it: logs leaving is something
    // the person is told. This line is itself the first that leaves.
    if (leaving) log.info(SEG.UI, 'Logs leave this tab', { release: leaving.release, to: window.location.hostname });
    if (window.logLoaderStep) window.logLoaderStep('Asking the node whether it is running...');

    // A node that cannot read its operational store cannot function, and a UI
    // that loads anyway offers a login to a system that is not there. The
    // loader is already the scrim; the answer is to stay behind it.
    const reached = await askHealth(backendUrl() + '/health');
    if (!isLive(reached)) {
        const said = statedPlainly(reached);
        for (const line of said) {
            if (window.logLoaderStep) window.logLoaderStep(line, true);
        }
        // A node that will not answer is refused the same way a login is: the
        // door stands, and it is red. The scrim lifts onto it.
        doorStand();
        showDoor();
        stricken();
        say(`${backendUrl()} doesn't respond`, true);
        if (window.hideLoadingScreen) window.hideLoadingScreen();
        return;
    }

    // The indicator rail is part of the panel, so it is built before the panel
    // is shown rather than after. It reads connectivity, which is true whether
    // or not the node knows who you are.
    statusIndicators.init();

    // A node nobody owns is not an auth state, so no auth glyph opens for it.
    // The scrim lifts onto the door instead, and the app starts after it rather
    // than behind it (ADR-033).
    //
    // A node that will not say whether it has an owner is not an unclaimed
    // node. Same posture as /health above — stay behind the loader and say why.
    let owned;
    let holdsSession = false;
    try {
        owned = await setupState();
        if (owned.claimed) {
            holdsSession = await signedIn();
        }
    } catch (err) {
        if (window.logLoaderStep) {
            window.logLoaderStep(err instanceof Error ? err.message : String(err), true);
        }
        return;
    }
    // A claimed node is the only one with a door to stand at, and it says
    // nothing about how it is configured — so being claimed is the question.
    if (owned.governed && !owned.claimed) {
        await claimNode(owned);
    } else if (owned.claimed && (!holdsSession || relayed())) {
        // Relayed, the session is the dev server's rather than this browser's.
        // Walking straight in on someone else's credential without the door
        // ever standing is the one case where being let in says nothing.
        await openDoor();
    }

    if (window.logLoaderStep) window.logLoaderStep('Initializing application...');

    // Connect WebSocket FIRST — this is the critical transport and must not wait
    // on storage, WASM, or canvas sync which can take seconds (or 30s on timeout).
    if (window.logLoaderStep) window.logLoaderStep('Connecting to server...');

    const handlers: MessageHandlers = {
        'version': handleVersion,
        'import_progress': handleImportProgress,
        'import_stats': handleImportStats,
        'import_complete': handleImportComplete,
        'usage_update': handleUsageUpdate,
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
    initLaye().catch(err => log.error(SEG.WASM, '[Init] laye init failed:', err));
    installCopyable();

    (async () => {
        const { loadCanvasState, mergeCanvasState, upsertCanvasGlyph, upsertComposition, addMinimizedWindow } = await import('./api/canvas.ts');

        let backendReachable = false;
        try {
            const backendState = await Promise.race([
                loadCanvasState(),
                new Promise<never>((_, reject) =>
                    setTimeout(() => reject(new Error('canvas state fetch timed out after 8s')), 8000)
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
    // Root only, and the node is what says so — it answers 403 below SUPER and
    // 501 where namespaces do not exist, so no bar is grown either way.
    initNamespacesBar();

    if (window.logLoaderStep) window.logLoaderStep('Setting up editor...', false, true);

    // Wire @qntx/glyphs with QNTX's logger, persistence, and canvas bridge
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
        canvasHost: {
            saveCanvasGlyph: (glyph) => uiState.addCanvasGlyph(glyph),
            getCanvasGlyphs: (canvasId) => uiState.getCanvasGlyphs(canvasId),
            getTransform: (canvasId) => getTransform(canvasId),
            getSelectedGlyphIds: (canvasId) => getSelectedGlyphIds(canvasId),
            isGlyphSelected: (canvasId, glyphId) => isGlyphSelected(canvasId, glyphId),
            saveComposition: (composition) => addComposition(composition),
            removeComposition: (id) => removeComposition(id),
            findCompositionByGlyph: (glyphId) => findCompositionByGlyph(glyphId),
            flushSync: () => canvasSyncQueue.flush(),
        },
        // Touch devices get a bigger resting dot so it stays findable with a thumb.
        // This used to live in @media rules in web/css/glyph/states/dot.css, where it
        // was overwritten by the inline size the proximity engine writes every frame.
        dotGeometry: restingDotSize(),
    });


    // Initialize glyph run FIRST (before any glyphs are created)
    // This ensures the run is ready to receive glyphs
    glyphRun.init();

    registerDefaultGlyphs();

    // Restore minimized glyphs from persisted state
    const minimizedIds = uiState.getMinimizedWindows();
    if (minimizedIds.length > 0) {
        for (const id of minimizedIds) {
            if (glyphRun.has(id)) continue;

            const glyph = uiState.getCanvasGlyph(id);
            if (!glyph || !glyph.content) {
                log.debug(SEG.GLYPH, `[Init] Removing stale minimized glyph ${id} - no stored content`);
                uiState.removeMinimizedWindow(id);
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

    // The brow — the node's status line around the Dynamic Island. Mounts
    // only where the shell granted the unsafe headroom (or ?brow to preview).
    import('./brow.ts')
        .then(({ initBrow }) => initBrow())
        .catch(err => log.warn(SEG.UI, '[Init] Brow failed to mount:', err));

    // Listen for Tauri events (menu actions)
    if (typeof window.__TAURI__ !== 'undefined') {
        // Menu items always show (never toggle/hide)
        listenOrSay('show-config-panel', () => {
            import('./config-panel.ts')
                .then(({ showConfig }) => showConfig())
                .catch((err: unknown) => log.error(SEG.UI, 'Config panel failed to open:', err));
        });

        // Kept for backwards compatibility - not used by menu system
        // Keyboard shortcut (Cmd+,) is in keyboard.ts
        listenOrSay('toggle-config-panel', () => {
            toggleConfig();
        });

        listenOrSay('toggle-pulse-daemon', () => {
            // TODO: Track daemon state to toggle between start/stop
            // For now, always send stop (pause)
            import('./client')
                .then(({ sendMessage }) => sendMessage({ type: 'daemon_control', action: 'stop' }))
                .catch((err: unknown) => log.error(SEG.UI, 'daemon_control stop never sent:', err));
        });

        // Panel show events from menu bar (menu items always show, never toggle)
        listenOrSay('show-pulse-panel', () => {
            glyphRun.openGlyph('pulse-glyph');
        });

        listenOrSay('show-plugin-panel', () => {
            glyphRun.openGlyph('plugin-glyph');
        });

        listenOrSay('show-handlers-panel', () => {
            glyphRun.openGlyph('handlers-glyph');
        });

        listenOrSay('toggle-logs', () => {
            focusDrawerSearch();
        });

        listenOrSay('open-url', (event: any) => {
            // Open URL in default browser
            window.open(event.payload, '_blank');
        });
    }

    // Global keyboard shortcuts (SPACE → search, Cmd+, → config)
    initGlobalKeyboard();

    if (window.logLoaderStep) window.logLoaderStep('Finalizing startup...');

    // The scrim comes down here, where init got all the way through. Every
    // return above it is a node that cannot be reached or does not know you,
    // and those stay behind it.
    if (window.hideLoadingScreen) window.hideLoadingScreen();
    Window.finishWindowRestore();
}

// The backstop under every promise nothing awaits: a rejection that reaches
// here was dropped by its caller, and the log is its last chance to exist.
window.addEventListener('unhandledrejection', (event) => {
    log.error(SEG.UI, 'Unhandled promise rejection:', event.reason);
});

// Start application when DOM is ready
// Virtue #8: Progressive Enhancement - Core init works immediately, enhanced features layer on
if (document.readyState === 'loading') {
    document.addEventListener('DOMContentLoaded', () => { void init(); });
} else {
    void init();
}

// Make this a module
export {};