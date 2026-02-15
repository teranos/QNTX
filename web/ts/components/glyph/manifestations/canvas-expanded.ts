/**
 * Canvas-Expanded Manifestation — canvas-placed ↔ fullscreen morph path
 *
 * General capability: any canvas-placed glyph can morph to fullscreen and back.
 * The subcanvas glyph is the first consumer.
 *
 * Element Axiom: the glyph element is reparented (not recreated):
 * - Canvas-placed: child of `.canvas-content-layer` (inside CSS transform)
 * - Fullscreen: child of `document.body` (escapes CSS transform for pan/zoom)
 *
 * Two minimize paths:
 * - Default: morph back to compact canvas position (morphFullscreenToCanvasPlaced)
 * - Escape to tray: morph to glyph-run dot (future — uses morphFromCanvas)
 */

import { log, SEG } from '../../../logger';
import type { Glyph } from '../glyph';
import { getMaximizeDuration, getMinimizeDuration } from '../glyph';
import { setWindowState, setCanvasOrigin, getCanvasOrigin, clearCanvasOrigin } from '../dataset';
import { beginMaximizeMorph, beginRestoreMorph } from '../morph-transaction';
import { canvasToScreen, getTransform } from '../canvas/canvas-pan';
import { buildCanvasWorkspace } from '../canvas/canvas-workspace-builder';
import { uiState } from '../../../state/ui';
import { getGlyphTypeBySymbol } from '../glyph-registry';
import { destroyCanvasSelection } from '../canvas/selection';
import { pushBreadcrumb, popBreadcrumb, buildBreadcrumbBar } from '../canvas/breadcrumb';

/**
 * Morph a canvas-placed glyph to fullscreen workspace
 *
 * @param element - The glyph's persistent DOM element (Axiom: same element throughout)
 * @param glyph - Glyph data (id, position, size)
 * @param canvasId - The parent canvas ID (for coordinate conversion on return)
 * @param onMinimize - Called when the glyph is minimized back to canvas-placed
 */
export function morphCanvasPlacedToFullscreen(
    element: HTMLElement,
    glyph: Glyph,
    canvasId: string,
    onMinimize: (element: HTMLElement, glyph: Glyph) => void
): void {
    // Capture current screen-space rect (accounts for CSS transform from pan/zoom)
    const fromRect = element.getBoundingClientRect();

    // Store canvas-local origin for return morph
    setCanvasOrigin(element, {
        x: glyph.x ?? 0,
        y: glyph.y ?? 0,
        width: glyph.width ?? 180,
        height: glyph.height ?? 120,
        canvasId
    });

    // Remove from canvas content layer and reparent to body
    element.remove();
    element.className = 'glyph-morphing-to-canvas';
    element.style.position = 'fixed';
    element.style.zIndex = '1000';
    element.innerHTML = '';
    document.body.appendChild(element);

    setWindowState(element, true);

    // Target: full viewport
    const toRect = { x: 0, y: 0, width: window.innerWidth, height: window.innerHeight };

    beginMaximizeMorph(element, fromRect, toRect, getMaximizeDuration())
        .then(() => {
            log.debug(SEG.GLYPH, `[CanvasExpanded] Morph to fullscreen committed for ${glyph.id}`);

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
            element.className = 'canvas-subcanvas-glyph-expanded';

            const doMinimize = (instant: boolean = false) => {
                element.removeEventListener('keydown', escapeHandler);
                // Only pop when directly minimized (button/Escape).
                // Cascade via jumpToBreadcrumb splices the stack itself.
                if (!instant) {
                    popBreadcrumb();
                }

                if (instant) {
                    collapseImmediately(element, glyph, onMinimize);
                } else {
                    morphFullscreenToCanvasPlaced(element, glyph, onMinimize);
                }
            };

            // Push breadcrumb entry
            pushBreadcrumb({
                canvasId: glyph.id,
                name: glyph.content || 'subcanvas',
                minimize: doMinimize,
            });

            // Build breadcrumb bar with minimize button inside it
            const breadcrumbBar = buildBreadcrumbBar();

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

            // Load inner glyphs for this subcanvas workspace
            const innerGlyphs = loadInnerGlyphs(glyph.id);

            // Render workspace content
            const workspace = buildCanvasWorkspace(glyph.id, innerGlyphs);
            workspace.style.flex = '1';
            workspace.style.overflow = 'hidden';
            element.appendChild(workspace);
        })
        .catch(err => {
            log.warn(SEG.GLYPH, `[CanvasExpanded] Morph to fullscreen failed for ${glyph.id}:`, err, {
                canvasId, fromRect: { x: fromRect.x, y: fromRect.y, width: fromRect.width, height: fromRect.height },
                toRect,
            });
        });
}

/**
 * Morph fullscreen back to canvas-placed position (animated)
 */
export function morphFullscreenToCanvasPlaced(
    element: HTMLElement,
    glyph: Glyph,
    onRestoreComplete: (element: HTMLElement, glyph: Glyph) => void
): void {
    log.debug(SEG.GLYPH, `[CanvasExpanded] Minimizing ${glyph.id} back to canvas`);

    const currentRect = element.getBoundingClientRect();
    const origin = getCanvasOrigin(element);

    if (!origin) {
        log.error(SEG.GLYPH, `[CanvasExpanded] No canvas origin for ${glyph.id}, cannot restore`);
        return;
    }

    // Clean up subcanvas selection state before collapsing
    destroyCanvasSelection(glyph.id);

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

    beginRestoreMorph(element, currentRect, toRect, getMinimizeDuration())
        .then(() => {
            log.debug(SEG.GLYPH, `[CanvasExpanded] Restore animation committed for ${glyph.id}`);

            // Clear morph state
            setWindowState(element, false);
            clearCanvasOrigin(element);

            // Remove from body
            element.remove();
            element.style.cssText = '';

            // Notify caller to reparent back to canvas
            onRestoreComplete(element, glyph);
        })
        .catch(err => {
            log.warn(SEG.GLYPH, `[CanvasExpanded] Restore animation failed for ${glyph.id}:`, err, {
                canvasId: origin.canvasId, origin, toRect,
            });
        });
}

/**
 * Collapse fullscreen immediately without morph animation.
 * Same cleanup as morphFullscreenToCanvasPlaced but instant.
 */
function collapseImmediately(
    element: HTMLElement,
    glyph: Glyph,
    onRestoreComplete: (element: HTMLElement, glyph: Glyph) => void
): void {
    log.debug(SEG.GLYPH, `[CanvasExpanded] Instant collapse ${glyph.id}`);

    destroyCanvasSelection(glyph.id);
    element.innerHTML = '';
    setWindowState(element, false);
    clearCanvasOrigin(element);
    element.remove();
    element.style.cssText = '';
    onRestoreComplete(element, glyph);
}

/**
 * Load inner glyphs for a subcanvas workspace from uiState
 */
function loadInnerGlyphs(subcanvasId: string): Glyph[] {
    const saved = uiState.getCanvasGlyphs(subcanvasId);
    return saved
        .filter(g => g.symbol !== 'error')
        .map(g => {
            const entry = g.symbol ? getGlyphTypeBySymbol(g.symbol) : undefined;
            return {
                id: g.id,
                title: entry?.title ?? 'Glyph',
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
