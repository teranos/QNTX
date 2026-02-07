/**
 * Canvas API Client
 *
 * Provides HTTP API calls for persisting canvas state (glyphs and compositions)
 * to the backend database.
 */

import type { CanvasGlyphState, CompositionState } from '../state/ui';
import { log, SEG } from '../logger';

export interface CanvasGlyphResponse {
    id: string;
    symbol: string;
    x: number;
    y: number;
    width?: number;
    height?: number;
    result_data?: string;
    created_at: string;
    updated_at: string;
}

export interface CompositionResponse {
    id: string;
    type: 'ax-prompt' | 'ax-py' | 'py-prompt';
    initiator_id: string;
    target_id: string;
    x: number;
    y: number;
    created_at: string;
    updated_at: string;
}

/**
 * Upsert a canvas glyph (create or update)
 */
export async function upsertCanvasGlyph(glyph: CanvasGlyphState): Promise<void> {
    try {
        const payload = {
            id: glyph.id,
            symbol: glyph.symbol,
            x: glyph.x,
            y: glyph.y,
            width: glyph.width,
            height: glyph.height,
            result_data: glyph.result ? JSON.stringify(glyph.result) : undefined,
        };

        const response = await fetch('/api/canvas/glyphs', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify(payload),
        });

        if (!response.ok) {
            const error = await response.json();
            throw new Error(error.error || 'Failed to upsert canvas glyph');
        }

        log.debug(SEG.GLYPH, `[CanvasAPI] Upserted glyph ${glyph.id}`);
    } catch (error) {
        log.error(SEG.GLYPH, `[CanvasAPI] Failed to upsert glyph ${glyph.id}:`, error);
        throw error;
    }
}

/**
 * Delete a canvas glyph
 */
export async function deleteCanvasGlyph(id: string): Promise<void> {
    try {
        const response = await fetch(`/api/canvas/glyphs/${id}`, {
            method: 'DELETE',
        });

        if (!response.ok) {
            const error = await response.json();
            throw new Error(error.error || 'Failed to delete canvas glyph');
        }

        log.debug(SEG.GLYPH, `[CanvasAPI] Deleted glyph ${id}`);
    } catch (error) {
        log.error(SEG.GLYPH, `[CanvasAPI] Failed to delete glyph ${id}:`, error);
        throw error;
    }
}

/**
 * List all canvas glyphs
 */
export async function listCanvasGlyphs(): Promise<CanvasGlyphResponse[]> {
    try {
        const response = await fetch('/api/canvas/glyphs');
        if (!response.ok) {
            const error = await response.json();
            throw new Error(error.error || 'Failed to list canvas glyphs');
        }

        const glyphs = await response.json();
        log.debug(SEG.GLYPH, `[CanvasAPI] Listed ${glyphs.length} glyphs`);
        return glyphs;
    } catch (error) {
        log.error(SEG.GLYPH, '[CanvasAPI] Failed to list glyphs:', error);
        throw error;
    }
}

/**
 * Upsert a canvas composition (create or update)
 */
export async function upsertComposition(composition: CompositionState): Promise<void> {
    try {
        const payload = {
            id: composition.id,
            type: composition.type,
            initiator_id: composition.initiatorId,
            target_id: composition.targetId,
            x: composition.x,
            y: composition.y,
        };

        const response = await fetch('/api/canvas/compositions', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify(payload),
        });

        if (!response.ok) {
            const error = await response.json();
            throw new Error(error.error || 'Failed to upsert composition');
        }

        log.debug(SEG.GLYPH, `[CanvasAPI] Upserted composition ${composition.id}`);
    } catch (error) {
        log.error(SEG.GLYPH, `[CanvasAPI] Failed to upsert composition ${composition.id}:`, error);
        throw error;
    }
}

/**
 * Delete a canvas composition
 */
export async function deleteComposition(id: string): Promise<void> {
    try {
        const response = await fetch(`/api/canvas/compositions/${id}`, {
            method: 'DELETE',
        });

        if (!response.ok) {
            const error = await response.json();
            throw new Error(error.error || 'Failed to delete composition');
        }

        log.debug(SEG.GLYPH, `[CanvasAPI] Deleted composition ${id}`);
    } catch (error) {
        log.error(SEG.GLYPH, `[CanvasAPI] Failed to delete composition ${id}:`, error);
        throw error;
    }
}

/**
 * List all canvas compositions
 */
export async function listCompositions(): Promise<CompositionResponse[]> {
    try {
        const response = await fetch('/api/canvas/compositions');
        if (!response.ok) {
            const error = await response.json();
            throw new Error(error.error || 'Failed to list compositions');
        }

        const compositions = await response.json();
        log.debug(SEG.GLYPH, `[CanvasAPI] Listed ${compositions.length} compositions`);
        return compositions;
    } catch (error) {
        log.error(SEG.GLYPH, '[CanvasAPI] Failed to list compositions:', error);
        throw error;
    }
}

/**
 * Load all canvas state from backend (glyphs + compositions)
 * Converts backend format to frontend state format
 */
export async function loadCanvasState(): Promise<{
    glyphs: CanvasGlyphState[];
    compositions: CompositionState[];
}> {
    try {
        const [glyphsResponse, compositionsResponse] = await Promise.all([
            listCanvasGlyphs(),
            listCompositions(),
        ]);

        // Convert backend format to frontend format
        const glyphs: CanvasGlyphState[] = glyphsResponse.map(g => ({
            id: g.id,
            symbol: g.symbol,
            x: g.x,
            y: g.y,
            width: g.width,
            height: g.height,
            result: g.result_data ? JSON.parse(g.result_data) : undefined,
        }));

        const compositions: CompositionState[] = compositionsResponse.map(c => ({
            id: c.id,
            type: c.type,
            initiatorId: c.initiator_id,
            targetId: c.target_id,
            x: c.x,
            y: c.y,
        }));

        log.info(SEG.GLYPH, `[CanvasAPI] Loaded canvas state: ${glyphs.length} glyphs, ${compositions.length} compositions`);

        return { glyphs, compositions };
    } catch (error) {
        log.error(SEG.GLYPH, '[CanvasAPI] Failed to load canvas state:', error);
        throw error;
    }
}
