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
