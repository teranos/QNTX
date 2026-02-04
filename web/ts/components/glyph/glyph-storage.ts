/**
 * Generic glyph status persistence
 *
 * Provides reusable status types and storage helpers for all glyph types.
 * Eliminates duplication across IX, Prompt, and future glyph components.
 *
 * Uses IndexedDB (via state/storage.ts) for persistence with in-memory cache.
 */

import { getItem, setItem } from '../../state/storage';

/**
 * Base status interface shared by all glyph types
 */
export interface GlyphStatus {
    state: 'idle' | 'running' | 'success' | 'error';
    message?: string;
    timestamp?: number;
}

/**
 * Save glyph status to IndexedDB
 *
 * @param type - Glyph type identifier (e.g., 'ix', 'prompt', 'ax')
 * @param glyphId - Unique glyph ID
 * @param status - Status object to persist
 */
export function saveGlyphStatus<T extends GlyphStatus>(
    type: string,
    glyphId: string,
    status: T
): void {
    const key = `${type}-status-${glyphId}`;
    setItem(key, status);
}

/**
 * Load glyph status from IndexedDB
 *
 * @param type - Glyph type identifier (e.g., 'ix', 'prompt', 'ax')
 * @param glyphId - Unique glyph ID
 * @returns Parsed status object or null if not found/invalid
 */
export function loadGlyphStatus<T extends GlyphStatus>(
    type: string,
    glyphId: string
): T | null {
    const key = `${type}-status-${glyphId}`;
    return getItem<T>(key);
}

/**
 * Extended status for glyphs with job execution tracking (e.g., IX glyph)
 */
export interface ExecutableGlyphStatus extends GlyphStatus {
    scheduledJobId?: string;
    executionId?: string;
}
