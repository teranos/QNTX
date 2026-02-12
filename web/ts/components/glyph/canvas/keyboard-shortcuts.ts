/**
 * Canvas Keyboard Shortcuts
 *
 * Handles keyboard interactions for canvas operations:
 * - ESC: deselect all glyphs
 * - DELETE/BACKSPACE: remove selected glyphs
 * - U: unmeld selected composition
 * - 0: reset zoom/pan to origin
 * - 1: fit all glyphs in view (TODO)
 *
 * Shortcuts are scoped to the focused canvas container
 * Uses AbortController for automatic cleanup when container is removed
 */

import { log, SEG } from '../../../logger';

/**
 * Callback to check if any glyphs are selected
 */
export type HasSelectionCallback = () => boolean;

/**
 * Setup keyboard shortcuts for canvas container
 * Uses AbortController for automatic cleanup - signal will abort when container is removed
 */
export function setupKeyboardShortcuts(
    container: HTMLElement,
    hasSelection: HasSelectionCallback,
    onDeselect: () => void,
    onDelete: () => void,
    onUnmeld: () => void,
    onResetView: () => void
): AbortController {
    const controller = new AbortController();

    const handleKeydown = (e: KeyboardEvent) => {
        // Ignore if user is typing in an input/textarea
        const target = e.target as HTMLElement;
        if (target.tagName === 'INPUT' || target.tagName === 'TEXTAREA' || target.isContentEditable) {
            return;
        }

        // ESC to deselect
        if (e.key === 'Escape') {
            if (hasSelection()) {
                e.preventDefault();
                onDeselect();
                log.debug(SEG.GLYPH, '[Canvas] ESC pressed - deselecting all glyphs');
            }
            return;
        }

        // 0 to reset zoom and pan to origin (works regardless of selection)
        if (e.key === '0' && !e.ctrlKey && !e.metaKey && !e.altKey && !e.shiftKey) {
            e.preventDefault();
            onResetView();
            log.debug(SEG.GLYPH, '[Canvas] 0 pressed - resetting zoom/pan to origin');
            return;
        }

        // Following shortcuts require selection
        if (!hasSelection()) {
            return;
        }

        // DELETE/BACKSPACE to delete selected glyphs
        if (e.key === 'Delete' || e.key === 'Backspace') {
            e.preventDefault();
            onDelete();
            log.debug(SEG.GLYPH, '[Canvas] DELETE/BACKSPACE pressed - deleting selected glyphs');
            return;
        }

        // U to unmeld composition
        if (e.key === 'u' || e.key === 'U') {
            e.preventDefault();
            onUnmeld();
            log.debug(SEG.GLYPH, '[Canvas] U pressed - unmelding selected composition');
            return;
        }

        // TODO: 1 to fit all glyphs in view
        // Calculate bounding box of all canvas glyphs and zoom/pan to show everything
        // if (e.key === '1' && !e.ctrlKey && !e.metaKey && !e.altKey && !e.shiftKey) {
        //     e.preventDefault();
        //     onFitToView();
        //     return;
        // }
    };

    container.addEventListener('keydown', handleKeydown, { signal: controller.signal });

    return controller;
}
