/**
 * Canvas Workspace Builder — reusable workspace DOM for root and subcanvas
 *
 * Extracted from canvas-element.ts renderContent() so both the root canvas
 * element and expanded subcanvas elements share the same workspace infrastructure:
 * content layer, spawn menu, selection, keyboard shortcuts, pan/zoom,
 * rectangle selection, element restoration, and composition restoration.
 */

import type { Element } from '@teranos/elements';
import { Doc } from '../../../sym';
import { log, SEG } from '../../../logger';
import { toast } from '../../../toast';
import { getElementTypeBySymbol, getElementTypeBySavedSymbol, getElementTypeByElement } from '../element-registry';
import { createErrorElement } from '../error-element';
import { setResponseState } from '../response-state';
import { createAbsentElement, askAbsentAgain } from '../absent-element';
import { getPluginNameBySymbol } from '../plugin-provided-elements';
import { createResultElement, type ExecutionResult, type PromptConfig } from '../result-element';
import type { SpawnResultDetail } from '../element-ui';
import { uploadFile } from '../../../api/files';

import { uiState } from '../../../state/ui';
import { getRestDuration } from '@teranos/elements';
import { unmeldComposition, reconstructMeld, detachElement } from '@teranos/elements';
import { autoMeldResultBelow } from '../meld/auto-meld-result';
import { makeDraggable, runCleanup } from '@teranos/elements';
import { showActionBar, hideActionBar } from './action-bar';
import { showSpawnMenu, isSpawnMenuOpen } from './spawn-menu';
import { isPlacementActive } from './placement-mode';
import { addSpine, removeSpine, getSpineByNode } from './spine-renderer';
import { enterThreadBuildingMode } from './thread-line';
import { pinThreadElement, unpinThreadElement } from '../thread-element';
import { navigateThread } from './thread-navigation';
import { setupKeyboardShortcuts } from './keyboard-shortcuts';
import { setupRectangleSelection, didRectangleSelectionJustComplete } from './rectangle-selection';
import { setupCanvasPan, resetTransform, panToElement, centerOnElementSymbol, screenToCanvas, getTransform } from './canvas-pan';
import { getAllCompositions, removeComposition, extractElementIds } from '../../../state/compositions';
import { convertNoteToPrompt, convertResultToNote } from '../conversions';
import {
    hasSelection, selectionSize, getSelectedElementIds,
    addToSelection, removeFromSelection, replaceSelection, clearSelection,
    isElementSelected,
} from './selection';

/**
 * Select an element on the canvas.
 * - Normal click: Replace selection with this element
 * - Shift+click: Add/remove element from selection (toggle)
 */
function selectElement(canvasId: string, itemId: string, container: HTMLElement, shiftKey: boolean): void {
    if (shiftKey) {
        if (isElementSelected(canvasId, itemId)) {
            removeFromSelection(canvasId, itemId);
            const el = container.querySelector(`[data-element-id="${itemId}"]`) as HTMLElement | null;
            if (el) el.classList.remove('canvas-element-selected');
        } else {
            addToSelection(canvasId, itemId);
            const el = container.querySelector(`[data-element-id="${itemId}"]`) as HTMLElement | null;
            log.debug(SEG.ELEMENT, '[Canvas] selectElement: Adding to selection', {
                itemId, foundElement: !!el, elementClass: el?.className
            });
            if (el) el.classList.add('canvas-element-selected');
            else log.warn(SEG.ELEMENT, '[Canvas] selectElement: Element not found', { itemId });
        }
    } else {
        deselectAll(canvasId, container);
        replaceSelection(canvasId, [itemId]);
        const el = container.querySelector(`[data-element-id="${itemId}"]`) as HTMLElement | null;
        log.debug(SEG.ELEMENT, '[Canvas] selectElement: Replace mode', {
            itemId, foundElement: !!el, elementClass: el?.className
        });
        if (el) el.classList.add('canvas-element-selected');
        else log.warn(SEG.ELEMENT, '[Canvas] selectElement: Element not found in replace mode', { itemId });
    }

    const selectedIds = getSelectedElementIds(canvasId);
    log.debug(SEG.ELEMENT, '[Canvas] selectElement: Checking action bar', {
        selectedCount: selectionSize(canvasId), selectedIds
    });
    if (hasSelection(canvasId)) {
        log.debug(SEG.ELEMENT, '[Canvas] selectElement: Showing action bar');
        showActionBar(
            selectedIds,
            container,
            () => deleteSelectedElements(canvasId, container),
            (composition) => unmeldSelectedElements(canvasId, container, composition),
            () => convertNoteToPrompt(container, selectedIds[0]),
            () => convertResultToNote(container, selectedIds[0]),
        );
    } else {
        hideActionBar(container);
    }

    log.debug(SEG.ELEMENT, `[Canvas] Selected ${selectionSize(canvasId)} elements`, { selectedIds });
}

/**
 * Create a Element object from a DOM element by detecting its type.
 */
function createElementFromElement(element: HTMLElement, id: string): Element {
    const entry = getElementTypeByElement(element);
    return {
        id,
        title: entry?.title ?? 'Element',
        symbol: entry?.symbol,
        renderContent: () => element,
    };
}

/** Deselect all elements and hide action bar */
function deselectAll(canvasId: string, container: HTMLElement): void {
    if (!hasSelection(canvasId)) return;
    const selected = container.querySelectorAll('.canvas-element-selected');
    selected.forEach(el => el.classList.remove('canvas-element-selected'));
    hideActionBar(container);
    clearSelection(canvasId);
}

/** Unmeld composition containing currently selected elements */
function unmeldFromSelection(canvasId: string, container: HTMLElement): void {
    if (!hasSelection(canvasId)) return;
    for (const itemId of getSelectedElementIds(canvasId)) {
        const elementEl = container.querySelector(`[data-element-id="${itemId}"]`) as HTMLElement | null;
        if (!elementEl) continue;
        const composition = elementEl.closest('.melded-composition') as HTMLElement | null;
        if (composition) {
            unmeldSelectedElements(canvasId, container, composition);
            return;
        }
    }
    log.debug(SEG.ELEMENT, '[Canvas] No composition found for selected elements');
}

/** Unmeld selected elements that are in a melded composition */
function unmeldSelectedElements(canvasId: string, container: HTMLElement, composition: HTMLElement): void {
    const selectedIds = getSelectedElementIds(canvasId);

    // Single element selected inside a composition → try partial detach
    if (selectedIds.length === 1) {
        const itemId = selectedIds[0];
        const elementEl = container.querySelector(`[data-element-id="${itemId}"]`) as HTMLElement | null;
        if (elementEl?.closest('.melded-composition') === composition) {
            const detachResult = detachElement(itemId, composition);
            if (detachResult) {
                const { detachedElement, remainingComposition } = detachResult;

                // Restore drag on detached element
                const detachedId = detachedElement.dataset.elementId || detachedElement.getAttribute('data-element-id') || 'unknown';
                const detachedItem = createElementFromElement(detachedElement, detachedId);
                const detachedEntry = detachedItem.symbol ? getElementTypeBySymbol(detachedItem.symbol) : undefined;
                makeDraggable(detachedElement, detachedElement, detachedItem, { logLabel: detachedEntry?.label ?? 'Element' });

                if (remainingComposition) {
                    // Remaining composition needs drag handler refreshed
                    const compId = remainingComposition.getAttribute('data-element-id') || 'unknown';
                    const compElement: Element = { id: compId, title: 'Melded Composition', renderContent: () => remainingComposition };
                    makeDraggable(remainingComposition, remainingComposition, compElement, { logLabel: 'MeldedComposition' });
                } else {
                    // Full unmeld happened — restore drag on all freed elements
                    const allFreed = container.querySelectorAll('[data-element-id]');
                    allFreed.forEach((el) => {
                        const element = el as HTMLElement;
                        const id = element.dataset.elementId || element.getAttribute('data-element-id') || 'unknown';
                        if (id === detachedId) return; // already handled
                        if (element.closest('.melded-composition')) return; // still in a composition
                        const item = createElementFromElement(element, id);
                        const entry = item.symbol ? getElementTypeBySymbol(item.symbol) : undefined;
                        makeDraggable(element, element, item, { logLabel: entry?.label ?? 'Element' });
                    });
                }

                deselectAll(canvasId, container);
                log.debug(SEG.ELEMENT, '[Canvas] Detached element from composition', {
                    itemId, partial: !!remainingComposition
                });
                return;
            }
        }
    }

    // Full unmeld: multiple selected or detach not applicable
    const result = unmeldComposition(composition);
    if (!result) {
        const compId = composition.dataset.elementId || 'unknown';
        log.error(SEG.ELEMENT, `[Canvas] Failed to unmeld composition ${compId}`);
        return;
    }
    const { members: elementElements } = result;
    elementElements.forEach((element) => {
        const itemId = element.dataset.elementId || element.getAttribute('data-element-id') || 'unknown';
        const item = createElementFromElement(element, itemId);
        const entry = item.symbol ? getElementTypeBySymbol(item.symbol) : undefined;
        makeDraggable(element, element, item, { logLabel: entry?.label ?? 'Element' });
    });
    deselectAll(canvasId, container);
    log.debug(SEG.ELEMENT, '[Canvas] Unmelded composition', {
        count: elementElements.length,
        elementIds: elementElements.map(el => el.dataset.elementId).filter(Boolean)
    });
}

/** Remove a single element element from the canvas with animation */
function removeElementElement(container: HTMLElement, itemId: string, duration: number): void {
    const el = container.querySelector(`[data-element-id="${itemId}"]`) as HTMLElement | null;
    uiState.removeCanvasElement(itemId);
    container.dispatchEvent(new CustomEvent('element-deleted', { detail: { itemId } }));
    if (!el) return;
    runCleanup(el);
    el.classList.remove('canvas-element-selected');
    if (duration === 0) { el.remove(); return; }
    const animation = el.animate([
        { opacity: 1, transform: 'scale(1)' },
        { opacity: 0, transform: 'scale(0.85)' }
    ], { duration, easing: 'ease-in', fill: 'forwards' });
    animation.onfinish = () => { el.remove(); };
}

/** Delete all currently selected elements from the canvas */
function deleteSelectedElements(canvasId: string, container: HTMLElement): void {
    if (!hasSelection(canvasId)) return;
    const elementIdsToDelete = getSelectedElementIds(canvasId);
    hideActionBar(container);
    clearSelection(canvasId);
    const duration = getRestDuration();

    // Collect spines that need to be deleted (any selected element is on a spine)
    const spinesToDelete = new Set<string>();
    const extraElementsToDelete = new Set<string>();
    for (const itemId of elementIdsToDelete) {
        const spine = getSpineByNode(canvasId, itemId);
        if (spine && !spinesToDelete.has(spine.id)) {
            spinesToDelete.add(spine.id);
            // Also delete the 〽 end marker (last node) if it's not already in the delete list
            const endMarker = spine.nodes[spine.nodes.length - 1];
            if (!elementIdsToDelete.includes(endMarker)) {
                extraElementsToDelete.add(endMarker);
            }
        }
    }

    // Remove spines
    for (const spineId of spinesToDelete) {
        removeSpine(canvasId, spineId);
        uiState.removeCanvasSpine(spineId);
    }

    // Remove selected elements + any extra 〽 end markers
    for (const itemId of elementIdsToDelete) {
        removeElementElement(container, itemId, duration);
    }
    for (const itemId of extraElementsToDelete) {
        removeElementElement(container, itemId, duration);
    }

    const totalDeleted = elementIdsToDelete.length + extraElementsToDelete.size;
    log.debug(SEG.ELEMENT, `[Canvas] Deleted ${totalDeleted} elements, ${spinesToDelete.size} spines`, { elementIdsToDelete });
}

/**
 * Render an element on the canvas.
 * Uses element-registry for dispatch instead of per-type if/else.
 */
export async function renderElement(item: Element): Promise<HTMLElement> {
    log.debug(SEG.ELEMENT, `[Canvas] Rendering element ${item.id}`, {
        symbol: item.symbol, hasContent: !!item.content
    });

    // Result elements: parse content JSON to get ExecutionResult
    if (item.symbol === 'result') {
        if (!item.content) {
            log.error(SEG.ELEMENT, `[Canvas] Result element ${item.id} missing content`, {
                itemId: item.id, position: { x: item.x, y: item.y },
                size: { width: item.width, height: item.height }
            });
            return createErrorElement(
                item.id, 'result',
                { x: item.x ?? 200, y: item.y ?? 200 },
                { type: 'missing_data', message: 'Execution result data missing',
                  details: { 'Has content': false, 'Position': `(${item.x}, ${item.y})`,
                    'Size': `${item.width}x${item.height}`,
                    'Cause': 'Element metadata saved without execution result (migration bug)' } }
            );
        }
        try {
            const parsed = JSON.parse(item.content);
            // Token-based content (streamed responses): let createResultElement
            // restore from saved canvas state internally — don't pass a result.
            const hasTokens = Array.isArray(parsed.tokens) && parsed.tokens.length > 0;
            // Backwards-compatible: new format has .result, old is raw ExecutionResult
            const result = hasTokens ? undefined : (parsed.result ?? parsed);
            const promptConfig: PromptConfig | undefined = parsed.promptConfig;
            const prompt: string | undefined = parsed.prompt;
            const el = createResultElement(item, result, promptConfig, prompt);

            // Restore follow-up error state if persisted
            if (parsed.followupError) {
                setResponseState(el, 'error');
                const zone = el.querySelector('.result-followup-zone');
                const status = el.querySelector('.followup-status');
                if (zone) zone.classList.add('has-error');
                if (status) status.textContent = parsed.followupError;
            }

            return el;
        } catch (err) {
            log.error(SEG.ELEMENT, `[Canvas] Result element ${item.id} has invalid JSON content`, err);
            return createErrorElement(
                item.id, 'result',
                { x: item.x ?? 200, y: item.y ?? 200 },
                { type: 'parse_failed', message: 'Failed to parse execution result JSON',
                  details: { 'Content length': item.content.length,
                    'Content preview': item.content.substring(0, 100) } }
            );
        }
    }

    // Stream elements: skip if no saved content (orphaned empty stream)
    if (item.symbol === 'stream' && !item.content) {
        log.debug(SEG.ELEMENT, `[Canvas] Skipping empty stream element ${item.id}`);
        uiState.removeCanvasElement(item.id);
        return document.createElement('div'); // invisible placeholder
    }

    // Look up element type in registry
    const entry = item.symbol ? getElementTypeBySavedSymbol(item.symbol, item.content) : undefined;
    if (entry) return await entry.render(item);

    // Nothing is registered under this symbol. What the element is — published,
    // a plugin's, or nothing this node has — is the node's to answer, and the
    // placeholder asks it rather than assuming. plugin_name is read for the
    // name only: records written before published elements stopped claiming to
    // be plugins carry the element's name in that field.
    const persistedElement = uiState.getCanvasElement(item.id);
    const name = persistedElement?.plugin_name
        || (item.symbol ? getPluginNameBySymbol(item.symbol) : null)
        || item.symbol
        || 'unknown';

    log.warn(SEG.ELEMENT, `[Canvas] Nothing is registered for ${item.symbol}; drawing why in its place`, {
        itemId: item.id, symbol: item.symbol, name, position: { x: item.x, y: item.y }
    });
    const placeholder = createAbsentElement(item, name);

    // Discovery may not have run yet on a fresh page. When it lands and the
    // symbol is registered, what is drawn is replaced by the element itself.
    (async () => {
        const { loadPluginElements } = await import('../plugin-provided-elements');
        await loadPluginElements();
        const retryEntry = item.symbol ? getElementTypeBySavedSymbol(item.symbol, item.content) : undefined;
        if (retryEntry && placeholder.parentElement) {
            log.info(SEG.ELEMENT, `[Canvas] ${item.symbol} is registered now; drawing it`);
            const real = await retryEntry.render(item);
            placeholder.parentElement.replaceChild(real, placeholder);
            return;
        }
        // Discovery ran and this is still not registered, so what the frame
        // said — that the page had not loaded it yet — has stopped being true.
        // It asks again, and now there is a recorded reason to find.
        askAbsentAgain(placeholder);
    })().catch((err: unknown) => log.error(SEG.ELEMENT, `[Canvas] Could not draw ${item.symbol} after discovery:`, err));

    return placeholder;
}

/**
 * Redraw every element of one symbol that is already placed on a canvas.
 *
 * Replacing a registry entry only reaches the next render. An element drawn from
 * the module that was replaced keeps what that module built, so a republish is
 * invisible on the canvas somebody is looking at until this runs.
 */
export async function redrawPlacedElements(symbol: string): Promise<number> {
    const entry = getElementTypeBySymbol(symbol);
    if (!entry) return 0;

    const placed = Array.from(
        document.querySelectorAll<HTMLElement>('.canvas-workspace [data-element-id]')
    );

    let redrawn = 0;
    for (const el of placed) {
        const id = el.dataset.elementId;
        const saved = id ? uiState.getCanvasElement(id) : undefined;
        if (!id || saved?.symbol !== symbol) continue;

        const parent = el.parentElement;
        if (!parent) continue;

        // Same order the canvas uses to remove one: what the old module
        // registered runs before its element stops existing.
        runCleanup(el);
        const fresh = await renderElement({
            id,
            title: entry.title,
            symbol,
            x: saved.x,
            y: saved.y,
            width: saved.width,
            height: saved.height,
            content: saved.content,
            renderContent: () => document.createElement('div'),
        });
        parent.replaceChild(fresh, el);
        redrawn++;
    }

    if (redrawn > 0) {
        log.info(SEG.ELEMENT, `[Canvas] Redrew ${redrawn} placed ${symbol} element(s) from the module now registered`);
    }
    return redrawn;
}

/**
 * Build a canvas workspace DOM element with full interaction support.
 *
 * Used by both the root canvas element and subcanvas when expanded to fullscreen.
 */
export function buildCanvasWorkspace(
    canvasId: string,
    items: Element[]
): HTMLElement {
    const container = document.createElement('div');
    container.className = 'canvas-workspace';
    container.dataset.canvasId = canvasId;
    (container as any).__elements = items;
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

    // Right-click opens spawn menu — suppressed if already open or placing
    // Context-aware: right-click on element symbol shows thread actions, background shows element types
    container.addEventListener('contextmenu', (e) => {
        e.preventDefault();
        if (isPlacementActive() || isSpawnMenuOpen()) return;
        const target = e.target as HTMLElement;
        // Find the actual symbol span — walk up from click target or search within element
        const elementEl = target.closest('.canvas-element') as HTMLElement | null;
        const symbolEl = target.closest('.symbol') as HTMLElement | null
            ?? elementEl?.querySelector('.symbol') as HTMLElement | null;
        showSpawnMenu(e.clientX, e.clientY, contentLayer, items, canvasId, symbolEl);
    });

    // Prevent dblclick from bubbling past workspace boundary (stops re-morph on parent subcanvas)
    container.addEventListener('dblclick', (e) => { e.stopPropagation(); });

    // File drop: drag files onto canvas to create Doc elements
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

                    // What the doc element reads back out of the canvas. The
                    // module that draws it is published, so this shape is the
                    // contract between the drop and whatever is registered.
                    const contentMeta = {
                        fileId: result.id,
                        filename: result.filename,
                        ext,
                    };

                    const item: Element = {
                        id: `doc-${crypto.randomUUID()}`,
                        title: result.filename,
                        symbol: Doc,
                        x,
                        y,
                        content: JSON.stringify(contentMeta),
                        renderContent: () => document.createElement('div'),
                    };

                    items.push(item);
                    // Persisted before it is drawn, because the module reads
                    // its content off the canvas rather than off the element.
                    uiState.addCanvasElement({
                        id: item.id,
                        symbol: Doc,
                        x,
                        y,
                        content: JSON.stringify(contentMeta),
                    });

                    // Through the registry, so what draws a dropped file is
                    // whatever is registered for Doc — and a node with nothing
                    // registered draws why, rather than nothing.
                    const elementElement = await renderElement(item);
                    contentLayer.appendChild(elementElement);

                    const rect = elementElement.getBoundingClientRect();
                    uiState.addCanvasElement({
                        id: item.id,
                        symbol: Doc,
                        x,
                        y,
                        width: Math.round(rect.width),
                        height: Math.round(rect.height),
                        content: JSON.stringify(contentMeta),
                    });

                    log.info(SEG.ELEMENT, `[Canvas] Spawned Doc element for ${result.filename} at (${x}, ${y})`);
                } catch (err) {
                    const message = err instanceof Error ? err.message : String(err);
                    log.error(SEG.ELEMENT, `[Canvas] Failed to upload file ${file.name}`, { error: err });
                    toast.error(`Failed to upload ${file.name}: ${message}`);
                }
            })();
        }
    });

    // SDK spawn-result: elements fire this event via ui.spawnResult(), canvas handles the rest
    // TODO [TS-5]: Extract shared spawnResultBelow — this pattern is repeated
    // in prompt-element.ts and element-followup.ts.
    container.addEventListener('element:spawn-result', ((e: CustomEvent<SpawnResultDetail>) => {
        const { elementId: itemId, name, result } = e.detail;
        const parentElement = (e.target as HTMLElement).closest('[data-element-id]') as HTMLElement | null;
        if (!parentElement) {
            log.error(SEG.ELEMENT, `[Canvas] spawn-result: no parent element element for ${itemId}`);
            return;
        }

        const parentRect = parentElement.getBoundingClientRect();
        const canvasRect = container.getBoundingClientRect();
        const x = Math.round(parentRect.left - canvasRect.left);
        const y = Math.round(parentRect.bottom - canvasRect.top);

        const resultElementId = `result-${crypto.randomUUID()}`;
        const resultItem: Element = {
            id: resultElementId,
            title: `${name} Result`,
            symbol: 'result',
            x, y,
            width: Math.round(parentRect.width),
            renderContent: () => document.createElement('div'),
        };

        const resultElement = createResultElement(resultItem, result as ExecutionResult);
        contentLayer.appendChild(resultElement);

        const resultRect = resultElement.getBoundingClientRect();
        uiState.addCanvasElement({
            id: resultElementId,
            symbol: 'result',
            x, y,
            width: Math.round(resultRect.width),
            height: Math.round(resultRect.height),
            content: JSON.stringify(result),
        });

        autoMeldResultBelow(
            parentElement, itemId, name, name,
            resultElement, resultElementId, name,
        );

        log.info(SEG.ELEMENT, `[Canvas] Spawned result for ${name} element ${itemId} at (${x}, ${y})`);
    }) as EventListener);

    // Selection: click on an element to select, Shift+click for multi-select, click background to deselect
    container.addEventListener('click', (e) => {
        const target = e.target as HTMLElement;

        // Close spawn menu if it exists
        const spawnMenu = document.querySelector('.canvas-spawn-menu');
        if (spawnMenu && !spawnMenu.contains(target)) spawnMenu.remove();

        // Ignore clicks on action bar, buttons, inputs, textareas, contenteditable,
        // and elements marked as interactive via preventDrag (e.g. xterm terminals)
        if (target.closest('.canvas-action-bar')) return;
        if (target.tagName === 'BUTTON' || target.tagName === 'INPUT' || target.tagName === 'TEXTAREA') return;
        if (target.isContentEditable || target.closest('[contenteditable="true"]')) return;

        // Pick up 〽: left-click on a thread end marker resumes threading from its spine.
        // The same DOM element becomes the cursor and gets re-pinned at the new endpoint —
        // one element across the entire pickup → drop cycle (element axiom).
        const threadElementEl = target.closest('.canvas-thread-element') as HTMLElement | null;
        if (threadElementEl && container.contains(threadElementEl)) {
            const threadElementId = threadElementEl.dataset.elementId;
            const spine = threadElementId ? getSpineByNode(canvasId, threadElementId) : null;
            if (spine && threadElementId) {
                e.stopPropagation();
                const symbolEl = threadElementEl.querySelector('.symbol') as HTMLElement | null;
                if (!symbolEl) return;
                const existingNodeIds = spine.nodes.slice(0, -1);
                const originPos = uiState.getCanvasElements().find(g => g.id === threadElementId);

                // Take down the old spine and lift the 〽 into cursor mode (same DOM element)
                removeSpine(canvasId, spine.id);
                unpinThreadElement(threadElementEl);

                enterThreadBuildingMode(symbolEl, spine.color, (result) => {
                    // The cursor handed back IS threadElementEl — pin it at the new position
                    uiState.removeCanvasSpine(spine.id);

                    const cont = contentLayer.parentElement!;
                    const contRect = cont.getBoundingClientRect();
                    const t = getTransform(canvasId);
                    const px = Math.round((result.placeX - contRect.left - t.panX) / t.scale);
                    const py = Math.round((result.placeY - contRect.top - t.panY) / t.scale);
                    pinThreadElement(threadElementEl, contentLayer, px, py, spine.color, threadElementId);

                    const existing = uiState.getCanvasElements().find(g => g.id === threadElementId);
                    if (existing) uiState.addCanvasElement({ ...existing, x: px, y: py });

                    const newSpine = {
                        id: `spine-${crypto.randomUUID()}`,
                        color: spine.color,
                        nodes: [...result.nodeIds, threadElementId],
                    };
                    addSpine(canvasId, contentLayer, newSpine);
                    uiState.addCanvasSpine(newSpine);
                }, () => {
                    // Cancel: pin the 〽 back at its original position and restore the spine
                    pinThreadElement(threadElementEl, contentLayer, originPos?.x ?? 0, originPos?.y ?? 0, spine.color, threadElementId);
                    addSpine(canvasId, contentLayer, spine);
                }, existingNodeIds, threadElementEl);
                return;
            }
            // Orphan 〽 (spine gone) — fall through to normal select
        }

        if (target.closest('[data-prevent-drag]')) return;

        // Focus container to enable keyboard shortcuts
        container.focus({ preventScroll: true });

        // Walk up from click target to find an element element (must be inside this workspace)
        const elementEl = target.closest('[data-element-id]') as HTMLElement | null;
        const isInsideWorkspace = elementEl ? container.contains(elementEl) : false;
        if (elementEl && isInsideWorkspace && elementEl.dataset.elementId !== 'canvas-workspace') {
            const itemId = elementEl.dataset.elementId;
            if (itemId) {
                e.stopPropagation();
                selectElement(canvasId, itemId, container, e.shiftKey);
            }
        } else {
            // Clicked on background — deselect (skip if rectangle selection just completed)
            if (!didRectangleSelectionJustComplete()) {
                deselectAll(canvasId, container);
            }
        }
    }, true);

    // Drill-in state: when inside a melded composition, hjkl navigates children only
    let drilledComposition: HTMLElement | null = null;

    // Per-element "active thread" — which spine ←/→ follows when the element is on multiple.
    // Set by ↑/↓ (rotate) and by ←/→ (carry the active spine to the next element).
    const activeSpinePerElement = new Map<string, string>();

    /**
     * Arrow-key thread navigation. Delegates the decision to the pure
     * `navigateThread` function and applies any resulting side effects
     * (select target, pan camera).
     */
    function onThreadNavigate(direction: 'left' | 'down' | 'up' | 'right'): boolean {
        const selected = getSelectedElementIds(canvasId);
        const result = navigateThread(direction, {
            canvasId,
            currentElementId: selected.length > 0 ? selected[0] : null,
            activeSpinePerElement,
        });

        if (result.targetElementId) {
            const targetEl = contentLayer.querySelector(`[data-element-id="${result.targetElementId}"]`) as HTMLElement | null;
            if (targetEl) {
                selectElement(canvasId, result.targetElementId, container, false);
                centerOnElementSymbol(container, canvasId, targetEl);
            }
        }
        return result.handled;
    }

    function getNavigationCandidates(): HTMLElement[] {
        if (drilledComposition) {
            // Inside a composition — only direct element children, skip error elements
            return ([...drilledComposition.querySelectorAll(':scope > [data-element-id]')] as HTMLElement[])
                .filter(el => !el.classList.contains('canvas-error-element'));
        }
        // Top level — compositions as units + standalone elements
        const all = [...container.querySelectorAll('[data-element-id]')] as HTMLElement[];
        return all.filter(el => {
            if (!el.dataset.elementId || el.dataset.elementId === 'canvas-workspace') return false;
            // Skip error elements
            if (el.classList.contains('canvas-error-element')) return false;
            // Skip elements that are inside a composition (the composition itself is the nav target)
            const parentComp = el.parentElement?.closest('.melded-composition');
            if (parentComp && parentComp !== el) return false;
            return true;
        });
    }

    function navigateDirection(direction: 'left' | 'down' | 'up' | 'right'): void {
        const selected = getSelectedElementIds(canvasId);

        // Auto-drill: if selected element is inside a composition we haven't drilled into, do it now
        if (!drilledComposition && selected.length > 0) {
            const selEl = container.querySelector(`[data-element-id="${selected[0]}"]`) as HTMLElement | null;
            if (selEl) {
                const parentComp = selEl.parentElement?.closest('.melded-composition') as HTMLElement | null;
                if (parentComp && parentComp !== selEl) {
                    drilledComposition = parentComp;
                }
            }
        }

        const candidates = getNavigationCandidates();
        if (candidates.length === 0) return;

        // Reference point: selected element center, or viewport center
        let cx: number, cy: number;
        if (selected.length > 0) {
            const currentEl = candidates.find(el => el.dataset.elementId === selected[0]);
            if (!currentEl) return;
            cx = currentEl.offsetLeft + currentEl.offsetWidth / 2;
            cy = currentEl.offsetTop + currentEl.offsetHeight / 2;
        } else {
            const viewCenter = screenToCanvas(canvasId, container.clientWidth / 2, container.clientHeight / 2);
            cx = viewCenter.x;
            cy = viewCenter.y;
        }

        const currentId = selected.length > 0 ? selected[0] : null;
        let best: HTMLElement | null = null;
        let bestDist = Infinity;
        for (const el of candidates) {
            if (el.dataset.elementId === currentId) continue;
            const ex = el.offsetLeft + el.offsetWidth / 2;
            const ey = el.offsetTop + el.offsetHeight / 2;
            const dx = ex - cx;
            const dy = ey - cy;

            let valid = false;
            if (direction === 'right' && dx > 0) valid = true;
            else if (direction === 'left' && dx < 0) valid = true;
            else if (direction === 'down' && dy > 0) valid = true;
            else if (direction === 'up' && dy < 0) valid = true;
            if (!valid) continue;

            const dist = dx * dx + dy * dy;
            if (dist < bestDist) {
                bestDist = dist;
                best = el;
            }
        }
        if (best) {
            selectElement(canvasId, best.dataset.elementId!, container, false);
            panToElement(container, canvasId, best);
        }
    }

    // Setup keyboard shortcuts (hjkl nav, Enter drill-in, ESC drill-out, DELETE, U, 0)
    void setupKeyboardShortcuts(
        container,
        () => hasSelection(canvasId),
        () => {
            // ESC: if drilled into a composition, pop out and select the composition
            if (drilledComposition) {
                const compId = drilledComposition.dataset.elementId;
                drilledComposition = null;
                if (compId) {
                    selectElement(canvasId, compId, container, false);
                } else {
                    deselectAll(canvasId, container);
                }
                return;
            }
            deselectAll(canvasId, container);
        },
        () => deleteSelectedElements(canvasId, container),
        () => unmeldFromSelection(canvasId, container),
        () => resetTransform(container, canvasId),
        navigateDirection,
        () => {
            // Enter: if selected element is a composition, drill into it.
            // Otherwise, focus textarea.
            const selected = getSelectedElementIds(canvasId);
            if (selected.length !== 1) return;
            const el = container.querySelector(`[data-element-id="${selected[0]}"]`) as HTMLElement | null;
            if (!el) return;

            if (el.classList.contains('melded-composition') && !drilledComposition) {
                // Drill into composition — select first child
                drilledComposition = el;
                const firstChild = el.querySelector(':scope > [data-element-id]') as HTMLElement | null;
                if (firstChild && firstChild.dataset.elementId) {
                    selectElement(canvasId, firstChild.dataset.elementId, container, false);
                }
                log.debug(SEG.ELEMENT, '[Canvas] Drilled into composition', { id: selected[0] });
                return;
            }

            // Focus textarea
            const textarea = el.querySelector('textarea') as HTMLTextAreaElement | null;
            if (textarea) {
                textarea.focus();
            }
        },
        onThreadNavigate
    );

    // Setup canvas pan (two-finger scroll on desktop, single finger drag on mobile)
    void setupCanvasPan(container, canvasId);

    // Setup rectangle selection
    // Always register — user may resize browser between mobile/desktop widths
    void setupRectangleSelection(
        container,
        (itemId, cont, shiftKey) => selectElement(canvasId, itemId, cont, shiftKey),
        (cont) => deselectAll(canvasId, cont)
    );

    // Clean up local elements array when an element is deleted
    container.addEventListener('element-deleted', ((e: CustomEvent<{ itemId: string }>) => {
        const idx = items.findIndex(g => g.id === e.detail.itemId);
        if (idx !== -1) items.splice(idx, 1);
    }) as EventListener);

    // Render existing elements in parallel then append in original order
    (async () => {
        // Step 1: Render all individual elements (skip minimized — they live in the tray)
        const minimizedIds = new Set(uiState.getMinimizedWindows());
        const visible = items.filter(g => !minimizedIds.has(g.id));
        const rendered = await Promise.all(visible.map(g => renderElement(g)));
        for (const el of rendered) contentLayer.appendChild(el);

        // Step 2: Restore melded compositions after all elements are rendered
        const savedCompositions = getAllCompositions();
        log.debug(SEG.ELEMENT, `[Canvas] Restoring ${savedCompositions.length} compositions from state`);

        for (const comp of savedCompositions) {
            // Skip and clean up invalid compositions (old format without edges)
            if (!comp.edges || !Array.isArray(comp.edges)) {
                log.warn(SEG.ELEMENT, `[Canvas] Removing invalid composition ${comp.id} - old format (missing edges array)`);
                removeComposition(comp.id);
                continue;
            }

            const elementIds = extractElementIds(comp.edges);
            const elementElements = elementIds
                .map(id => container.querySelector(`[data-element-id="${id}"]`) as HTMLElement)
                .filter(el => el !== null);

            if (elementElements.length !== elementIds.length) {
                log.debug(SEG.ELEMENT, `[Canvas] Removing stale composition ${comp.id} - ${elementIds.length - elementElements.length} elements no longer on canvas`);
                removeComposition(comp.id);
                continue;
            }

            try {
                const composition = reconstructMeld(elementElements, comp.edges, comp.id, comp.x, comp.y);
                const compositionElement: Element = {
                    id: comp.id,
                    title: 'Melded Composition',
                    renderContent: () => composition
                };
                makeDraggable(composition, composition, compositionElement, { logLabel: 'MeldedComposition' });
                log.debug(SEG.ELEMENT, `[Canvas] Restored composition ${comp.id}`, {
                    edgeCount: comp.edges.length, elementCount: elementIds.length
                });
            } catch (err) {
                log.error(SEG.ELEMENT, `[Canvas] Failed to restore composition ${comp.id}`, { error: err });
            }
        }

        // Step 3: Restore navigational threads (spines)
        const savedSpines = uiState.getCanvasSpines();
        for (const spine of savedSpines) {
            // Verify all nodes exist on canvas
            const allExist = spine.nodes.every(id =>
                contentLayer.querySelector(`[data-element-id="${id}"]`) !== null
            );
            if (!allExist) {
                log.debug(SEG.ELEMENT, `[Canvas] Removing stale spine ${spine.id} — missing nodes`);
                uiState.removeCanvasSpine(spine.id);
                continue;
            }
            addSpine(canvasId, contentLayer, spine);
            log.debug(SEG.ELEMENT, `[Canvas] Restored spine ${spine.id} with ${spine.nodes.length} nodes`);
        }
    })().catch((err: unknown) => log.error(SEG.ELEMENT, `[Canvas] Rendering workspace ${canvasId} failed:`, err));

    return container;
}
