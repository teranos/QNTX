/**
 * Canvas Element - Fractal container with spatial grid layout
 *
 * The canvas is an element that morphs to full-screen and contains other elements
 * arranged on a spatial grid. Right-click spawns new elements.
 *
 * Selection & Interaction:
 * - Click an element to select it (green outline, action bar appears at top)
 * - Shift+click to add/remove elements from selection (multi-select)
 * - Click canvas background to deselect
 * - Drag selected element(s) - all selected elements move together maintaining relative positions
 * - Action bar provides delete and unmeld (for melded compositions)
 *
 * Keyboard Shortcuts:
 * - ESC: deselect all elements
 * - DELETE or BACKSPACE: remove selected elements
 * - Shortcuts scoped to focused canvas (click to focus)
 *
 * This demonstrates the fractal principle: all elements are containers.
 */

import type { Element } from '@teranos/elements';
import { Pulse, AX } from '../../../sym';
import { log, SEG } from '../../../logger';
import { getElementTypeBySavedSymbol } from '../element-registry';
import { uiState } from '../../../state/ui';
import { buildCanvasWorkspace } from './canvas-workspace-builder';

/**
 * Factory function to create a Canvas element
 */
export function createCanvasElement(): Element {
    // Load persisted elements from uiState
    const allSavedElements = uiState.getCanvasElements('canvas-workspace');

    // Filter out error elements (ephemeral - should never be persisted)
    const errorElements = allSavedElements.filter(g => g.symbol === 'error');
    if (errorElements.length > 0) {
        log.warn(SEG.ELEMENT, `[Canvas] Removing ${errorElements.length} persisted error elements (should be ephemeral)`, {
            ids: errorElements.map(g => g.id)
        });
        errorElements.forEach(g => uiState.removeCanvasElement(g.id));
    }

    const savedElements = allSavedElements.filter(g => g.symbol !== 'error');
    const resultCount = savedElements.filter(g => g.symbol === 'result').length;
    log.debug(SEG.ELEMENT, `[Canvas] Restoring ${savedElements.length} elements from state (${resultCount} result elements)`, {
        symbols: savedElements.map(g => g.symbol),
        resultElements: savedElements.filter(g => g.symbol === 'result').map(g => ({
            id: g.id,
            hasContent: !!g.content
        }))
    });

    const items: Element[] = savedElements.map(saved => {
        if (saved.symbol === 'result') {
            log.debug(SEG.ELEMENT, `[Canvas] Restoring result element ${saved.id}`, {
                hasContent: !!saved.content,
                x: saved.x,
                y: saved.y
            });
        }

        const entry = saved.symbol ? getElementTypeBySavedSymbol(saved.symbol, saved.content) : undefined;
        return {
            id: saved.id,
            title: entry?.title ?? 'Element',
            symbol: saved.symbol,
            x: saved.x,
            y: saved.y,
            width: saved.width,
            height: saved.height,
            content: saved.content,
            renderContent: () => document.createElement('div'),
        };
    });

    return {
        id: 'canvas-workspace',
        title: 'Canvas',
        opensAs: 'workspace', // Full-viewport, no chrome
        layoutStrategy: 'grid',
        children: items,
        onSpawnMenu: () => [Pulse, AX],

        renderContent: () => buildCanvasWorkspace('canvas-workspace', items)
    };
}
