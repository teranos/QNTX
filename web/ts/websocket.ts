// WebSocket connection management

import type {
    WebSocketMessage,
    MessageHandlers,
    MessageHandler,
    BaseMessage,
    ReloadMessage,
    BackendStatusMessage,
    JobUpdateMessage,
    DaemonStatusMessage,
    LLMStreamMessage,
    StorageWarningMessage,
    PluginHealthMessage,
    SystemCapabilitiesMessage,
    DatabaseStatsMessage,
    RichSearchResultsMessage,
    WatcherMatchMessage,
    WatcherErrorMessage,
    GlyphFiredMessage,
} from '../types/websocket';
import { handleJobNotification, notifyStorageWarning, handleDaemonStatusNotification } from './tauri-notifications';
import { handlePluginHealth } from './websocket-handlers/plugin-health';
import { handleSystemCapabilities } from './websocket-handlers/system-capabilities';
import { log, SEG } from './logger';
import { connectivityManager } from './connectivity';
import { updateResultGlyphContent, type ExecutionResult } from './components/glyph/result-glyph';

let ws: WebSocket | null = null;
let reconnectTimer: ReturnType<typeof setTimeout> | null = null;
let messageHandlers: MessageHandlers = {};

/**
 * Find a result glyph melded below a parent glyph element.
 * Looks inside the parent's melded composition for a result glyph sibling.
 */
function findResultGlyphBelow(parentElement: HTMLElement): HTMLElement | null {
    const composition = parentElement.closest('.melded-composition');
    if (!composition) return null;
    return composition.querySelector('[data-glyph-symbol="result"]') as HTMLElement | null;
}

/**
 * Built-in message handlers for WebSocket messages
 * Maps message type to handler function with logging and side effects
 */
const MESSAGE_HANDLERS = {
    reload: (data: ReloadMessage) => {
        log.info(SEG.WS, 'Dev server triggered reload', data.reason);
        if (typeof window !== 'undefined') {
            window.location.reload();
        }
    },

    backend_status: (data: BackendStatusMessage) => {
        log.info(SEG.WS, `Backend status: ${data.status}`, data);
        // Could update UI to show backend health status
    },

    job_update: (data: JobUpdateMessage) => {
        log.info(SEG.PULSE, 'Job update:', data.job.id, data.job.status);

        // Send native desktop notification if in Tauri
        handleJobNotification({
            id: data.job.id,
            handler_name: data.job.handler_name,
            status: data.job.status,
            error: data.job.error
        });

        // Invoke registered handler
        messageHandlers['job_update']?.(data);
    },

    daemon_status: (data: DaemonStatusMessage) => {
        log.info(SEG.PULSE, 'Daemon status:',
            data.server_state || 'running',
            `${data.active_jobs} active`,
            `${data.queued_jobs} queued`,
            `${data.load_percent}% load`
        );

        // Send native desktop notification for server state changes
        handleDaemonStatusNotification({
            server_state: data.server_state,
            active_jobs: data.active_jobs,
            queued_jobs: data.queued_jobs,
        });

        // Invoke registered handler
        messageHandlers['daemon_status']?.(data);
    },

    llm_stream: (data: LLMStreamMessage) => {
        log.debug(SEG.PULSE, 'LLM stream:', data.job_id, data.content.length, 'chars', data.done ? '(done)' : '');
        messageHandlers['llm_stream']?.(data);
    },

    storage_warning: (data: StorageWarningMessage) => {
        log.warn(SEG.DB, 'Storage warning:', data.actor, `${(data.fill_percent * 100).toFixed(0)}%`);

        // Send native desktop notification if in Tauri
        notifyStorageWarning(data.actor, data.fill_percent);

        // Invoke registered handler
        messageHandlers['storage_warning']?.(data);
    },

    plugin_health: (data: PluginHealthMessage) => {
        log.info(SEG.PULSE, 'Plugin health:', data.name, data.state, data.healthy ? 'healthy' : 'unhealthy');

        // Handle toast notification and indicator update
        handlePluginHealth(data);

        // Invoke registered handler
        messageHandlers['plugin_health']?.(data);
    },

    system_capabilities: (data: SystemCapabilitiesMessage) => {
        log.info(SEG.CONFIG, 'System capabilities:', {
            fuzzy_backend: data.fuzzy_backend,
            fuzzy_optimized: data.fuzzy_optimized ? 'optimized' : 'fallback'
        });

        // Handle capability-based UI updates
        handleSystemCapabilities(data);

        // Invoke registered handler
        messageHandlers['system_capabilities']?.(data);
    },

    database_stats: (data: DatabaseStatsMessage) => {
        log.info(SEG.DB, 'Database stats:', {
            total_attestations: data.total_attestations,
            path: data.path
        });

        // Update database stats window with response
        import('./database-stats-window.js').then(({ databaseStatsWindow }) => {
            databaseStatsWindow.updateStats({
                path: data.path,
                storage_backend: data.storage_backend,
                storage_optimized: data.storage_optimized,
                storage_version: data.storage_version,
                total_attestations: data.total_attestations,
                unique_actors: data.unique_actors,
                unique_subjects: data.unique_subjects,
                unique_contexts: data.unique_contexts,
                rich_fields: data.rich_fields as (string[] | undefined)
            });
        });

        // Update database stats glyph
        import('./default-glyphs.js').then(({ updateDatabaseStats }) => {
            updateDatabaseStats(data);
        });

        // Update status indicator with total count
        import('./status-indicators.js').then(({ statusIndicators }) => {
            statusIndicators.handleDatabaseStats(data.total_attestations);
        });
    },

    rich_search_results: (data: RichSearchResultsMessage) => {
        log.info(SEG.QUERY, 'Rich search results:', data.total, 'matches');

        // Pass results to the CodeMirror editor's fuzzy search view
        import('./codemirror-editor.js').then(({ handleFuzzySearchResults }) => {
            handleFuzzySearchResults(data);
        });
    },

    watcher_match: (data: WatcherMatchMessage) => {
        log.debug(SEG.WS, 'Watcher match:', data.watcher_id, data.attestation?.id);

        // Extract glyph ID from watcher ID (format: "ax-glyph-{glyphId}")
        const watcherIdPrefix = 'ax-glyph-';
        if (data.watcher_id && data.watcher_id.startsWith(watcherIdPrefix)) {
            const glyphId = data.watcher_id.substring(watcherIdPrefix.length);

            // Update AX glyph with new result
            import('./components/glyph/ax-glyph.js').then(({ updateAxGlyphResults }) => {
                updateAxGlyphResults(glyphId, data.attestation);
            });
        } else {
            log.warn(SEG.WS, 'Received watcher_match with unexpected watcher_id format:', data.watcher_id);
        }

        // Invoke registered handler
        messageHandlers['watcher_match']?.(data);
    },

    glyph_fired: (data: GlyphFiredMessage) => {
        log.info(SEG.WS, 'Glyph fired:', data.glyph_id, data.status, data.error || '');

        // Apply execution state to target glyph element for CSS-driven visual feedback
        const el = document.querySelector(`[data-glyph-id="${CSS.escape(data.glyph_id)}"]`) as HTMLElement | null;
        if (el) {
            const stateMap: Record<string, string> = { started: 'running', success: 'completed', error: 'failed' };
            const state = stateMap[data.status] || data.status;
            el.dataset.executionState = state;

            // Auto-clear success state after 3s so border returns to default
            if (data.status === 'success') {
                setTimeout(() => {
                    if (el.dataset.executionState === 'completed') {
                        delete el.dataset.executionState;
                    }
                }, 3000);
            }

            // Update existing result glyph melded below (if one exists)
            if (data.result && (data.status === 'success' || data.status === 'error')) {
                const resultEl = findResultGlyphBelow(el);
                if (resultEl) {
                    try {
                        const result = JSON.parse(data.result) as ExecutionResult;
                        updateResultGlyphContent(resultEl, result);
                        log.debug(SEG.WS, 'Updated result glyph for', data.glyph_id);
                    } catch (e) {
                        log.error(SEG.WS, 'Failed to parse glyph_fired result:', e);
                    }
                }
            } else if (data.status === 'error' && data.error) {
                // Error without result payload — surface error text in existing result glyph
                const resultEl = findResultGlyphBelow(el);
                if (resultEl) {
                    const errorResult: ExecutionResult = {
                        success: false, stdout: '', stderr: '',
                        result: null, error: data.error, duration_ms: 0,
                    };
                    updateResultGlyphContent(resultEl, errorResult);
                    log.debug(SEG.WS, 'Updated result glyph with error for', data.glyph_id);
                }
            }
        } else {
            log.warn(SEG.WS, 'Glyph fired: no DOM element found for', data.glyph_id);
        }

        // Invoke registered handler
        messageHandlers['glyph_fired']?.(data);
    },

    watcher_error: (data: WatcherErrorMessage) => {
        log.warn(SEG.WS, 'Watcher error:', data.watcher_id, data.error, `(${data.severity})`);
        if (data.details?.length) {
            log.warn(SEG.WS, 'Watcher error details:', ...data.details);
        }

        // Extract glyph ID from watcher ID (format: "ax-glyph-{glyphId}")
        const watcherIdPrefix = 'ax-glyph-';
        if (data.watcher_id && data.watcher_id.startsWith(watcherIdPrefix)) {
            const glyphId = data.watcher_id.substring(watcherIdPrefix.length);

            // Update AX glyph with error message and details
            import('./components/glyph/ax-glyph.js').then(({ updateAxGlyphError }) => {
                updateAxGlyphError(glyphId, data.error, data.severity, data.details);
            });
        } else {
            log.warn(SEG.WS, 'Received watcher_error with unexpected watcher_id format:', data.watcher_id);
        }

        // Invoke registered handler
        messageHandlers['watcher_error']?.(data);
    }
} as const;

/**
 * Validate and sanitize backend URL
 * Virtue #12: Graceful Degradation - Invalid URLs return null instead of throwing
 * @param url - URL to validate
 * @returns Validated URL origin or null if invalid
 */
export function validateBackendURL(url: string): string | null {
    try {
        const parsed = new URL(url, window.location.origin);

        // Only allow http/https protocols (will be converted to ws/wss)
        if (!['http:', 'https:'].includes(parsed.protocol)) {
            return null;
        }

        return parsed.origin;
    } catch (error: unknown) {
        return null;
    }
}

/**
 * Route WebSocket message to appropriate handler
 * Checks built-in handlers first, then registered handlers, then default handler
 * @param data - The WebSocket message to route
 * @param registeredHandlers - Map of custom message handlers
 * @returns Whether the message was handled and by which handler type
 */
export function routeMessage(
    data: WebSocketMessage,
    registeredHandlers: MessageHandlers
): { handled: boolean; handlerType: 'builtin' | 'registered' | 'default' | 'none' } {
    // Look up built-in handler first
    const builtInHandler = MESSAGE_HANDLERS[data.type as keyof typeof MESSAGE_HANDLERS];
    if (builtInHandler) {
        builtInHandler(data as any);
        return { handled: true, handlerType: 'builtin' };
    }

    // Fall back to registered handlers for custom message types
    const registeredHandler = registeredHandlers[data.type as keyof MessageHandlers];
    if (registeredHandler) {
        (registeredHandler as MessageHandler)(data);
        return { handled: true, handlerType: 'registered' };
    }

    // Fall back to default handler for unknown types (e.g., graph data)
    // TODO(#209): Graph data should have explicit 'graph_data' type instead of using _default
    if (registeredHandlers['_default']) {
        registeredHandlers['_default'](data);
        return { handled: true, handlerType: 'default' };
    }

    return { handled: false, handlerType: 'none' };
}

/**
 * Initialize WebSocket connection
 * @param handlers - Map of message type to handler functions
 */
export function connectWebSocket(handlers: MessageHandlers): void {
    messageHandlers = handlers || {};

    // Use backend URL from injected global with validation
    const rawUrl = (window as any).__BACKEND_URL__ || window.location.origin;
    const validatedUrl = validateBackendURL(rawUrl);

    if (!validatedUrl) {
        log.error(SEG.WS, 'Invalid backend URL:', rawUrl);
        log.info(SEG.WS, 'Falling back to same-origin');
    }

    const backendUrl = validatedUrl || window.location.origin;
    const backendHost = backendUrl.replace(/^https?:\/\//, '');
    const protocol = backendUrl.startsWith('https') ? 'wss:' : 'ws:';
    const wsUrl = `${protocol}//${backendHost}/ws`;

    ws = new WebSocket(wsUrl);

    ws.onopen = function(): void {
        log.info(SEG.WS, 'WebSocket connected');
        updateConnectionStatus(true);

        // Request database stats on connect to populate DB indicator
        sendMessage({ type: 'get_database_stats' });
    };

    ws.onmessage = function(event: MessageEvent): void {
        const data = JSON.parse(event.data) as WebSocketMessage;

        // Debug: Log all WebSocket messages
        log.debug(SEG.WS, 'Message:', data.type, data);

        // Route message to appropriate handler
        const result = routeMessage(data, messageHandlers);

        // Warn if no handler was found
        if (!result.handled) {
            log.warn(SEG.WS, 'No handler for message type:', data.type);
        }
    };

    ws.onerror = function(error: Event): void {
        log.error(SEG.WS, 'WebSocket error:', error);
        updateConnectionStatus(false);
    };

    // Virtue #12: Graceful Degradation - Handle disconnection without crashing, auto-reconnect
    ws.onclose = function(event: CloseEvent): void {
        log.info(SEG.WS, 'WebSocket disconnected', {
            code: event.code,
            reason: event.reason || '(no reason)',
            wasClean: event.wasClean
        });
        updateConnectionStatus(false);
        // Clear any existing timer
        if (reconnectTimer) {
            clearTimeout(reconnectTimer);
        }
        // Reconnect after 3 seconds
        reconnectTimer = setTimeout(() => connectWebSocket(messageHandlers), 3000);
    };
}

/**
 * Update connection status in UI
 * @param connected - Whether the WebSocket is connected
 */
function updateConnectionStatus(connected: boolean): void {
    // Notify connectivity manager of WebSocket state change
    connectivityManager.setWebSocketConnected(connected);

    // Update status indicator using the new system
    import('./status-indicators.ts').then(({ statusIndicators }) => {
        statusIndicators.handleConnectionStatus(connected);
    });

    if (connected) {
        // Remove desaturation/dimming from entire UI
        document.body.classList.remove('disconnected');
        // Collapse system drawer when connected
        const systemDrawer = document.getElementById('system-drawer');
        systemDrawer?.classList.add('collapsed');
    } else {
        // Apply desaturation/dimming to entire UI
        document.body.classList.add('disconnected');
        // Expand system drawer when disconnected (useful for debugging)
        const systemDrawer = document.getElementById('system-drawer');
        systemDrawer?.classList.remove('collapsed');
    }
}

/**
 * Send message via WebSocket
 * @param message - Message to send
 * @returns True if message was sent, false if WebSocket is not ready
 */
export function sendMessage(message: BaseMessage | Record<string, unknown>): boolean {
    if (ws && ws.readyState === WebSocket.OPEN) {
        ws.send(JSON.stringify(message));
        return true;
    }
    return false;
}

/**
 * Get WebSocket ready state
 * @returns True if WebSocket is connected and ready
 */
export function isConnected(): boolean {
    return ws !== null && ws.readyState === WebSocket.OPEN;
}

/**
 * Register a message handler dynamically
 * Useful for components that initialize after WebSocket connection
 * @param type - Message type to handle (type-safe: must be a known message type)
 * @param handler - Handler function matching the message type
 */
export function registerHandler<K extends keyof MessageHandlers>(
    type: K,
    handler: NonNullable<MessageHandlers[K]>
): void {
    (messageHandlers as Record<string, MessageHandler>)[type] = handler as MessageHandler;
}

/**
 * Unregister a message handler
 * Should be called when components are destroyed/hidden to prevent handler leaks
 * @param type - Message type to unregister
 */
export function unregisterHandler<K extends keyof MessageHandlers>(type: K): void {
    delete (messageHandlers as Record<string, MessageHandler>)[type];
}

/**
 * Cleanup WebSocket connection
 * Called on page unload to prevent reconnection attempts
 */
export function cleanup(): void {
    if (reconnectTimer) {
        clearTimeout(reconnectTimer);
        reconnectTimer = null;
    }
    if (ws) {
        ws.close();
        ws = null;
    }
}