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
import { Pulse, IX, AX, SO, Prose } from '@generated/sym.js';
import { log, SEG } from '../../logger';
import { createResultGlyph, type ExecutionResult } from './result-glyph';
import { createAxGlyph } from './ax-glyph';
import { createIxGlyph } from './ix-glyph';
import { createPyGlyph } from './py-glyph';
import { createPromptGlyph } from './prompt-glyph';
import { createNoteGlyph } from './note-glyph';
import { createErrorGlyph } from './error-glyph';
import { uiState } from '../../state/ui';
import { getMinimizeDuration } from './glyph';
import { unmeldComposition, reconstructMeld } from './meld-system';
import { makeDraggable } from './glyph-interaction';
import { showActionBar, hideActionBar } from './canvas/action-bar';
import { showSpawnMenu } from './canvas/spawn-menu';
import { setupKeyboardShortcuts } from './canvas/keyboard-shortcuts';
import { setupRectangleSelection, didRectangleSelectionJustComplete } from './canvas/rectangle-selection';
import { getAllCompositions, removeComposition, extractGlyphIds } from '../../state/compositions';
import { convertNoteToPrompt, convertResultToNote } from './conversions';

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
            log.debug(SEG.GLYPH, '[Canvas] selectGlyph: Adding to selection', {
                glyphId,
                foundElement: !!el,
                elementClass: el?.className
            });
            if (el) {
                el.classList.add('canvas-glyph-selected');
            } else {
                log.warn(SEG.GLYPH, '[Canvas] selectGlyph: Element not found', { glyphId });
            }
        }
    } else {
        // Replace selection
        deselectAll(container);
        selectedGlyphIds = [glyphId];
        const el = container.querySelector(`[data-glyph-id="${glyphId}"]`) as HTMLElement | null;
        log.debug(SEG.GLYPH, '[Canvas] selectGlyph: Replace mode', {
            glyphId,
            foundElement: !!el,
            elementClass: el?.className
        });
        if (el) {
            el.classList.add('canvas-glyph-selected');
        } else {
            log.warn(SEG.GLYPH, '[Canvas] selectGlyph: Element not found in replace mode', { glyphId });
        }
    }

    // Show/hide action bar based on selection
    log.debug(SEG.GLYPH, '[Canvas] selectGlyph: Checking action bar', {
        selectedCount: selectedGlyphIds.length,
        selectedIds: selectedGlyphIds
    });
    if (selectedGlyphIds.length > 0) {
        log.debug(SEG.GLYPH, '[Canvas] selectGlyph: Showing action bar');
        showActionBar(
            selectedGlyphIds,
            container,
            () => deleteSelectedGlyphs(container),
            (composition) => unmeldSelectedGlyphs(container, composition),
            () => convertNoteToPrompt(container, selectedGlyphIds[0]),
            () => convertResultToNote(container, selectedGlyphIds[0]),
        );
    } else {
        hideActionBar();
    }

    log.debug(SEG.GLYPH, `[Canvas] Selected ${selectedGlyphIds.length} glyphs`, { selectedGlyphIds });
}

/**
 * Create a Glyph object from a DOM element by detecting its type
 * Used when restoring glyphs after unmeld
 */
function createGlyphFromElement(element: HTMLElement, id: string): Glyph {
    if (element.classList.contains('canvas-ax-glyph')) {
        return { id, title: 'AX Query', symbol: AX, renderContent: () => element };
    }
    if (element.classList.contains('canvas-ix-glyph')) {
        return { id, title: 'Ingest', symbol: IX, renderContent: () => element };
    }
    if (element.classList.contains('canvas-py-glyph')) {
        return { id, title: 'Python', symbol: 'py', renderContent: () => element };
    }
    if (element.classList.contains('canvas-prompt-glyph')) {
        return { id, title: 'Prompt', symbol: SO, renderContent: () => element };
    }
    if (element.classList.contains('canvas-note-glyph')) {
        return { id, title: 'Note', symbol: Prose, renderContent: () => element };
    }
    if (element.classList.contains('canvas-result-glyph')) {
        return { id, title: 'Result', symbol: 'result', renderContent: () => element };
    }
    // Fallback
    return { id, title: 'Glyph', renderContent: () => element };
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
        const compId = composition.dataset.glyphId || 'unknown';
        log.error(SEG.GLYPH, `[Canvas] Failed to unmeld composition ${compId}`);
        return;
    }

    const { glyphElements } = result;

    // Restore drag handlers on each unmelded glyph
    glyphElements.forEach((element) => {
        const glyphId = element.dataset.glyphId || element.getAttribute('data-glyph-id') || 'unknown';
        const glyph = createGlyphFromElement(element, glyphId);

        // Determine log label from glyph symbol
        const label = glyph.symbol === AX ? 'AX' : glyph.symbol === SO ? 'Prompt' : 'Py';

        makeDraggable(element, element, glyph, { logLabel: label });
    });

    // Clear selection and hide action bar
    deselectAll(container);

    log.debug(SEG.GLYPH, '[Canvas] Unmelded composition', {
        count: glyphElements.length,
        glyphIds: glyphElements.map(el => el.dataset.glyphId).filter(Boolean)
    });
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

        // Explicitly remove selection class for clean visual transition
        el.classList.remove('canvas-glyph-selected');

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

    log.debug(SEG.GLYPH, `[Canvas] Deleted ${glyphIdsToDelete.length} glyphs`, { glyphIdsToDelete });
}

/**
 * Factory function to create a Canvas glyph
 */
export function createCanvasGlyph(): Glyph {
    // Load persisted glyphs from uiState
    const allSavedGlyphs = uiState.getCanvasGlyphs();

    // Filter out error glyphs (ephemeral - should never be persisted)
    const errorGlyphs = allSavedGlyphs.filter(g => g.symbol === 'error');
    if (errorGlyphs.length > 0) {
        log.warn(SEG.GLYPH, `[Canvas] Removing ${errorGlyphs.length} persisted error glyphs (should be ephemeral)`, {
            ids: errorGlyphs.map(g => g.id)
        });
        errorGlyphs.forEach(g => uiState.removeCanvasGlyph(g.id));
    }

    const savedGlyphs = allSavedGlyphs.filter(g => g.symbol !== 'error');
    const resultCount = savedGlyphs.filter(g => g.symbol === 'result').length;
    log.debug(SEG.GLYPH, `[Canvas] Restoring ${savedGlyphs.length} glyphs from state (${resultCount} result glyphs)`, {
        symbols: savedGlyphs.map(g => g.symbol),
        resultGlyphs: savedGlyphs.filter(g => g.symbol === 'result').map(g => ({
            id: g.id,
            hasResult: !!g.result
        }))
    });

    const glyphs: Glyph[] = savedGlyphs.map(saved => {
        // For ax glyphs, recreate using factory function to restore full functionality
        if (saved.symbol === AX) {
            const axGlyph = createAxGlyph(saved.id, '', saved.x, saved.y);
            axGlyph.width = saved.width;
            axGlyph.height = saved.height;
            return axGlyph;
        }

        if (saved.symbol === 'result') {
            log.debug(SEG.GLYPH, `[Canvas] Restoring result glyph ${saved.id}`, {
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
            // Placeholder renderContent - result glyphs render via createResultGlyph() instead
            // TODO: Clarify if Pulse glyphs should display content
            renderContent: () => {
                const content = document.createElement('div');
                content.textContent = saved.symbol === 'result'
                    ? 'Result placeholder (should not be visible)'
                    : 'Pulse glyph content (TBD)';
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
            container.style.backgroundColor = 'var(--bg-dark-hover)';
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

                // Ignore clicks on buttons, inputs, textareas, and contenteditable elements (allow interactive elements to work)
                // This includes ProseMirror and CodeMirror editors which use contenteditable divs
                if (target.tagName === 'BUTTON' || target.tagName === 'INPUT' || target.tagName === 'TEXTAREA') {
                    return;
                }

                // Check if click is inside a contenteditable element (editors)
                if (target.isContentEditable || target.closest('[contenteditable="true"]')) {
                    return;
                }

                // Focus container to enable keyboard shortcuts
                container.focus();

                // Walk up from click target to find a glyph element
                const glyphEl = target.closest('[data-glyph-id]') as HTMLElement | null;

                // Exclude canvas-workspace itself and glyphs inside compositions from being selectable
                const isInsideComposition = glyphEl?.closest('.melded-composition') !== null;
                if (glyphEl && glyphEl.dataset.glyphId !== 'canvas-workspace' && !isInsideComposition) {
                    const glyphId = glyphEl.dataset.glyphId;
                    if (glyphId) {
                        e.stopPropagation();
                        selectGlyph(glyphId, container, e.shiftKey);
                    }
                } else {
                    // Clicked on background (not a glyph) — deselect
                    // But skip if rectangle selection just completed (to avoid immediate deselection)
                    if (!didRectangleSelectionJustComplete()) {
                        deselectAll(container);
                    }
                }
            }, true);

            // Setup keyboard shortcuts (ESC to deselect, DELETE/BACKSPACE to delete)
            // AbortController signal auto-cleans up when container is removed from DOM
            void setupKeyboardShortcuts(
                container,
                () => selectedGlyphIds.length > 0,
                () => deselectAll(container),
                () => deleteSelectedGlyphs(container)
            );
            // Note: AbortController returned but not stored - signal handles cleanup automatically
            // Future: if we add explicit canvas.destroy(), store and call .abort()

            // Setup rectangle selection (drag on canvas background to select glyphs)
            void setupRectangleSelection(
                container,
                selectGlyph,
                deselectAll
            );

            // Clean up local glyphs array when a glyph is deleted
            container.addEventListener('glyph-deleted', ((e: CustomEvent<{ glyphId: string }>) => {
                const idx = glyphs.findIndex(g => g.id === e.detail.glyphId);
                if (idx !== -1) {
                    glyphs.splice(idx, 1);
                }
            }) as EventListener);

            // Render existing glyphs asynchronously (to support py and ix glyphs)
            (async () => {
                // Step 1: Render all individual glyphs
                for (const glyph of glyphs) {
                    const glyphElement = await renderGlyph(glyph);
                    container.appendChild(glyphElement);
                }

                // Step 2: Restore melded compositions after all glyphs are rendered
                const savedCompositions = getAllCompositions();
                log.debug(SEG.GLYPH, `[Canvas] Restoring ${savedCompositions.length} compositions from state`);

                for (const comp of savedCompositions) {
                    // TODO: Remove this guard after Feb 2026 (migration to edge-based DAG structure)
                    // Skip and clean up invalid compositions (old format without edges)
                    // This handles stale IndexedDB data from before PR #443 schema change
                    if (!comp.edges || !Array.isArray(comp.edges)) {
                        log.warn(SEG.GLYPH, `[Canvas] Removing invalid composition ${comp.id} - old format (missing edges array)`);
                        removeComposition(comp.id);
                        continue;
                    }

                    // Extract glyph IDs from edges (DAG-native)
                    const glyphIds = extractGlyphIds(comp.edges);

                    // Find all glyph elements in the DOM
                    const glyphElements = glyphIds
                        .map(id => container.querySelector(`[data-glyph-id="${id}"]`) as HTMLElement)
                        .filter(el => el !== null);

                    if (glyphElements.length !== glyphIds.length) {
                        log.warn(SEG.GLYPH, `[Canvas] Cannot restore composition ${comp.id} - missing glyphs`, {
                            glyphIds,
                            foundCount: glyphElements.length,
                            expectedCount: glyphIds.length
                        });
                        continue;
                    }

                    // Reconstruct the composition DOM (without persisting)
                    try {
                        const composition = reconstructMeld(glyphElements, comp.id, comp.x, comp.y);

                        // Make the restored composition draggable
                        const compositionGlyph: Glyph = {
                            id: comp.id,
                            title: 'Melded Composition',
                            renderContent: () => composition
                        };
                        makeDraggable(composition, composition, compositionGlyph, {
                            logLabel: 'MeldedComposition'
                        });

                        log.debug(SEG.GLYPH, `[Canvas] Restored composition ${comp.id}`, {
                            edgeCount: comp.edges.length,
                            glyphCount: glyphIds.length
                        });
                    } catch (err) {
                        log.error(SEG.GLYPH, `[Canvas] Failed to restore composition ${comp.id}`, { error: err });
                    }
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
    log.debug(SEG.GLYPH, `[Canvas] Rendering glyph ${glyph.id}`, {
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

    // For note glyphs, create markdown editor
    if (glyph.symbol === Prose) {
        return await createNoteGlyph(glyph);
    }

    // For AX glyphs, render content directly (they handle their own rendering)
    if (glyph.symbol === AX) {
        return glyph.renderContent();
    }

    // For result glyphs, create result display
    if (glyph.symbol === 'result') {
        if (glyph.result) {
            log.debug(SEG.GLYPH, `[Canvas] Creating result glyph for ${glyph.id}`);
            return createResultGlyph(glyph, glyph.result as ExecutionResult);
        }

        // Result glyph without execution data - spawn error glyph (ephemeral)
        log.error(SEG.GLYPH, `[Canvas] Result glyph ${glyph.id} missing execution data`, {
            glyphId: glyph.id,
            hasResult: !!glyph.result,
            position: { x: glyph.x, y: glyph.y },
            size: { width: glyph.width, height: glyph.height }
        });

        return createErrorGlyph(
            glyph.id,
            'result',
            { x: glyph.x ?? 200, y: glyph.y ?? 200 },
            {
                type: 'missing_data',
                message: 'Execution result data missing',
                details: {
                    'Has result': !!glyph.result,
                    'Position': `(${glyph.x}, ${glyph.y})`,
                    'Size': `${glyph.width}x${glyph.height}`,
                    'Cause': 'Glyph metadata saved without execution result (migration bug)'
                }
            }
        );
    }

    // Unsupported glyph type - spawn error glyph (ephemeral)
    log.error(SEG.GLYPH, `[Canvas] Unsupported glyph type: ${glyph.symbol}`, {
        glyphId: glyph.id,
        symbol: glyph.symbol,
        position: { x: glyph.x, y: glyph.y }
    });

    return createErrorGlyph(
        glyph.id,
        glyph.symbol ?? 'unknown',
        { x: glyph.x ?? 200, y: glyph.y ?? 200 },
        {
            type: 'unknown_type',
            message: `Glyph type '${glyph.symbol ?? 'unknown'}' not supported`,
            details: {
                'Symbol': glyph.symbol ?? 'unknown',
                'Position': `(${glyph.x}, ${glyph.y})`,
                'Cause': 'Glyph type not implemented in renderGlyph() - check canvas-glyph.ts'
            }
        }
    );
}
