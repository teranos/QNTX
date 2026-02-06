/**
 * Canvas Glyph - Fractal container with spatial grid layout
 *
 * The canvas is a glyph that morphs to full-screen and contains other glyphs
 * arranged on a spatial grid. Right-click spawns new glyphs.
 *
 * Selection & Interaction:
 * - Click a glyph to select it (green outline, action bar appears at top)
 * - Shift+click to add/remove glyphs from selection (multi-select)
 * - Click canvas background to deselect
 * - Drag selected glyph(s) - all selected glyphs move together maintaining relative positions
 * - Action bar provides delete and unmeld (for melded compositions)
 *
 * Keyboard Shortcuts:
 * - ESC: deselect all glyphs
 * - DELETE or BACKSPACE: remove selected glyphs
 * - Shortcuts scoped to focused canvas (click to focus)
 *
 * This demonstrates the fractal principle: all glyphs are containers.
 */

import type { Glyph } from './glyph';
import { Pulse, IX, AX } from '@generated/sym.js';
import { log, SEG } from '../../logger';
import { createGridGlyph } from './grid-glyph';
import { createResultGlyph, type ExecutionResult } from './result-glyph';
import { uiState } from '../../state/ui';
import { getMinimizeDuration } from './glyph';
import { unmeldComposition } from './meld-system';
import { showActionBar, hideActionBar } from './canvas/action-bar';
import { showSpawnMenu } from './canvas/spawn-menu';

// ============================================================================
// Selection State
// ============================================================================

/** Currently selected glyph IDs (empty = nothing selected) */
let selectedGlyphIds: string[] = [];

/**
 * Check if a glyph is currently selected
 */
export function isGlyphSelected(glyphId: string): boolean {
    return selectedGlyphIds.includes(glyphId);
}

/**
 * Get all currently selected glyph IDs
 */
export function getSelectedGlyphIds(): string[] {
    return [...selectedGlyphIds];
}

/**
 * Get all selected glyph elements from the canvas
 */
export function getSelectedGlyphElements(container: HTMLElement): HTMLElement[] {
    return selectedGlyphIds
        .map(id => container.querySelector(`[data-glyph-id="${id}"]`) as HTMLElement | null)
        .filter((el): el is HTMLElement => el !== null);
}

/**
 * Select a glyph on the canvas.
 * - Normal click: Replace selection with this glyph
 * - Shift+click: Add/remove glyph from selection (toggle)
 */
function selectGlyph(glyphId: string, container: HTMLElement, shiftKey: boolean): void {
    if (shiftKey) {
        // Toggle glyph in selection
        const idx = selectedGlyphIds.indexOf(glyphId);
        if (idx !== -1) {
            // Already selected — deselect it
            selectedGlyphIds.splice(idx, 1);
            const el = container.querySelector(`[data-glyph-id="${glyphId}"]`) as HTMLElement | null;
            if (el) {
                el.classList.remove('canvas-glyph-selected');
            }
        } else {
            // Not selected — add to selection
            selectedGlyphIds.push(glyphId);
            const el = container.querySelector(`[data-glyph-id="${glyphId}"]`) as HTMLElement | null;
            if (el) {
                el.classList.add('canvas-glyph-selected');
            }
        }
    } else {
        // Replace selection
        deselectAll(container);
        selectedGlyphIds = [glyphId];
        const el = container.querySelector(`[data-glyph-id="${glyphId}"]`) as HTMLElement | null;
        if (el) {
            el.classList.add('canvas-glyph-selected');
        }
    }

    // Show/hide action bar based on selection
    if (selectedGlyphIds.length > 0) {
        showActionBar(
            selectedGlyphIds,
            container,
            () => deleteSelectedGlyphs(container),
            (composition) => unmeldSelectedGlyphs(container, composition)
        );
    } else {
        hideActionBar();
    }

    log.debug(SEG.UI, `[Canvas] Selected ${selectedGlyphIds.length} glyphs`, { selectedGlyphIds });
}

/**
 * Deselect all glyphs and hide action bar
 */
function deselectAll(container: HTMLElement): void {
    if (selectedGlyphIds.length === 0) return;

    const selected = container.querySelectorAll('.canvas-glyph-selected');
    selected.forEach(el => el.classList.remove('canvas-glyph-selected'));

    hideActionBar();
    selectedGlyphIds = [];
}

/**
 * Unmeld selected glyphs that are in a melded composition
 */
function unmeldSelectedGlyphs(container: HTMLElement, composition: HTMLElement): void {
    const result = unmeldComposition(composition);
    if (!result) {
        log.error(SEG.UI, '[Canvas] Failed to unmeld composition');
        return;
    }

    const { axElement, promptElement, axId, promptId } = result;

    // Restore drag handlers on the unmelded glyphs
    const axGlyph: Glyph = {
        id: axId,
        title: 'AX Query',
        renderContent: () => axElement
    };

    const promptGlyph: Glyph = {
        id: promptId,
        title: 'Prompt',
        symbol: SO,
        renderContent: () => promptElement
    };

    makeDraggable(axElement, axElement, axGlyph, { logLabel: 'AX' });
    makeDraggable(promptElement, promptElement, promptGlyph, { logLabel: 'Prompt' });

    // Clear selection and hide action bar
    deselectAll(container);

    log.debug(SEG.UI, '[Canvas] Unmelded composition', { axId, promptId });
}

/**
 * Delete all currently selected glyphs from the canvas
 * Animates scale-down + fade-out before removal (respects reduced motion)
 */
function deleteSelectedGlyphs(container: HTMLElement): void {
    if (selectedGlyphIds.length === 0) return;

    const glyphIdsToDelete = [...selectedGlyphIds];

    // Clear selection immediately (prevent double-delete)
    hideActionBar();
    selectedGlyphIds = [];

    const duration = getMinimizeDuration();

    // Delete each selected glyph
    for (const glyphId of glyphIdsToDelete) {
        const el = container.querySelector(`[data-glyph-id="${glyphId}"]`) as HTMLElement | null;

        // Remove from persisted state and local array immediately
        uiState.removeCanvasGlyph(glyphId);
        container.dispatchEvent(new CustomEvent('glyph-deleted', {
            detail: { glyphId }
        }));

        if (!el) continue;

        if (duration === 0) {
            el.remove();
            continue;
        }

        // Animate out, then remove
        const animation = el.animate([
            { opacity: 1, transform: 'scale(1)' },
            { opacity: 0, transform: 'scale(0.85)' }
        ], {
            duration,
            easing: 'ease-in',
            fill: 'forwards'
        });

        animation.onfinish = () => {
            el.remove();
        };
    }

    log.debug(SEG.UI, `[Canvas] Deleted ${glyphIdsToDelete.length} glyphs`, { glyphIdsToDelete });
}

/**
 * Factory function to create a Canvas glyph
 */
export function createCanvasGlyph(): Glyph {
    // Load persisted glyphs from uiState
    const savedGlyphs = uiState.getCanvasGlyphs();
    log.debug(SEG.UI, `[Canvas] Restoring ${savedGlyphs.length} glyphs from state`);

    const glyphs: Glyph[] = savedGlyphs.map(saved => {
        // For ax glyphs, recreate using factory function to restore full functionality
        if (saved.symbol === AX) {
            const axGlyph = createAxGlyph(saved.id, '', saved.x, saved.y);
            axGlyph.width = saved.width;
            axGlyph.height = saved.height;
            return axGlyph;
        }

        if (saved.symbol === 'result') {
            log.debug(SEG.UI, `[Canvas] Restoring result glyph ${saved.id}`, {
                hasResult: !!saved.result,
                x: saved.x,
                y: saved.y
            });
        }

        return {
            id: saved.id,
            title: saved.symbol === 'result' ? 'Python Result' : 'Pulse Schedule',
            symbol: saved.symbol,
            x: saved.x,
            y: saved.y,
            width: saved.width,   // Restore custom size if saved
            height: saved.height,
            result: saved.result, // For result glyphs
            // TODO: Clarify if grid glyphs should display content
            renderContent: () => {
                const content = document.createElement('div');
                content.textContent = 'Pulse glyph content (TBD)';
                return content;
            }
        };
    });

    return {
        id: 'canvas-workspace',
        title: 'Canvas',
        manifestationType: 'fullscreen', // Full-viewport, no chrome
        layoutStrategy: 'grid',
        children: glyphs,
        onSpawnMenu: () => [Pulse, IX, AX], // TODO: Remove Pulse when IX wired up

        renderContent: () => {
            const container = document.createElement('div');
            container.className = 'canvas-workspace';
            container.tabIndex = 0; // Make focusable for keyboard events

            // Full-screen, no padding
            container.style.width = '100%';
            container.style.height = '100%';
            container.style.position = 'relative';
            container.style.overflow = 'hidden';
            container.style.backgroundColor = '#2a2b2a'; // Mid-dark gray for night work
            container.style.outline = 'none'; // Remove focus outline

            // Add subtle grid overlay
            const gridOverlay = document.createElement('div');
            gridOverlay.className = 'canvas-grid-overlay';
            container.appendChild(gridOverlay);

            // Right-click handler for spawn menu
            container.addEventListener('contextmenu', (e) => {
                e.preventDefault();
                showSpawnMenu(e.clientX, e.clientY, container, glyphs);
            });

            // Selection: click on a glyph to select, Shift+click for multi-select, click background to deselect
            container.addEventListener('click', (e) => {
                const target = e.target as HTMLElement;

                // Close spawn menu if it exists
                const spawnMenu = document.querySelector('.canvas-spawn-menu');
                if (spawnMenu && !spawnMenu.contains(target)) {
                    spawnMenu.remove();
                }

                // Ignore clicks on action bar
                if (target.closest('.canvas-action-bar')) {
                    return;
                }

                // Ignore clicks on buttons, inputs, and textareas (allow interactive elements to work)
                if (target.tagName === 'BUTTON' || target.tagName === 'INPUT' || target.tagName === 'TEXTAREA') {
                    return;
                }

                // Focus container to enable keyboard shortcuts
                container.focus();

                // Walk up from click target to find a glyph element
                const glyphEl = target.closest('[data-glyph-id]') as HTMLElement | null;

                // Exclude canvas-workspace itself from being selectable
                if (glyphEl && glyphEl.dataset.glyphId !== 'canvas-workspace') {
                    const glyphId = glyphEl.dataset.glyphId;
                    if (glyphId) {
                        e.stopPropagation();
                        selectGlyph(glyphId, container, e.shiftKey);
                    }
                } else {
                    // Clicked on background (not a glyph) — deselect
                    deselectAll(container);
                }
            }, true);

            // Keyboard support: DELETE/BACKSPACE to delete, ESC to deselect
            // Scoped to this canvas container (not document-level)
            container.addEventListener('keydown', (e) => {
                // Ignore if user is typing in an input/textarea
                const target = e.target as HTMLElement;
                if (target.tagName === 'INPUT' || target.tagName === 'TEXTAREA' || target.isContentEditable) {
                    return;
                }

                // ESC to deselect
                if (e.key === 'Escape') {
                    if (selectedGlyphIds.length > 0) {
                        e.preventDefault();
                        deselectAll(container);
                    }
                    return;
                }

                // DELETE/BACKSPACE to delete selected glyphs
                if (selectedGlyphIds.length === 0) {
                    return;
                }

                if (e.key === 'Delete' || e.key === 'Backspace') {
                    e.preventDefault();
                    deleteSelectedGlyphs(container);
                }
            });

            // Clean up local glyphs array when a glyph is deleted
            container.addEventListener('glyph-deleted', ((e: CustomEvent<{ glyphId: string }>) => {
                const idx = glyphs.findIndex(g => g.id === e.detail.glyphId);
                if (idx !== -1) {
                    glyphs.splice(idx, 1);
                }
            }) as EventListener);

            // Render existing glyphs asynchronously (to support py and ix glyphs)
            (async () => {
                for (const glyph of glyphs) {
                    const glyphElement = await renderGlyph(glyph);
                    container.appendChild(glyphElement);
                }
            })();

            return container;
        }
    };
}

/**
 * Render a glyph on the canvas
 * Checks symbol type and creates appropriate glyph element
 */
async function renderGlyph(glyph: Glyph): Promise<HTMLElement> {
    log.debug(SEG.UI, `[Canvas] Rendering glyph ${glyph.id}`, {
        symbol: glyph.symbol,
        hasResult: !!glyph.result
    });

    // For py glyphs, create full editor
    if (glyph.symbol === 'py') {
        return await createPyGlyph(glyph);
    }

    // For IX glyphs, create full form
    if (glyph.symbol === IX) {
        return await createIxGlyph(glyph);
    }

    // For prompt glyphs, create template editor
    if (glyph.symbol === SO) {
        return await createPromptGlyph(glyph);
    }

    // For AX glyphs, render content directly (they handle their own rendering)
    if (glyph.symbol === AX) {
        return glyph.renderContent();
    }

    // For result glyphs, create result display
    if (glyph.symbol === 'result' && glyph.result) {
        log.debug(SEG.UI, `[Canvas] Creating result glyph for ${glyph.id}`);
        return createResultGlyph(glyph, glyph.result as ExecutionResult);
    }

    // Otherwise create simple grid glyph
    return createGridGlyph(glyph);
}
