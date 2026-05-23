/**
 * Canvas Placement Mode
 *
 * Transient state between selecting a glyph type from the spawn menu
 * and clicking to place it on the canvas. Three visual phases:
 *
 * 1. Menu open — heavy scrim dims the canvas, only spawn symbols are bright
 * 2. Carrying — lighter scrim, a glyph manifestation follows the cursor
 * 3. Place (click) or cancel (Escape/right-click) — scrim lifts, normal state
 */

import type { GlyphTypeEntry } from '../glyph-registry';
import { log, SEG } from '../../../logger';

interface PlacementState {
    entry: GlyphTypeEntry;
    cursorGlyph: HTMLElement;
    scrim: HTMLElement;
    onMouseMove: (e: MouseEvent) => void;
    onMouseDown: (e: MouseEvent) => void;
    onKeyDown: (e: KeyboardEvent) => void;
    onContextMenu: (e: MouseEvent) => void;
}

let active: PlacementState | null = null;

/** Whether the canvas is currently in placement mode */
export function isPlacementActive(): boolean {
    return active !== null;
}

/** The scrim overlay class names for each phase */
const SCRIM_CLASS = 'placement-scrim';
const SCRIM_MENU_CLASS = 'placement-scrim--menu';
const SCRIM_CARRYING_CLASS = 'placement-scrim--carrying';

/** Cursor glyph class */
const CURSOR_GLYPH_CLASS = 'placement-cursor-glyph';

/**
 * Show the menu-phase scrim (heavy dim).
 * Called when the spawn menu opens.
 */
export function showMenuScrim(): HTMLElement {
    removeScrim();
    const scrim = document.createElement('div');
    scrim.className = `${SCRIM_CLASS} ${SCRIM_MENU_CLASS}`;
    document.body.appendChild(scrim);
    return scrim;
}

/** Remove any existing scrim */
export function removeScrim(): void {
    const existing = document.querySelector(`.${SCRIM_CLASS}`);
    if (existing) existing.remove();
}

/**
 * Enter placement mode after selecting a glyph type from the spawn menu.
 * Creates a cursor glyph that follows the mouse, transitions scrim to
 * carrying phase, and waits for click-to-place or cancel.
 */
export function enterPlacementMode(
    entry: GlyphTypeEntry,
    canvas: HTMLElement,
    placeCallback: (x: number, y: number) => void
): void {
    // Cancel any existing placement
    cancelPlacement();

    // Transition scrim from menu to carrying
    const existingScrim = document.querySelector(`.${SCRIM_CLASS}`);
    const scrim = existingScrim as HTMLElement || showMenuScrim();
    scrim.className = `${SCRIM_CLASS} ${SCRIM_CARRYING_CLASS}`;

    // Create cursor glyph — a small glyph manifestation that follows the pointer
    const cursorGlyph = document.createElement('div');
    cursorGlyph.className = CURSOR_GLYPH_CLASS;
    cursorGlyph.textContent = entry.symbol;
    cursorGlyph.setAttribute('data-glyph-type', entry.label);
    cursorGlyph.style.position = 'fixed';
    cursorGlyph.style.pointerEvents = 'none';
    cursorGlyph.style.zIndex = '10001';
    document.body.appendChild(cursorGlyph);

    log.debug(SEG.GLYPH, `[Canvas] Entered placement mode for ${entry.label}`);

    const onMouseMove = (e: MouseEvent) => {
        cursorGlyph.style.left = `${e.clientX}px`;
        cursorGlyph.style.top = `${e.clientY}px`;
    };

    const onMouseDown = (e: MouseEvent) => {
        if (e.button !== 0) return; // Left click only
        e.preventDefault();
        e.stopPropagation();

        // Calculate canvas-local coordinates
        const container = canvas.parentElement!;
        const containerRect = container.getBoundingClientRect();

        placeCallback(e.clientX, e.clientY);
        cancelPlacement();
    };

    const onKeyDown = (e: KeyboardEvent) => {
        if (e.key === 'Escape') {
            e.preventDefault();
            cancelPlacement();
        }
    };

    const onContextMenu = (e: MouseEvent) => {
        e.preventDefault();
        cancelPlacement();
    };

    document.addEventListener('mousemove', onMouseMove);
    document.addEventListener('mousedown', onMouseDown, { capture: true });
    document.addEventListener('keydown', onKeyDown);
    document.addEventListener('contextmenu', onContextMenu);

    active = { entry, cursorGlyph, scrim, onMouseMove, onMouseDown, onKeyDown, onContextMenu };
}

/** Cancel placement mode and clean up all listeners and DOM elements */
export function cancelPlacement(): void {
    if (!active) return;

    document.removeEventListener('mousemove', active.onMouseMove);
    document.removeEventListener('mousedown', active.onMouseDown, { capture: true });
    document.removeEventListener('keydown', active.onKeyDown);
    document.removeEventListener('contextmenu', active.onContextMenu);

    active.cursorGlyph.remove();
    active.scrim.remove();

    log.debug(SEG.GLYPH, `[Canvas] Exited placement mode for ${active.entry.label}`);
    active = null;
}
