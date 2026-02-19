/**
 * Canvas Workspace Builder — reusable workspace DOM for root and subcanvas
 *
 * Extracted from canvas-glyph.ts renderContent() so both the root canvas
 * glyph and expanded subcanvas glyphs share the same workspace infrastructure:
 * content layer, spawn menu, selection, keyboard shortcuts, pan/zoom,
 * rectangle selection, glyph restoration, and composition restoration.
 */

import type { Glyph } from '../glyph';
import { Doc } from '@generated/sym.js';
import { log, SEG } from '../../../logger';
import { toast } from '../../../toast';
import { getGlyphTypeBySymbol, getGlyphTypeByElement } from '../glyph-registry';
import { createErrorGlyph } from '../error-glyph';
import { createResultGlyph, type PromptConfig } from '../result-glyph';
import { uploadFile } from '../../../api/files';
import { createDocGlyph, type DocGlyphContent } from '../doc-glyph';
import { uiState } from '../../../state/ui';
import { getMinimizeDuration } from '../glyph';
import { unmeldComposition, reconstructMeld } from '../meld/meld-system';
import { makeDraggable } from '../glyph-interaction';
import { showActionBar, hideActionBar } from './action-bar';
import { showSpawnMenu } from './spawn-menu';
import { setupKeyboardShortcuts } from './keyboard-shortcuts';
import { setupRectangleSelection, didRectangleSelectionJustComplete } from './rectangle-selection';
import { setupCanvasPan, resetTransform } from './canvas-pan';
import { getAllCompositions, removeComposition, extractGlyphIds } from '../../../state/compositions';
import { convertNoteToPrompt, convertResultToNote } from '../conversions';
import {
    hasSelection, selectionSize, getSelectedGlyphIds,
    addToSelection, removeFromSelection, replaceSelection, clearSelection,
    isGlyphSelected,
} from './selection';

/**
 * Select a glyph on the canvas.
 * - Normal click: Replace selection with this glyph
 * - Shift+click: Add/remove glyph from selection (toggle)
 */
function selectGlyph(canvasId: string, glyphId: string, container: HTMLElement, shiftKey: boolean): void {
    if (shiftKey) {
        if (isGlyphSelected(canvasId, glyphId)) {
            removeFromSelection(canvasId, glyphId);
            const el = container.querySelector(`[data-glyph-id="${glyphId}"]`) as HTMLElement | null;
            if (el) el.classList.remove('canvas-glyph-selected');
        } else {
            addToSelection(canvasId, glyphId);
            const el = container.querySelector(`[data-glyph-id="${glyphId}"]`) as HTMLElement | null;
            log.debug(SEG.GLYPH, '[Canvas] selectGlyph: Adding to selection', {
                glyphId, foundElement: !!el, elementClass: el?.className
            });
            if (el) el.classList.add('canvas-glyph-selected');
            else log.warn(SEG.GLYPH, '[Canvas] selectGlyph: Element not found', { glyphId });
        }
    } else {
        deselectAll(canvasId, container);
        replaceSelection(canvasId, [glyphId]);
        const el = container.querySelector(`[data-glyph-id="${glyphId}"]`) as HTMLElement | null;
        log.debug(SEG.GLYPH, '[Canvas] selectGlyph: Replace mode', {
            glyphId, foundElement: !!el, elementClass: el?.className
        });
        if (el) el.classList.add('canvas-glyph-selected');
        else log.warn(SEG.GLYPH, '[Canvas] selectGlyph: Element not found in replace mode', { glyphId });
    }

    const selectedIds = getSelectedGlyphIds(canvasId);
    log.debug(SEG.GLYPH, '[Canvas] selectGlyph: Checking action bar', {
        selectedCount: selectionSize(canvasId), selectedIds
    });
    if (hasSelection(canvasId)) {
        log.debug(SEG.GLYPH, '[Canvas] selectGlyph: Showing action bar');
        showActionBar(
            selectedIds,
            container,
            () => deleteSelectedGlyphs(canvasId, container),
            (composition) => unmeldSelectedGlyphs(canvasId, container, composition),
            () => convertNoteToPrompt(container, selectedIds[0]),
            () => convertResultToNote(container, selectedIds[0]),
        );
    } else {
        hideActionBar(container);
    }

    log.debug(SEG.GLYPH, `[Canvas] Selected ${selectionSize(canvasId)} glyphs`, { selectedIds });
}

/**
 * Create a Glyph object from a DOM element by detecting its type.
 */
function createGlyphFromElement(element: HTMLElement, id: string): Glyph {
    const entry = getGlyphTypeByElement(element);
    return {
        id,
        title: entry?.title ?? 'Glyph',
        symbol: entry?.symbol,
        renderContent: () => element,
    };
}

/** Deselect all glyphs and hide action bar */
function deselectAll(canvasId: string, container: HTMLElement): void {
    if (!hasSelection(canvasId)) return;
    const selected = container.querySelectorAll('.canvas-glyph-selected');
    selected.forEach(el => el.classList.remove('canvas-glyph-selected'));
    hideActionBar(container);
    clearSelection(canvasId);
}

/** Unmeld composition containing currently selected glyphs */
function unmeldFromSelection(canvasId: string, container: HTMLElement): void {
    if (!hasSelection(canvasId)) return;
    for (const glyphId of getSelectedGlyphIds(canvasId)) {
        const glyphEl = container.querySelector(`[data-glyph-id="${glyphId}"]`) as HTMLElement | null;
        if (!glyphEl) continue;
        const composition = glyphEl.closest('.melded-composition') as HTMLElement | null;
        if (composition) {
            unmeldSelectedGlyphs(canvasId, container, composition);
            return;
        }
    }
    log.debug(SEG.GLYPH, '[Canvas] No composition found for selected glyphs');
}

/** Unmeld selected glyphs that are in a melded composition */
function unmeldSelectedGlyphs(canvasId: string, container: HTMLElement, composition: HTMLElement): void {
    const result = unmeldComposition(composition);
    if (!result) {
        const compId = composition.dataset.glyphId || 'unknown';
        log.error(SEG.GLYPH, `[Canvas] Failed to unmeld composition ${compId}`);
        return;
    }
    const { glyphElements } = result;
    glyphElements.forEach((element) => {
        const glyphId = element.dataset.glyphId || element.getAttribute('data-glyph-id') || 'unknown';
        const glyph = createGlyphFromElement(element, glyphId);
        const entry = glyph.symbol ? getGlyphTypeBySymbol(glyph.symbol) : undefined;
        makeDraggable(element, element, glyph, { logLabel: entry?.label ?? 'Glyph' });
    });
    deselectAll(canvasId, container);
    log.debug(SEG.GLYPH, '[Canvas] Unmelded composition', {
        count: glyphElements.length,
        glyphIds: glyphElements.map(el => el.dataset.glyphId).filter(Boolean)
    });
}

/** Delete all currently selected glyphs from the canvas */
function deleteSelectedGlyphs(canvasId: string, container: HTMLElement): void {
    if (!hasSelection(canvasId)) return;
    const glyphIdsToDelete = getSelectedGlyphIds(canvasId);
    hideActionBar(container);
    clearSelection(canvasId);
    const duration = getMinimizeDuration();
    for (const glyphId of glyphIdsToDelete) {
        const el = container.querySelector(`[data-glyph-id="${glyphId}"]`) as HTMLElement | null;
        uiState.removeCanvasGlyph(glyphId);
        container.dispatchEvent(new CustomEvent('glyph-deleted', { detail: { glyphId } }));
        if (!el) continue;
        el.classList.remove('canvas-glyph-selected');
        if (duration === 0) { el.remove(); continue; }
        const animation = el.animate([
            { opacity: 1, transform: 'scale(1)' },
            { opacity: 0, transform: 'scale(0.85)' }
        ], { duration, easing: 'ease-in', fill: 'forwards' });
        animation.onfinish = () => { el.remove(); };
    }
    log.debug(SEG.GLYPH, `[Canvas] Deleted ${glyphIdsToDelete.length} glyphs`, { glyphIdsToDelete });
}

/**
 * Render a glyph on the canvas.
 * Uses glyph-registry for dispatch instead of per-type if/else.
 */
export async function renderGlyph(glyph: Glyph): Promise<HTMLElement> {
    log.debug(SEG.GLYPH, `[Canvas] Rendering glyph ${glyph.id}`, {
        symbol: glyph.symbol, hasContent: !!glyph.content
    });

    // Result glyphs: parse content JSON to get ExecutionResult
    if (glyph.symbol === 'result') {
        if (!glyph.content) {
            log.error(SEG.GLYPH, `[Canvas] Result glyph ${glyph.id} missing content`, {
                glyphId: glyph.id, position: { x: glyph.x, y: glyph.y },
                size: { width: glyph.width, height: glyph.height }
            });
            return createErrorGlyph(
                glyph.id, 'result',
                { x: glyph.x ?? 200, y: glyph.y ?? 200 },
                { type: 'missing_data', message: 'Execution result data missing',
                  details: { 'Has content': false, 'Position': `(${glyph.x}, ${glyph.y})`,
                    'Size': `${glyph.width}x${glyph.height}`,
                    'Cause': 'Glyph metadata saved without execution result (migration bug)' } }
            );
        }
        try {
            const parsed = JSON.parse(glyph.content);
            // Backwards-compatible: new format has .result, old is raw ExecutionResult
            const result = parsed.result ?? parsed;
            const promptConfig: PromptConfig | undefined = parsed.promptConfig;
            const prompt: string | undefined = parsed.prompt;
            return createResultGlyph(glyph, result, promptConfig, prompt);
        } catch (err) {
            log.error(SEG.GLYPH, `[Canvas] Result glyph ${glyph.id} has invalid JSON content`, err);
            return createErrorGlyph(
                glyph.id, 'result',
                { x: glyph.x ?? 200, y: glyph.y ?? 200 },
                { type: 'parse_failed', message: 'Failed to parse execution result JSON',
                  details: { 'Content length': glyph.content.length,
                    'Content preview': glyph.content.substring(0, 100) } }
            );
        }
    }

    // Look up glyph type in registry
    const entry = glyph.symbol ? getGlyphTypeBySymbol(glyph.symbol) : undefined;
    if (entry) return await entry.render(glyph);

    // Unknown glyph type → diagnostic error glyph
    log.error(SEG.GLYPH, `[Canvas] Unsupported glyph type: ${glyph.symbol}`, {
        glyphId: glyph.id, symbol: glyph.symbol, position: { x: glyph.x, y: glyph.y }
    });
    return createErrorGlyph(
        glyph.id, glyph.symbol ?? 'unknown',
        { x: glyph.x ?? 200, y: glyph.y ?? 200 },
        { type: 'unknown_type', message: `Glyph type '${glyph.symbol ?? 'unknown'}' not supported`,
          details: { 'Symbol': glyph.symbol ?? 'unknown', 'Position': `(${glyph.x}, ${glyph.y})`,
            'Cause': 'Glyph type not recognized by registry - check glyph-registry.ts' } }
    );
}

/**
 * Build a canvas workspace DOM element with full interaction support.
 *
 * Used by both the root canvas glyph and subcanvas when expanded to fullscreen.
 */
export function buildCanvasWorkspace(
    canvasId: string,
    glyphs: Glyph[]
): HTMLElement {
    const container = document.createElement('div');
    container.className = 'canvas-workspace';
    container.dataset.canvasId = canvasId;
    (container as any).__glyphs = glyphs;
    container.tabIndex = 0;

    container.style.width = '100%';
    container.style.height = '100%';
    container.style.position = 'relative';
    container.style.overflow = 'hidden';
    // background-color set via CSS (.canvas-workspace in canvas.css)
    container.style.outline = 'none';

    // Inner content layer that gets transformed (for pan)
    const contentLayer = document.createElement('div');
    contentLayer.className = 'canvas-content-layer';
    contentLayer.style.position = 'absolute';
    contentLayer.style.top = '0';
    contentLayer.style.left = '0';
    contentLayer.style.width = '100%';
    contentLayer.style.height = '100%';
    container.appendChild(contentLayer);

    // Right-click handler for spawn menu
    container.addEventListener('contextmenu', (e) => {
        e.preventDefault();
        showSpawnMenu(e.clientX, e.clientY, contentLayer, glyphs, canvasId);
    });

    // Prevent dblclick from bubbling past workspace boundary (stops re-morph on parent subcanvas)
    container.addEventListener('dblclick', (e) => { e.stopPropagation(); });

    // File drop: drag files onto canvas to create Doc glyphs
    container.addEventListener('dragover', (e) => {
        if (e.dataTransfer?.types.includes('Files')) {
            e.preventDefault();
            e.dataTransfer.dropEffect = 'copy';
        }
    });

    container.addEventListener('drop', (e) => {
        const files = e.dataTransfer?.files;
        if (!files || files.length === 0) return;
        e.preventDefault();

        const canvasRect = contentLayer.getBoundingClientRect();
        const baseX = Math.round(e.clientX - canvasRect.left);
        const baseY = Math.round(e.clientY - canvasRect.top);

        for (let i = 0; i < files.length; i++) {
            const file = files[i];
            const x = baseX + i * 30;
            const y = baseY + i * 30;

            void (async () => {
                try {
                    const result = await uploadFile(file);
                    const ext = file.name.includes('.') ? '.' + file.name.split('.').pop() : '';

                    const contentMeta: DocGlyphContent = {
                        fileId: result.id,
                        filename: result.filename,
                        ext,
                    };

                    const glyph: Glyph = {
                        id: `doc-${crypto.randomUUID()}`,
                        title: result.filename,
                        symbol: Doc,
                        x,
                        y,
                        content: JSON.stringify(contentMeta),
                        renderContent: () => document.createElement('div'),
                    };

                    glyphs.push(glyph);
                    const glyphElement = await createDocGlyph(glyph);
                    contentLayer.appendChild(glyphElement);

                    const rect = glyphElement.getBoundingClientRect();
                    uiState.addCanvasGlyph({
                        id: glyph.id,
                        symbol: Doc,
                        x,
                        y,
                        width: Math.round(rect.width),
                        height: Math.round(rect.height),
                        content: JSON.stringify(contentMeta),
                    });

                    log.info(SEG.GLYPH, `[Canvas] Spawned Doc glyph for ${result.filename} at (${x}, ${y})`);
                } catch (err) {
                    const message = err instanceof Error ? err.message : String(err);
                    log.error(SEG.GLYPH, `[Canvas] Failed to upload file ${file.name}`, { error: err });
                    toast.error(`Failed to upload ${file.name}: ${message}`);
                }
            })();
        }
    });

    // Selection: click on a glyph to select, Shift+click for multi-select, click background to deselect
    container.addEventListener('click', (e) => {
        const target = e.target as HTMLElement;

        // Close spawn menu if it exists
        const spawnMenu = document.querySelector('.canvas-spawn-menu');
        if (spawnMenu && !spawnMenu.contains(target)) spawnMenu.remove();

        // Ignore clicks on action bar, buttons, inputs, textareas, and contenteditable elements
        if (target.closest('.canvas-action-bar')) return;
        if (target.tagName === 'BUTTON' || target.tagName === 'INPUT' || target.tagName === 'TEXTAREA') return;
        if (target.isContentEditable || target.closest('[contenteditable="true"]')) return;

        // Focus container to enable keyboard shortcuts
        container.focus();

        // Walk up from click target to find a glyph element (must be inside this workspace)
        const glyphEl = target.closest('[data-glyph-id]') as HTMLElement | null;
        const isInsideComposition = glyphEl?.closest('.melded-composition') !== null;
        const isInsideWorkspace = glyphEl ? container.contains(glyphEl) : false;
        if (glyphEl && isInsideWorkspace && glyphEl.dataset.glyphId !== 'canvas-workspace' && !isInsideComposition) {
            const glyphId = glyphEl.dataset.glyphId;
            if (glyphId) {
                e.stopPropagation();
                selectGlyph(canvasId, glyphId, container, e.shiftKey);
            }
        } else {
            // Clicked on background — deselect (skip if rectangle selection just completed)
            if (!didRectangleSelectionJustComplete()) {
                deselectAll(canvasId, container);
            }
        }
    }, true);

    // Setup keyboard shortcuts (ESC, DELETE, U, 0 for reset view)
    void setupKeyboardShortcuts(
        container,
        () => hasSelection(canvasId),
        () => deselectAll(canvasId, container),
        () => deleteSelectedGlyphs(canvasId, container),
        () => unmeldFromSelection(canvasId, container),
        () => resetTransform(container, canvasId)
    );

    // Setup canvas pan (two-finger scroll on desktop, single finger drag on mobile)
    void setupCanvasPan(container, canvasId);

    // Setup rectangle selection
    // Always register — user may resize browser between mobile/desktop widths
    void setupRectangleSelection(
        container,
        (glyphId, cont, shiftKey) => selectGlyph(canvasId, glyphId, cont, shiftKey),
        (cont) => deselectAll(canvasId, cont)
    );

    // Clean up local glyphs array when a glyph is deleted
    container.addEventListener('glyph-deleted', ((e: CustomEvent<{ glyphId: string }>) => {
        const idx = glyphs.findIndex(g => g.id === e.detail.glyphId);
        if (idx !== -1) glyphs.splice(idx, 1);
    }) as EventListener);

    // Render existing glyphs asynchronously (to support py and ix glyphs)
    (async () => {
        // Step 1: Render all individual glyphs (skip minimized — they live in the tray)
        const minimizedIds = new Set(uiState.getMinimizedWindows());
        for (const glyph of glyphs) {
            if (minimizedIds.has(glyph.id)) continue;
            const glyphElement = await renderGlyph(glyph);
            contentLayer.appendChild(glyphElement);
        }

        // Step 2: Restore melded compositions after all glyphs are rendered
        const savedCompositions = getAllCompositions();
        log.debug(SEG.GLYPH, `[Canvas] Restoring ${savedCompositions.length} compositions from state`);

        for (const comp of savedCompositions) {
            // Skip and clean up invalid compositions (old format without edges)
            if (!comp.edges || !Array.isArray(comp.edges)) {
                log.warn(SEG.GLYPH, `[Canvas] Removing invalid composition ${comp.id} - old format (missing edges array)`);
                removeComposition(comp.id);
                continue;
            }

            const glyphIds = extractGlyphIds(comp.edges);
            const glyphElements = glyphIds
                .map(id => container.querySelector(`[data-glyph-id="${id}"]`) as HTMLElement)
                .filter(el => el !== null);

            if (glyphElements.length !== glyphIds.length) {
                log.warn(SEG.GLYPH, `[Canvas] Cannot restore composition ${comp.id} - missing glyphs`, {
                    glyphIds, foundCount: glyphElements.length, expectedCount: glyphIds.length
                });
                continue;
            }

            try {
                const composition = reconstructMeld(glyphElements, comp.edges, comp.id, comp.x, comp.y);
                const compositionGlyph: Glyph = {
                    id: comp.id,
                    title: 'Melded Composition',
                    renderContent: () => composition
                };
                makeDraggable(composition, composition, compositionGlyph, { logLabel: 'MeldedComposition' });
                log.debug(SEG.GLYPH, `[Canvas] Restored composition ${comp.id}`, {
                    edgeCount: comp.edges.length, glyphCount: glyphIds.length
                });
            } catch (err) {
                log.error(SEG.GLYPH, `[Canvas] Failed to restore composition ${comp.id}`, { error: err });
            }
        }
    })();

    return container;
}
