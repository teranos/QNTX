/**
 * Canvas Keyboard Shortcuts
 *
 * Handles keyboard interactions for canvas operations:
 * - h/j/k/l: directional glyph selection (left/down/up/right)
 * - ENTER: focus the follow-up textarea of the selected glyph
 * - ESC: deselect all glyphs (or blur follow-up textarea)
 * - DELETE/BACKSPACE: remove selected glyphs
 * - U: unmeld selected composition
 * - 0: reset zoom/pan to origin
 *
 * Shortcuts listen on document (no focus/click required)
 * Uses AbortController for automatic cleanup when container is removed
 */

import { log, SEG } from '../../../logger';
import { isInputFocused } from '../../../keyboard';

/**
 * Callback to check if any glyphs are selected
 */
export type HasSelectionCallback = () => boolean;

export type Direction = 'left' | 'down' | 'up' | 'right';

/**
 * Setup keyboard shortcuts for canvas container
 * Uses AbortController for automatic cleanup - signal will abort when container is removed
 */
export function setupKeyboardShortcuts(
    _container: HTMLElement,
    hasSelection: HasSelectionCallback,
    onDeselect: () => void,
    onDelete: () => void,
    onUnmeld: () => void,
    onResetView: () => void,
    onNavigate: (direction: Direction) => void,
    onEnterGlyph: () => void,
    onThreadNavigate: (direction: Direction) => boolean,
): AbortController {
    const controller = new AbortController();

    const handleKeydown = (e: KeyboardEvent) => {
        if (isInputFocused(e.target)) return;
        // Only fire for the active (connected, topmost) workspace
        if (!_container.isConnected) return;

        // Arrow keys — thread navigation when selected glyph is on a spine;
        // falls through to spatial navigation otherwise. onThreadNavigate
        // returns true if it handled the key.
        const arrowMap: Record<string, Direction> = {
            ArrowLeft: 'left', ArrowDown: 'down', ArrowUp: 'up', ArrowRight: 'right',
        };
        const arrowDir = arrowMap[e.key];
        if (arrowDir) {
            e.preventDefault();
            if (!onThreadNavigate(arrowDir)) {
                onNavigate(arrowDir);
            }
            return;
        }

        // h/j/k/l — directional glyph navigation (spatial only)
        const dirMap: Record<string, Direction> = { h: 'left', j: 'down', k: 'up', l: 'right' };
        const dir = dirMap[e.key];
        if (dir) {
            e.preventDefault();
            onNavigate(dir);
            return;
        }

        // ENTER — focus the selected glyph's follow-up textarea
        if (e.key === 'Enter' && hasSelection()) {
            e.preventDefault();
            onEnterGlyph();
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
    };

    document.addEventListener('keydown', handleKeydown, { signal: controller.signal });

    return controller;
}
