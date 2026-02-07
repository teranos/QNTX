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
 * ✅ Migration Complete: All console.* calls migrated to log.*
 *
 * Migrated files:
 *   - Core infrastructure: main.ts, qntx-wasm.ts, dev-mode.ts
 *   - WebSocket handlers: storage-warning.ts, storage-eviction.ts, system-capabilities.ts
 *   - Graph visualization: renderer.ts, focus.ts, focus/dimensions.ts, focus/physics.ts, tile/controls.ts
 *   - Panels: pulse-panel.ts, prose/panel.ts, config-panel.ts, hixtory-panel.ts, command-explorer-panel.ts
 *   - Editors: prose/editor.ts, code/panel.ts, code/suggestions.ts, codemirror-editor.ts
 *   - Components: type-attestations.ts, base-panel-error.ts, glyph/py-glyph.ts
 *   - Prose nodes: ats-code-block.ts, go-code-block.ts, frontmatter-block.ts
 *   - Utilities: tauri-notifications.ts, symbol-palette.ts, fuzzy-search-view.ts
 *   - And more...
 *
 * Note: dev-debug-interceptor.ts and test files intentionally use console.* directly
 */

// Import core QNTX symbols from generated types
import * as CoreSEG from '../../types/generated/typescript/sym.js';

/**
 * Log levels in order of severity
 */
export type LogLevel = 'debug' | 'info' | 'warn' | 'error';

/**
 * SEG symbols for logging context
 * Combines core QNTX symbols with UI-specific extensions
 */
export const SEG = {
    // Core QNTX symbols (from generated types)
    SELF: CoreSEG.I,        // ⍟ - Self/vantage point
    CONFIG: CoreSEG.AM,     // ≡ - Configuration
    INGEST: CoreSEG.IX,     // ⨳ - Data ingestion
    QUERY: CoreSEG.AX,      // ⋈ - Query/expand
    ACTOR: CoreSEG.BY,      // ⌬ - Actor/catalyst
    TIME: CoreSEG.AT,       // ✦ - Temporal marker
    FLOW: CoreSEG.SO,       // ⟶ - Consequent action
    PULSE: CoreSEG.Pulse,   // ꩜ - Async operations
    DB: CoreSEG.DB,         // ⊔ - Database/storage

    // UI-specific extensions (not in core)
    WS: '⥂' as const,       // WebSocket communications
    UI: '▦' as const,       // UI components
    GLYPH: '⧉' as const,    // Glyph system
    GRAPH: '◇' as const,    // Graph visualization
    ERROR: '⚠' as const,    // Errors/warnings
    VID: '⮀' as const,      // VidStream
    WASM: '⧩' as const,     // WebAssembly/WASM module
    TAU: 'τ' as const,      // Tauri native integration
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
 * Logger implementation
 */
const logger = {
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
 * Shorthand wrapper - log() is an alias for log.info()
 */
function log(context: string, message: string, ...args: unknown[]): void {
    logger.info(context, message, ...args);
}

// Attach logger methods to log function
log.debug = logger.debug.bind(logger);
log.info = logger.info.bind(logger);
log.warn = logger.warn.bind(logger);
log.error = logger.error.bind(logger);
log.getLevel = logger.getLevel.bind(logger);
log.isDevMode = logger.isDevMode;

export { log };

/**
 * Default export for convenience
 */
export default log;
