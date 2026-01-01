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
    StorageWarningMessage
} from '../types/websocket';
import { handleJobNotification, notifyStorageWarning, handleDaemonStatusNotification } from './tauri-notifications';

let ws: WebSocket | null = null;
let reconnectTimer: ReturnType<typeof setTimeout> | null = null;
let messageHandlers: MessageHandlers = {};

/**
 * Validate and sanitize backend URL
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
    } catch (e) {
        return null;
    }
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
        console.error('Invalid backend URL:', rawUrl);
        console.log('Falling back to same-origin');
    }

    const backendUrl = validatedUrl || window.location.origin;
    const backendHost = backendUrl.replace(/^https?:\/\//, '');
    const protocol = backendUrl.startsWith('https') ? 'wss:' : 'ws:';
    const wsUrl = `${protocol}//${backendHost}/ws`;

    ws = new WebSocket(wsUrl);

    ws.onopen = function(): void {
        console.log('WebSocket connected');
        updateConnectionStatus(true);
    };

    ws.onmessage = function(event: MessageEvent): void {
        const data = JSON.parse(event.data) as WebSocketMessage;

        // Debug: Log all WebSocket messages
        console.log('📨 WS message:', data.type, data);

        // Handle reload message from dev server
        if (data.type === 'reload') {
            const reloadData = data as ReloadMessage;
            console.log('🔄 Dev server triggered reload', reloadData.reason);
            window.location.reload();  // Force hard reload
            return;
        }

        // Handle backend status updates from dev server
        if (data.type === 'backend_status') {
            const statusData = data as BackendStatusMessage;
            console.log(`📡 Backend status: ${statusData.status}`, statusData);
            // Could update UI to show backend health status
            return;
        }


        // Handle async job updates
        // Backend broadcasts job updates for running IX operations
        // Handler registered in main.js -> job-list-panel.js displays job hierarchy
        if (data.type === 'job_update') {
            const jobData = data as JobUpdateMessage;
            console.log('📦 Job update:', jobData.job.id, jobData.job.status);

            // Send native desktop notification if in Tauri
            if (jobData.job) {
                handleJobNotification({
                    id: jobData.job.id,
                    handler_name: jobData.job.handler_name,
                    status: jobData.job.status,
                    error: jobData.job.error
                });
            }

            const handler = messageHandlers['job_update'];
            if (handler) {
                handler(jobData);
            }
            return;
        }

        // Handle daemon status updates (IMPLEMENTED)
        // Backend broadcasts daemon status every 5 seconds
        // Handler registered in main.js -> daemon-indicator.js displays animated status
        if (data.type === 'daemon_status') {
            const daemonData = data as DaemonStatusMessage;
            console.log(
                '⚙️  Daemon status:',
                daemonData.server_state || 'running',
                `${daemonData.active_jobs} active`,
                `${daemonData.queued_jobs} queued`,
                `${daemonData.load_percent}% load`
            );

            // Send native desktop notification for server state changes (e.g., draining)
            handleDaemonStatusNotification({
                server_state: daemonData.server_state,
                active_jobs: daemonData.active_jobs,
                queued_jobs: daemonData.queued_jobs,
            });

            const handler = messageHandlers['daemon_status'];
            if (handler) {
                handler(daemonData);
            }
            return;
        }

        // Handle LLM streaming output (IMPLEMENTED)
        // Backend broadcasts token-by-token LLM output
        // Handler registered in main.js -> job-list-panel.js displays live stream
        if (data.type === 'llm_stream') {
            const llmData = data as LLMStreamMessage;
            console.log('🤖 LLM stream:', llmData.job_id, llmData.content.length, 'chars', llmData.done ? '(done)' : '');
            const handler = messageHandlers['llm_stream'];
            if (handler) {
                handler(llmData);
            }
            return;
        }

        // Handle storage warning messages
        // Backend broadcasts when storage limits are approaching
        if (data.type === 'storage_warning') {
            const warningData = data as StorageWarningMessage;
            console.log('⚠️ Storage warning:', warningData.actor, `${(warningData.fill_percent * 100).toFixed(0)}%`);

            // Send native desktop notification if in Tauri
            notifyStorageWarning(warningData.actor, warningData.fill_percent);

            const handler = messageHandlers['storage_warning'];
            if (handler) {
                handler(warningData);
            }
            return;
        }

        // Route message to appropriate handler
        const handler = messageHandlers[data.type as keyof MessageHandlers];
        if (handler) {
            (handler as MessageHandler)(data);
        } else if (messageHandlers['_default']) {
            // Fall back to default handler for unknown types (e.g., graph data)
            messageHandlers['_default'](data);
        } else {
            console.warn('⚠️  No handler for message type:', data.type);
        }
    };

    ws.onerror = function(error: Event): void {
        console.error('WebSocket error:', error);
        updateConnectionStatus(false);
    };

    ws.onclose = function(): void {
        console.log('WebSocket disconnected');
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
    const statusText = document.getElementById('status-text') as HTMLElement | null;
    const logPanel = document.getElementById('log-panel') as HTMLElement | null;

    if (statusText) {
        statusText.textContent = connected ? 'Connected' : 'Disconnected - Reconnecting...';
    }

    if (connected) {
        // Remove desaturation/dimming from entire UI
        document.body.classList.remove('disconnected');
        // Collapse log panel when connected
        logPanel?.classList.add('collapsed');
    } else {
        // Apply desaturation/dimming to entire UI
        document.body.classList.add('disconnected');
        // Expand log panel when disconnected (useful for debugging)
        logPanel?.classList.remove('collapsed');
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