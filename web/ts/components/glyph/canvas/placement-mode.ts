/**
 * Canvas Placement Mode
 *
 * Transient state between selecting a glyph type from the spawn menu
 * and clicking to place it on the canvas. Three visual phases:
 *
 * 1. Menu open — heavy scrim dims the canvas, only spawn symbols are bright
 * 2. Carrying — lighter scrim, a cursor glyph manifestation follows the pointer
 * 3. Place (click) or cancel (Escape/right-click) — scrim lifts, normal state
 */

import type { GlyphTypeEntry } from '../glyph-registry';
import { createCursorElement, attachCursorToMouse, prepareCursorForPlacement } from '@qntx/glyphs';
import { log, SEG } from '../../../logger';

interface PlacementState {
    entry: GlyphTypeEntry;
    cursorGlyph: HTMLElement;
    scrim: HTMLElement;
    cleanupCursorMove: () => void;
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
 * Creates a cursor glyph manifestation that follows the mouse, transitions
 * scrim to carrying phase, and waits for click-to-place or cancel.
 */
export function enterPlacementMode(
    entry: GlyphTypeEntry,
    canvas: HTMLElement,
    placeCallback: (x: number, y: number, cursorElement: HTMLElement) => void
): void {
    // Cancel any existing placement
    cancelPlacement();

    // Transition scrim from menu to carrying
    const existingScrim = document.querySelector(`.${SCRIM_CLASS}`);
    const scrim = existingScrim as HTMLElement || showMenuScrim();
    scrim.className = `${SCRIM_CLASS} ${SCRIM_CARRYING_CLASS}`;

    // Create cursor glyph manifestation
    const cursorGlyph = createCursorElement(entry.symbol, entry.label);
    document.body.appendChild(cursorGlyph);
    const cleanupCursorMove = attachCursorToMouse(cursorGlyph);

    log.debug(SEG.GLYPH, `[Canvas] Entered placement mode for ${entry.label}`);

    const onMouseDown = (e: MouseEvent) => {
        if (e.button !== 0) return; // Left click only
        e.preventDefault();
        e.stopPropagation();

        // Prepare element for adoption by canvasPlaced — strip cursor styles
        prepareCursorForPlacement(cursorGlyph);
        placeCallback(e.clientX, e.clientY, cursorGlyph);
        finalizePlacement();
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

    document.addEventListener('mousedown', onMouseDown, { capture: true });
    document.addEventListener('keydown', onKeyDown);
    document.addEventListener('contextmenu', onContextMenu);

    active = { entry, cursorGlyph, scrim, cleanupCursorMove, onMouseDown, onKeyDown, onContextMenu };
}

/** Clean up listeners and scrim shared by both place and cancel */
function cleanupListeners(): void {
    if (!active) return;
    active.cleanupCursorMove();
    document.removeEventListener('mousedown', active.onMouseDown, { capture: true });
    document.removeEventListener('keydown', active.onKeyDown);
    document.removeEventListener('contextmenu', active.onContextMenu);
    active.scrim.remove();
}

/** Finalize placement — element was handed off, only clean up listeners/scrim */
function finalizePlacement(): void {
    if (!active) return;
    cleanupListeners();
    log.debug(SEG.GLYPH, `[Canvas] Placed ${active.entry.label} glyph`);
    active = null;
}

/** Cancel placement mode — remove cursor element and clean up */
export function cancelPlacement(): void {
    if (!active) return;
    cleanupListeners();
    active.cursorGlyph.remove();
    log.debug(SEG.GLYPH, `[Canvas] Cancelled placement for ${active.entry.label}`);
    active = null;
}
