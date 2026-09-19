/**
 * Canvas-Expanded Form — canvasPlaced ↔ canvasExpanded morph path
 *
 * General capability: any canvas-placed element can fill the viewport and come
 * back. The subcanvas element is the first consumer.
 *
 * This file used to call the far end "fullscreen", which named nothing: the
 * workspace fills the viewport too, and so does a panel dragged past 90% of it.
 * `canvasExpanded` is the row it has in FORMS.
 *
 * Element Axiom: the element element is reparented (not recreated):
 * - canvasPlaced: child of `.canvas-content-layer` (inside CSS transform)
 * - canvasExpanded: child of `document.body` (escapes CSS transform for pan/zoom)
 *
 * Two minimize paths:
 * - Default: morph back to compact canvas position (morphCanvasExpandedToCanvasPlaced)
 * - Escape to tray: morph to tray dot (future — uses morphWorkspaceToDot)
 */

import { log, SEG } from '../../../logger';
import type { Element } from '@teranos/elements';
import { getOpenDuration, getRestDuration, setForm, setCanvasOrigin, getCanvasOrigin, clearCanvasOrigin, beginMorphToBox, beginMorphToCanvasPlaced } from '@teranos/elements';
import { canvasToScreen, getTransform } from '../canvas/canvas-pan';
import { buildCanvasWorkspace } from '../canvas/canvas-workspace-builder';
import { uiState } from '../../../state/ui';
import { getElementTypeBySavedSymbol } from '../element-registry';
import { destroyCanvasSelection } from '../canvas/selection';
import { pushBreadcrumb, popBreadcrumb, buildBreadcrumbBar } from '../canvas/breadcrumb';
import { exportCanvasStatic } from '../../../api/canvas';
import { Button } from '../../button';

/**
 * Morph a canvas-placed element to fullscreen workspace
 *
 * @param element - The element's persistent DOM element (Axiom: same element throughout)
 * @param element - Element data (id, position, size)
 * @param canvasId - The parent canvas ID (for coordinate conversion on return)
 * @param onMinimize - Called when the element is minimized back to canvas-placed
 */
export function morphCanvasPlacedToCanvasExpanded(
    element: HTMLElement,
    item: Element,
    canvasId: string,
    onMinimize: (element: HTMLElement, item: Element) => void
): void {
    // Capture current screen-space rect (accounts for CSS transform from pan/zoom)
    const fromRect = element.getBoundingClientRect();

    // Store canvas-local origin for return morph
    setCanvasOrigin(element, {
        x: item.x ?? 0,
        y: item.y ?? 0,
        width: item.width ?? 180,
        height: item.height ?? 120,
        canvasId
    });

    // Remove from canvas content layer and reparent to body
    element.remove();
    // In flight; setForm below says what it is morphing into. The
    // class this used to carry named the workspace, which this is not.
    element.className = 'morphing';
    element.style.position = 'fixed';
    element.style.zIndex = '1000';
    element.innerHTML = '';
    document.body.appendChild(element);

    // The manifestation this file's header names. It used to say "window",
    // because the boolean it wrote had no other way to say "off the canvas".
    setForm(element, 'canvasExpanded');

    // Target: full viewport
    const toRect = { x: 0, y: 0, width: window.innerWidth, height: window.innerHeight };

    beginMorphToBox(element, fromRect, toRect, getOpenDuration())
        .then(() => {
            log.debug(SEG.ELEMENT, `[CanvasExpanded] Morph to fullscreen committed for ${item.id}`);

            // Apply fullscreen styles
            element.style.position = 'fixed';
            element.style.left = '0';
            element.style.top = '0';
            element.style.width = '100vw';
            element.style.height = '100vh';
            element.style.borderRadius = '0';
            element.style.backgroundColor = 'var(--bg-primary)';
            element.style.boxShadow = 'none';
            element.style.padding = '0';
            element.style.opacity = '1';
            element.style.display = 'flex';
            element.style.flexDirection = 'column';
            element.className = 'canvas-subcanvas-element-expanded canvas-fullscreen-adjusted';

            const doMinimize = (instant: boolean = false) => {
                element.removeEventListener('keydown', escapeHandler);
                // Only pop when directly minimized (button/Escape).
                // Cascade via jumpToBreadcrumb splices the stack itself.
                if (!instant) {
                    popBreadcrumb();
                }

                if (instant) {
                    collapseImmediately(element, item, onMinimize);
                } else {
                    morphCanvasExpandedToCanvasPlaced(element, item, onMinimize);
                }
            };

            // Push breadcrumb entry
            pushBreadcrumb({
                canvasId: item.id,
                name: item.content || 'subcanvas',
                minimize: doMinimize,
            });

            // Build breadcrumb bar with minimize button inside it
            const breadcrumbBar = buildBreadcrumbBar();

            // Export button — server-side rendering via canvas-renderer plugin
            // Button automatically shows slide-out error display on failure
            // TODO: Add Publish button next to Export (scope to canvas_id like export)
            const exportBtn = new Button({
                label: 'Export',
                icon: '↓',
                variant: 'warning',
                size: 'small',
                className: 'canvas-export-btn',
                confirmation: {
                    label: 'Confirm Export',
                    timeout: 5000
                },
                onClick: async () => {
                    await exportCanvasStatic(item.id);
                    log.info(SEG.ELEMENT, '[Canvas] Export complete (server-side rendering)');
                }
            });
            breadcrumbBar.appendChild(exportBtn.element);

            const minimizeBtn = document.createElement('button');
            minimizeBtn.textContent = '−';
            minimizeBtn.className = 'canvas-minimize-btn';
            minimizeBtn.onclick = () => doMinimize(false);
            breadcrumbBar.appendChild(minimizeBtn);

            element.appendChild(breadcrumbBar);

            // Escape key minimizes back to canvas-placed position.
            // Listener on element (not document) so nested subcanvases only
            // collapse one level at a time — the innermost catches the event first.
            const escapeHandler = (e: KeyboardEvent) => {
                if (e.key !== 'Escape') return;
                const target = e.target as HTMLElement;
                if (target.tagName === 'INPUT' || target.tagName === 'TEXTAREA' || target.isContentEditable) return;
                e.preventDefault();
                e.stopPropagation();
                doMinimize(false);
            };
            element.addEventListener('keydown', escapeHandler);

            // Load inner elements for this subcanvas workspace
            const innerElements = loadInnerElements(item.id);

            // Render workspace content
            const workspace = buildCanvasWorkspace(item.id, innerElements);
            workspace.style.flex = '1';
            workspace.style.overflow = 'hidden';
            element.appendChild(workspace);
        })
        .catch(err => {
            log.warn(SEG.ELEMENT, `[CanvasExpanded] Morph to fullscreen failed for ${item.id}:`, err, {
                canvasId, fromRect: { x: fromRect.x, y: fromRect.y, width: fromRect.width, height: fromRect.height },
                toRect,
            });
        });
}

/**
 * Morph fullscreen back to canvas-placed position (animated)
 */
export function morphCanvasExpandedToCanvasPlaced(
    element: HTMLElement,
    item: Element,
    onRestoreComplete: (element: HTMLElement, item: Element) => void
): void {
    log.debug(SEG.ELEMENT, `[CanvasExpanded] Minimizing ${item.id} back to canvas`);

    const currentRect = element.getBoundingClientRect();
    const origin = getCanvasOrigin(element);

    if (!origin) {
        log.error(SEG.ELEMENT, `[CanvasExpanded] No canvas origin for ${item.id}, cannot restore`);
        return;
    }

    // Clean up subcanvas selection state before collapsing
    destroyCanvasSelection(item.id);

    // Clear fullscreen content
    element.innerHTML = '';

    // Convert canvas-local coordinates to screen-space for animation target
    const screenPos = canvasToScreen(origin.canvasId, origin.x, origin.y);
    const transform = getTransform(origin.canvasId);
    const scale = transform.scale;

    const toRect = {
        x: screenPos.x,
        y: screenPos.y,
        width: origin.width * scale,
        height: origin.height * scale
    };

    beginMorphToCanvasPlaced(element, currentRect, toRect, getRestDuration())
        .then(() => {
            log.debug(SEG.ELEMENT, `[CanvasExpanded] Restore animation committed for ${item.id}`);

            // Back on the canvas, and the element now says so
            setForm(element, 'canvasPlaced');
            clearCanvasOrigin(element);

            // Remove from body
            element.remove();
            element.style.cssText = '';

            // Notify caller to reparent back to canvas
            onRestoreComplete(element, item);
        })
        .catch(err => {
            log.warn(SEG.ELEMENT, `[CanvasExpanded] Restore animation failed for ${item.id}:`, err, {
                canvasId: origin.canvasId, origin, toRect,
            });
        });
}

/**
 * Collapse fullscreen immediately without morph animation.
 * Same cleanup as morphCanvasExpandedToCanvasPlaced but instant.
 */
function collapseImmediately(
    element: HTMLElement,
    item: Element,
    onRestoreComplete: (element: HTMLElement, item: Element) => void
): void {
    log.debug(SEG.ELEMENT, `[CanvasExpanded] Instant collapse ${item.id}`);

    destroyCanvasSelection(item.id);
    element.innerHTML = '';
    setForm(element, 'canvasPlaced');
    clearCanvasOrigin(element);
    element.remove();
    element.style.cssText = '';
    onRestoreComplete(element, item);
}

/**
 * Load inner elements for a subcanvas workspace from uiState
 */
function loadInnerElements(subcanvasId: string): Element[] {
    const saved = uiState.getCanvasElements(subcanvasId);
    return saved
        .filter(g => g.symbol !== 'error')
        .map(g => {
            const entry = g.symbol ? getElementTypeBySavedSymbol(g.symbol, g.content) : undefined;
            return {
                id: g.id,
                title: entry?.title ?? 'Element',
                symbol: g.symbol,
                x: g.x,
                y: g.y,
                width: g.width,
                height: g.height,
                content: g.content,
                renderContent: () => document.createElement('div'),
            };
        });
}
