/**
 * Canvas Placement Mode
 *
 * Transient state between selecting an element type from the spawn menu
 * and clicking to place it on the canvas. Three visual phases:
 *
 * 1. Menu open — heavy scrim dims the canvas, only spawn symbols are bright
 * 2. Carrying — lighter scrim, a cursor element manifestation follows the pointer
 * 3. Place (click) or cancel (Escape/right-click) — scrim lifts, normal state
 */

import type { ElementTypeEntry } from '../element-registry';
import { createCursorElement, attachCursorToMouse, prepareCursorForPlacement } from '@teranos/elements';
import { log, SEG } from '../../../logger';

export interface PlacementOptions {
    /** Apply color to the cursor element (e.g., thread color) */
    cursorColor?: string;
    /** Called when placement ends (place or cancel) for external cleanup */
    onExit?: () => void;
}

interface PlacementState {
    entry: ElementTypeEntry;
    cursorItem: HTMLElement;
    scrim: HTMLElement;
    cleanupCursorMove: () => void;
    onMouseDown: (e: MouseEvent) => void;
    onKeyDown: (e: KeyboardEvent) => void;
    onContextMenu: (e: MouseEvent) => void;
    onMouseMove: ((e: MouseEvent) => void) | null;
    onExit: (() => void) | undefined;
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

/**
 * Adopt an existing element as a cursor element — reparent to body,
 * strip spawn-menu styles, apply cursor class. Preserves DOM identity.
 */
function adoptAsCursor(el: HTMLElement, symbol: string, elementType: string): HTMLElement {
    // Strip spawn menu context reveal if present
    const reveal = el.querySelector('.spawn-context-reveal');
    if (reveal) reveal.remove();

    // Reset to cursor element styling
    el.className = 'cursor';
    el.setAttribute('data-element-type', elementType);
    el.style.cssText = 'position: fixed; pointer-events: none; z-index: 10003;';

    // Ensure symbol span exists
    let sym = el.querySelector('.cursor-symbol') as HTMLElement | null;
    if (!sym) {
        el.textContent = '';
        sym = document.createElement('span');
        sym.className = 'cursor-symbol';
        sym.textContent = symbol;
        el.appendChild(sym);
    }

    // Reparent to body if not already there
    if (el.parentElement !== document.body) {
        document.body.appendChild(el);
    }

    return el;
}

/** Remove any existing scrim */
export function removeScrim(): void {
    const existing = document.querySelector(`.${SCRIM_CLASS}`);
    if (existing) existing.remove();
}

/**
 * Enter placement mode after selecting an element type from the spawn menu.
 * Transitions the scrim to carrying phase and attaches a cursor element to the mouse.
 *
 * If an existing element is provided (e.g. the spawn menu button), it is
 * adopted as the cursor element — preserving DOM identity per the element axiom.
 * Otherwise a new cursor element is created.
 */
export function enterPlacementMode(
    entry: ElementTypeEntry,
    _canvas: HTMLElement,
    placeCallback: (x: number, y: number, cursorElement: HTMLElement, cursorRect: DOMRect, symbolElement: HTMLElement | null, content?: string) => void,
    existingElement?: HTMLElement,
    options?: PlacementOptions
): void {
    // Cancel any existing placement
    cancelPlacement();

    // Transition scrim from menu to carrying
    const existingScrim = document.querySelector(`.${SCRIM_CLASS}`);
    const scrim = existingScrim as HTMLElement || showMenuScrim();
    scrim.className = `${SCRIM_CLASS} ${SCRIM_CARRYING_CLASS}`;

    // Adopt existing element or create new cursor element
    const cursorItem = existingElement
        ? adoptAsCursor(existingElement, entry.symbol, entry.label)
        : createCursorElement(entry.symbol, entry.label);
    if (!existingElement) document.body.appendChild(cursorItem);
    const cleanupCursorMove = attachCursorToMouse(cursorItem);

    // Apply cursor color if specified (e.g., thread color)
    if (options?.cursorColor) {
        cursorItem.style.color = options.cursorColor;
    }

    // Ax segment glow: all triplet values light up when carrying an ax cursor
    const isAx = entry.label === 'AX';
    if (isAx) document.body.classList.add('ax-placement-active');

    const onMouseMove = null;

    log.debug(SEG.ELEMENT, `[Canvas] Entered placement mode for ${entry.label}`);

    const onMouseDown = (e: MouseEvent) => {
        if (e.button !== 0) return; // Left click only
        e.preventDefault();
        e.stopPropagation();

        // Check if clicking an ax segment — hit-test through cursor
        let segmentContent: string | undefined;
        if (isAx) {
            cursorItem.style.display = 'none';
            const target = document.elementFromPoint(e.clientX, e.clientY) as HTMLElement | null;
            cursorItem.style.display = '';
            segmentContent = target?.closest('[data-ax-segment]')?.getAttribute('data-ax-segment') ?? undefined;
        }

        // Capture cursor rect before stripping styles (for morph animation)
        const cursorRect = cursorItem.getBoundingClientRect();
        // Prepare element for adoption — strip cursor styles, extract symbol span
        const symbolSpan = prepareCursorForPlacement(cursorItem);
        placeCallback(e.clientX, e.clientY, cursorItem, cursorRect, symbolSpan, segmentContent);
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

    active = { entry, cursorItem, scrim, cleanupCursorMove, onMouseDown, onKeyDown, onContextMenu, onMouseMove, onExit: options?.onExit };
}

/** Clean up listeners and scrim shared by both place and cancel */
function cleanupListeners(): void {
    if (!active) return;
    active.onExit?.();
    active.cleanupCursorMove();
    document.body.classList.remove('ax-placement-active');
    document.removeEventListener('mousedown', active.onMouseDown, { capture: true });
    document.removeEventListener('keydown', active.onKeyDown);
    document.removeEventListener('contextmenu', active.onContextMenu);
    active.scrim.remove();
}

/** Finalize placement — element was handed off, only clean up listeners/scrim */
function finalizePlacement(): void {
    if (!active) return;
    cleanupListeners();
    log.debug(SEG.ELEMENT, `[Canvas] Placed ${active.entry.label} element`);
    active = null;
}

/** Cancel placement mode — remove cursor element and clean up */
export function cancelPlacement(): void {
    if (!active) return;
    cleanupListeners();
    active.cursorItem.remove();
    log.debug(SEG.ELEMENT, `[Canvas] Cancelled placement for ${active.entry.label}`);
    active = null;
}
