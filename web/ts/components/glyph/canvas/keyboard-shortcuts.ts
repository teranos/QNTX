/**
 * Canvas Keyboard Shortcuts
 *
 * Handles keyboard interactions for canvas operations:
 * - ESC: deselect all glyphs
 * - DELETE/BACKSPACE: remove selected glyphs
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
    onDelete: () => void
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
                log.debug(SEG.UI, '[Canvas] ESC pressed - deselecting all glyphs');
            }
            return;
        }

        // DELETE/BACKSPACE to delete selected glyphs
        if (!hasSelection()) {
            return;
        }

        if (e.key === 'Delete' || e.key === 'Backspace') {
            e.preventDefault();
            onDelete();
            log.debug(SEG.UI, '[Canvas] DELETE/BACKSPACE pressed - deleting selected glyphs');
        }
    };

    container.addEventListener('keydown', handleKeydown, { signal: controller.signal });

    return controller;
}
