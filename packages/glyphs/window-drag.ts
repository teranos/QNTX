/**
 * Window Drag — standalone drag implementation for glyph windows.
 *
 * Supports mouse and touch. Constrains the window to keep at least
 * 50px visible on screen. Saves position via dataset helpers on drag end.
 *
 * No canvas awareness — works with any fixed-position element.
 */

import { setLastPosition } from './dataset';
import { reflowBox } from './window-reflow';

const DRAG_KEY = '__glyphWindowDrag';

/** The width a window has when nothing is squeezing it. */
const NATURAL_KEY = '__glyphNaturalWidth';

/** How much of a window stays on screen at the bottom: its title bar. */
const TITLE_BAR_VISIBLE = 50;

/**
 * Remember how wide a window is when it is not against an edge.
 *
 * The drag asks with this every frame rather than with the width on screen, so
 * a window that gave way at an edge is its whole self again once it leaves.
 */
export function setNaturalWidth(el: HTMLElement, width: number): void {
    (el as unknown as Record<string, number>)[NATURAL_KEY] = width;
}

function naturalWidth(el: HTMLElement): number {
    const held = (el as unknown as Record<string, number>)[NATURAL_KEY];
    return typeof held === 'number' ? held : el.getBoundingClientRect().width;
}

/**
 * The width this glyph's content needs, asked of the browser once per drag.
 *
 * `min-content` is the browser's own answer to how narrow this can be and
 * still be laid out, which is the only place that number can come from.
 */
function contentFloor(el: HTMLElement): number {
    const held = el.style.width;
    const heldMax = el.style.maxWidth;
    el.style.maxWidth = 'none';
    el.style.width = 'min-content';
    const floor = el.getBoundingClientRect().width;
    el.style.width = held;
    el.style.maxWidth = heldMax;
    return floor;
}

interface DragState {
    handleMouseDown: (e: MouseEvent) => void;
    handleTouchStart: (e: TouchEvent) => void;
    handle: HTMLElement;
    dragController: AbortController | null;
}

export function setupWindowDrag(windowElement: HTMLElement, handle: HTMLElement): void {
    let isDragging = false;
    let offsetX = 0;
    let offsetY = 0;
    let dragController: AbortController | null = null;
    // Asked once when a drag starts: the content is settled by then, and asking
    // every frame would measure a window that is already being squeezed.
    let floor = 0;

    const stopDrag = () => {
        if (!isDragging) return;
        isDragging = false;
        document.body.style.cursor = '';

        const rect = windowElement.getBoundingClientRect();
        setLastPosition(windowElement, rect.left, rect.top);

        dragController?.abort();
        dragController = null;
        state.dragController = null;
    };

    const drag = (e: MouseEvent) => {
        if (!isDragging) return;
        applyDragPosition(windowElement, e.clientX - offsetX, e.clientY - offsetY, floor);
    };

    const touchDrag = (e: TouchEvent) => {
        if (!isDragging || !e.touches[0]) return;
        e.preventDefault();
        applyDragPosition(windowElement, e.touches[0].clientX - offsetX, e.touches[0].clientY - offsetY, floor);
    };

    const startDrag = (clientX: number, clientY: number) => {
        isDragging = true;
        floor = contentFloor(windowElement);
        const rect = windowElement.getBoundingClientRect();
        offsetX = clientX - rect.left;
        offsetY = clientY - rect.top;
        document.body.style.cursor = 'move';

        dragController = new AbortController();
        state.dragController = dragController;
        const signal = dragController.signal;

        window.addEventListener('mousemove', drag, { signal });
        window.addEventListener('mouseup', stopDrag, { signal });
        window.addEventListener('touchmove', touchDrag, { passive: false, signal });
        window.addEventListener('touchend', stopDrag, { signal });
    };

    const handleMouseDown = (e: MouseEvent) => {
        if ((e.target as HTMLElement).tagName === 'BUTTON') return;
        e.preventDefault();
        startDrag(e.clientX, e.clientY);
    };

    const handleTouchStart = (e: TouchEvent) => {
        if ((e.target as HTMLElement).tagName === 'BUTTON') return;
        if (!e.touches[0]) return;
        e.preventDefault();
        startDrag(e.touches[0].clientX, e.touches[0].clientY);
    };

    handle.addEventListener('mousedown', handleMouseDown);
    handle.addEventListener('touchstart', handleTouchStart, { passive: false });

    const state: DragState = { handleMouseDown, handleTouchStart, handle, dragController };
    (windowElement as any)[DRAG_KEY] = state;
}

function applyDragPosition(el: HTMLElement, newX: number, newY: number, floor: number): void {
    // Both edges, so a window gives way at the left the way it does at the
    // right. A fixed box measures its own room from the left alone.
    const box = reflowBox(newX, naturalWidth(el), window.innerWidth, floor);
    el.style.left = `${box.left}px`;
    el.style.maxWidth = `${box.width}px`;

    el.style.top = `${Math.max(0, Math.min(window.innerHeight - TITLE_BAR_VISIBLE, newY))}px`;
}

export function teardownWindowDrag(windowElement: HTMLElement): void {
    const state = (windowElement as any)[DRAG_KEY] as DragState | undefined;
    if (!state) return;
    const { handleMouseDown, handleTouchStart, handle, dragController } = state;
    // Abort any in-progress drag (cleans up global mousemove/mouseup/touchmove/touchend)
    dragController?.abort();
    document.body.style.cursor = '';
    handle.removeEventListener('mousedown', handleMouseDown);
    handle.removeEventListener('touchstart', handleTouchStart);
    delete (windowElement as any)[DRAG_KEY];
}
