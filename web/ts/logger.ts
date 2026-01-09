/**
 * Centralized Logging Utility
 *
 * Provides consistent, leveled logging across the QNTX web UI.
 * Uses SEG symbols for visual categorization in console output.
 *
 * Features:
 * - Log levels: debug < info < warn < error
 * - Auto-prefixes with component/SEG identifiers
 * - Production mode silences debug/info logs
 * - Preserves source location in browser dev tools
 *
 * Usage:
 *   import { log } from './logger';
 *   log.debug('꩜', 'Pulse job started', { jobId: 123 });
 *   log.info('⋈', 'Query executed', query);
 *   log.warn('⚠', 'Rate limit approaching');
 *   log.error('⊔', 'Database connection failed', error);
 *
 * TODO: Migrate remaining 43 files from console.* to log.*:
 *   - system-drawer.ts (3)
 *   - codemirror-editor.ts (5)
 *   - ai-provider-panel.ts (9)
 *   - plugin-panel.ts (17)
 *   - debug.ts (5)
 *   - legenda.ts (1)
 *   - pulse-panel.ts (1)
 *   - hixtory-panel.ts (4)
 *   - webscraper-panel.ts (1)
 *   - base-panel-error.ts (3)
 *   - usage-badge.ts (1)
 *   - config-panel.ts (6)
 *   - code/suggestions.ts (3)
 *   - dev-mode.ts (2)
 *   - tauri-notifications.ts (2)
 *   - console-reporter.ts (10)
 *   - main.ts (6)
 *   - code/panel.ts (8)
 *   - pulse/active-queue.ts (2)
 *   - command-explorer-panel.ts (1)
 *   - symbol-palette.ts (1)
 *   - filetree/navigator.ts (3)
 *   - python/panel.ts (9)
 *   - pulse/realtime-handlers.ts (1)
 *   - pulse/job-detail-panel.ts (5)
 *   - prose/panel.ts (1)
 *   - websocket-handlers/storage-eviction.ts (1)
 *   - prose/nodes/ats-code-block.ts (4)
 *   - pulse/panel-state.ts (2)
 *   - prose/editor.ts (4)
 *   - pulse/job-actions.ts (3)
 *   - websocket-handlers/storage-warning.ts (1)
 *   - pulse/system-status.ts (2)
 *   - prose/nodes/go-code-block.ts (2)
 *   - pulse/ats-node-view.ts (3)
 *   - websocket-handlers/system-capabilities.ts (1)
 *   - graph/renderer.ts (3)
 *   - storage.ts (6)
 *   - websocket-handlers/plugin-health.ts (1)
 *   - graph/focus.ts (9)
 *   - graph/focus/dimensions.ts (1)
 *   - graph/focus/physics.ts (2)
 *   - graph/tile/controls.ts (3)
 */

/**
 * Log levels in order of severity
 */
export type LogLevel = 'debug' | 'info' | 'warn' | 'error';

/**
 * Common SEG symbols for logging context
 * Use these as the first argument to log methods for consistency
 */
export const SEG = {
    // Primary operators
    SELF: '⍟',      // i - Self/vantage point
    CONFIG: '≡',    // am - Configuration
    INGEST: '⨳',    // ix - Data ingestion
    QUERY: '⋈',     // ax - Query/expand
    ACTOR: '⌬',     // by - Actor/catalyst
    TIME: '✦',      // at - Temporal marker
    FLOW: '⟶',      // so - Consequent action

    // System symbols
    PULSE: '꩜',     // Async operations
    DB: '⊔',        // Database/storage
    WS: '📡',       // WebSocket (using emoji for visibility)
    UI: '🖼',       // UI components
    GRAPH: '◇',     // Graph visualization
    ERROR: '⚠',     // Errors/warnings
} as const;

/**
 * Determine if we're in development mode
 * Checks multiple indicators for robustness
 */
function isDevMode(): boolean {
    // Check Vite/Bun dev mode via import.meta
    try {
        const meta = import.meta as { env?: { DEV?: boolean } };
        if (meta.env?.DEV) {
            return true;
        }
    } catch {
        // import.meta.env not available
    }
    // Check for localhost
    if (typeof window !== 'undefined') {
        const host = window.location?.hostname;
        if (host === 'localhost' || host === '127.0.0.1' || host?.endsWith('.local')) {
            return true;
        }
    }
    // Check for explicit dev flag
    if (typeof window !== 'undefined' && (window as unknown as { __DEV__?: boolean }).__DEV__) {
        return true;
    }
    return false;
}

/**
 * Current log level - debug in dev, warn in production
 */
const CURRENT_LEVEL: LogLevel = isDevMode() ? 'debug' : 'warn';

/**
 * Numeric values for level comparison
 */
const LEVEL_VALUES: Record<LogLevel, number> = {
    debug: 0,
    info: 1,
    warn: 2,
    error: 3,
};

/**
 * Check if a message at the given level should be logged
 */
function shouldLog(level: LogLevel): boolean {
    return LEVEL_VALUES[level] >= LEVEL_VALUES[CURRENT_LEVEL];
}

/**
 * Format the log prefix with timestamp and context
 */
function formatPrefix(context: string): string {
    return `[${context}]`;
}

/**
 * Logger interface - all methods preserve source location in dev tools
 */
export const log = {
    /**
     * Debug level - verbose logging for development
     * Silenced in production
     */
    debug(context: string, message: string, ...args: unknown[]): void {
        if (shouldLog('debug')) {
            console.log(formatPrefix(context), message, ...args);
        }
    },

    /**
     * Info level - general operational messages
     * Silenced in production
     */
    info(context: string, message: string, ...args: unknown[]): void {
        if (shouldLog('info')) {
            console.log(formatPrefix(context), message, ...args);
        }
    },

    /**
     * Warn level - potential issues that don't break functionality
     * Shown in production
     */
    warn(context: string, message: string, ...args: unknown[]): void {
        if (shouldLog('warn')) {
            console.warn(formatPrefix(context), message, ...args);
        }
    },

    /**
     * Error level - errors that affect functionality
     * Always shown
     */
    error(context: string, message: string, ...args: unknown[]): void {
        if (shouldLog('error')) {
            console.error(formatPrefix(context), message, ...args);
        }
    },

    /**
     * Get current log level
     */
    getLevel(): LogLevel {
        return CURRENT_LEVEL;
    },

    /**
     * Check if running in dev mode
     */
    isDevMode,
};

/**
 * Default export for convenience
 */
export default log;
