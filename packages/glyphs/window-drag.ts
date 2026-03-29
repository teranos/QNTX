/**
 * Window Drag — standalone drag implementation for glyph windows.
 *
 * Supports mouse and touch. Constrains the window to keep at least
 * 50px visible on screen. Saves position via dataset helpers on drag end.
 *
 * No canvas awareness — works with any fixed-position element.
 */

import { setLastPosition } from './dataset';

const DRAG_KEY = '__glyphWindowDrag';

export function setupWindowDrag(windowElement: HTMLElement, handle: HTMLElement): void {
    let isDragging = false;
    let offsetX = 0;
    let offsetY = 0;

    const startDrag = (clientX: number, clientY: number) => {
        isDragging = true;
        const rect = windowElement.getBoundingClientRect();
        offsetX = clientX - rect.left;
        offsetY = clientY - rect.top;
        document.body.style.cursor = 'move';

        window.addEventListener('mousemove', drag);
        window.addEventListener('mouseup', stopDrag);
        window.addEventListener('touchmove', touchDrag, { passive: false });
        window.addEventListener('touchend', stopDrag);
    };

    const drag = (e: MouseEvent) => {
        if (!isDragging) return;
        applyDragPosition(windowElement, e.clientX - offsetX, e.clientY - offsetY);
    };

    const touchDrag = (e: TouchEvent) => {
        if (!isDragging || !e.touches[0]) return;
        e.preventDefault();
        applyDragPosition(windowElement, e.touches[0].clientX - offsetX, e.touches[0].clientY - offsetY);
    };

    const stopDrag = () => {
        if (!isDragging) return;
        isDragging = false;
        document.body.style.cursor = '';

        const rect = windowElement.getBoundingClientRect();
        setLastPosition(windowElement, rect.left, rect.top);

        window.removeEventListener('mousemove', drag);
        window.removeEventListener('mouseup', stopDrag);
        window.removeEventListener('touchmove', touchDrag);
        window.removeEventListener('touchend', stopDrag);
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

    // Store for teardown
    (windowElement as any)[DRAG_KEY] = { isDragging, offsetX, offsetY, handleMouseDown, handleTouchStart, handle };
}

function applyDragPosition(el: HTMLElement, newX: number, newY: number): void {
    const rect = el.getBoundingClientRect();
    const minVisible = 50;
    newX = Math.max(-rect.width + minVisible, Math.min(window.innerWidth - minVisible, newX));
    newY = Math.max(0, Math.min(window.innerHeight - minVisible, newY));
    el.style.left = `${newX}px`;
    el.style.top = `${newY}px`;
}

export function teardownWindowDrag(windowElement: HTMLElement): void {
    const state = (windowElement as any)[DRAG_KEY];
    if (!state) return;
    const { handleMouseDown, handleTouchStart, handle } = state;
    handle.removeEventListener('mousedown', handleMouseDown);
    handle.removeEventListener('touchstart', handleTouchStart);
    delete (windowElement as any)[DRAG_KEY];
}
