/**
 * Thread navigation — pure decision logic for arrow keys when the selected
 * glyph is on a thread.
 *
 * - ←/→: step prev/next along the active spine, skipping the 〽 end marker.
 *   No-op at the ends (no wrap).
 * - ↑/↓: rotate which of the current glyph's spines is active. Wraps.
 *   No effect when the glyph is on only one spine.
 *
 * This module makes the decisions; the caller applies the side effects
 * (selecting the target, updating the camera, etc.).
 */

import type { Direction } from './keyboard-shortcuts';
import { getSpinesByNode } from './spine-renderer';

export interface ThreadNavContext {
    canvasId: string;
    /** Currently selected glyph, or null when nothing is selected. */
    currentGlyphId: string | null;
    /** Per-glyph "active thread" state — mutated by this function. */
    activeSpinePerGlyph: Map<string, string>;
}

export interface ThreadNavResult {
    /**
     * True when this function handled the arrow key (the caller should not
     * fall through to spatial navigation). False when the key wasn't
     * applicable (no selection, glyph not on a thread).
     */
    handled: boolean;
    /** When set, caller should select this glyph and pan to it. */
    targetGlyphId?: string;
    /** When set, the active spine for the current glyph changed to this id. */
    activatedSpineId?: string;
}

export function navigateThread(direction: Direction, ctx: ThreadNavContext): ThreadNavResult {
    if (!ctx.currentGlyphId) return { handled: false };
    const spines = getSpinesByNode(ctx.canvasId, ctx.currentGlyphId);
    if (spines.length === 0) return { handled: false };

    if (direction === 'up' || direction === 'down') {
        if (spines.length <= 1) return { handled: true };
        const activeId = ctx.activeSpinePerGlyph.get(ctx.currentGlyphId) ?? spines[0].id;
        const idx = spines.findIndex(s => s.id === activeId);
        const validIdx = idx === -1 ? 0 : idx;
        const delta = direction === 'up' ? -1 : 1;
        const newIdx = (validIdx + delta + spines.length) % spines.length;
        const newSpineId = spines[newIdx].id;
        ctx.activeSpinePerGlyph.set(ctx.currentGlyphId, newSpineId);
        return { handled: true, activatedSpineId: newSpineId };
    }

    // ←/→ — step along the active spine, skipping 〽
    const activeId = ctx.activeSpinePerGlyph.get(ctx.currentGlyphId) ?? spines[0].id;
    const activeSpine = spines.find(s => s.id === activeId) ?? spines[0];
    const navNodes = activeSpine.nodes.slice(0, -1); // 〽 is the last node — never a nav stop

    let currentIdx = navNodes.indexOf(ctx.currentGlyphId);
    if (currentIdx === -1) {
        // Current is the 〽 itself — treat as one-past-the-end (← lands on last real glyph)
        if (direction === 'right') return { handled: true };
        currentIdx = navNodes.length;
    }
    const delta = direction === 'left' ? -1 : 1;
    const newIdx = currentIdx + delta;
    if (newIdx < 0 || newIdx >= navNodes.length) return { handled: true }; // no-op at ends

    const targetId = navNodes[newIdx];
    ctx.activeSpinePerGlyph.set(targetId, activeSpine.id);
    return { handled: true, targetGlyphId: targetId };
}
