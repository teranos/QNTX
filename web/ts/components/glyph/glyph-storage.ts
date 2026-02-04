/**
 * Generic glyph status persistence
 *
 * Provides reusable status types and storage helpers for all glyph types.
 * Eliminates duplication across IX, Prompt, and future glyph components.
 */

import { log, SEG } from '../../logger';

/**
 * Base status interface shared by all glyph types
 */
export interface GlyphStatus {
    state: 'idle' | 'running' | 'success' | 'error';
    message?: string;
    timestamp?: number;
}

/**
 * Save glyph status to localStorage
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
    localStorage.setItem(key, JSON.stringify(status));
}

/**
 * Load glyph status from localStorage
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
    const stored = localStorage.getItem(key);
    if (!stored) return null;

    try {
        return JSON.parse(stored) as T;
    } catch (e) {
        log.error(SEG.UI, `[${type}] Failed to parse stored status for ${glyphId}:`, e);
        return null;
    }
}

/**
 * Extended status for glyphs with job execution tracking (e.g., IX glyph)
 */
export interface ExecutableGlyphStatus extends GlyphStatus {
    scheduledJobId?: string;
    executionId?: string;
}
