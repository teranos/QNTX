/**
 * Workspace Manifestation — the canvas itself, edge to edge, no chrome.
 *
 * Its row in MANIFESTATIONS is `workspace`: `canvas` sat one suffix from
 * `canvasPlaced` and meant the opposite thing, the surface rather than a glyph
 * on it. canvas-glyph.ts already gives this one `id: 'canvas-workspace'`.
 *
 * The canvas manifestation morphs a glyph to fill the entire viewport
 * with no window chrome, title bar, or padding. Used for spatial workspaces,
 * overlays, and other full-screen experiences.
 */

import { getLogger, getLogSegment } from '../config';
import { type Glyph, DEFAULT_GLYPH_COLOR, DEFAULT_GLYPH_TEXT_COLOR } from '../glyph';
import { beginMaximizeMorph, beginMorphToDot } from '../morph-transaction';
import { getMaximizeDuration, getMinimizeDuration } from '../glyph';
import { prepareMorphTo, calculateTrayTarget, resetGlyphElement } from './morphology';

/**
 * Morph a glyph to fullscreen canvas (no chrome)
 */
export function morphDotToWorkspace(
    glyphElement: HTMLElement,
    glyph: Glyph,
    verifyElement: (id: string, element: HTMLElement) => void,
    onMinimize: (element: HTMLElement, glyph: Glyph) => void
): void {
    const log = getLogger();
    const seg = getLogSegment();
    const morph = prepareMorphTo(glyphElement, glyph, verifyElement, 'workspace', 'glyph-morphing-to-canvas', '1000');
    const glyphRect = morph.rect;

    // Target: full viewport
    const targetX = 0;
    const targetY = 0;
    const targetWidth = window.innerWidth;
    const targetHeight = window.innerHeight;

    // BEGIN TRANSACTION: Start the morph animation
    beginMaximizeMorph(
        glyphElement,
        glyphRect,
        { x: targetX, y: targetY, width: targetWidth, height: targetHeight },
        getMaximizeDuration()
    ).then(() => {
        // COMMIT PHASE: Animation completed successfully
        log.debug(seg, `[Canvas] Animation committed for ${glyph.id}`);

        // Apply final fullscreen state - NO CHROME
        glyphElement.style.position = 'fixed';
        glyphElement.style.left = '0';
        glyphElement.style.top = '0';
        glyphElement.style.width = '100vw';
        glyphElement.style.height = '100vh';
        glyphElement.style.borderRadius = '0'; // No rounded corners
        glyphElement.style.backgroundColor = glyph.color ?? DEFAULT_GLYPH_COLOR;
        if (glyph.border) glyphElement.style.border = glyph.border;
        glyphElement.style.backdropFilter = 'blur(2px)';
        glyphElement.style.color = glyph.textColor ?? DEFAULT_GLYPH_TEXT_COLOR;
        glyphElement.style.boxShadow = 'none'; // No shadow
        glyphElement.style.padding = '0'; // No padding
        glyphElement.style.opacity = '1';

        // Set up as flex container (content fills entire viewport)
        glyphElement.style.display = 'flex';
        glyphElement.style.flexDirection = 'column';
        // Morph class leaves with the morph; settled fullscreen class stays
        morph.commitClass('canvas-fullscreen-adjusted');

        // Add minimize button (floating, top-right corner)
        const minimizeBtn = document.createElement('button');
        minimizeBtn.textContent = '\u2212';
        minimizeBtn.className = 'canvas-minimize-btn';
        minimizeBtn.onclick = () => morphWorkspaceToDot(
            glyphElement,
            glyph,
            verifyElement,
            onMinimize
        );
        glyphElement.appendChild(minimizeBtn);

        // Add content (fills viewport)
        try {
            const content = glyph.renderContent();
            content.style.flex = '1'; // Take all space
            content.style.overflow = 'hidden';
            glyphElement.appendChild(content);
        } catch (error) {
            log.error(seg, `[Canvas ${glyph.id}] Error rendering content: ${error instanceof Error ? error.message : String(error)}`);
            const errorContent = document.createElement('div');
            errorContent.style.padding = '16px';
            errorContent.style.flex = '1';
            errorContent.style.color = 'var(--color-error)';

            const errorTitle = document.createElement('div');
            errorTitle.style.fontWeight = 'bold';
            errorTitle.textContent = 'Error rendering content';
            errorContent.appendChild(errorTitle);

            const errorMsg = document.createElement('div');
            errorMsg.style.opacity = '0.8';
            errorMsg.style.fontSize = '12px';
            errorMsg.textContent = error instanceof Error ? error.message : String(error);
            errorContent.appendChild(errorMsg);

            glyphElement.appendChild(errorContent);
        }
    }).catch(error => {
        // ROLLBACK: Animation failed — the glyph keeps the classes it had
        log.warn(seg, `[Canvas] Animation failed for ${glyph.id}: ${error instanceof Error ? error.message : String(error)}`);
        morph.rollbackClass();
    });
}

/**
 * Morph canvas back to glyph (dot)
 */
export function morphWorkspaceToDot(
    canvasElement: HTMLElement,
    glyph: Glyph,
    verifyElement: (id: string, element: HTMLElement) => void,
    onMorphComplete: (element: HTMLElement, glyph: Glyph) => void
): void {
    const log = getLogger();
    const seg = getLogSegment();
    verifyElement(glyph.id, canvasElement);
    log.debug(seg, `[Canvas] Minimizing ${glyph.id}`);

    // Get current canvas state
    const currentRect = canvasElement.getBoundingClientRect();

    // Clear canvas content
    canvasElement.innerHTML = '';
    canvasElement.textContent = '';

    const trayTarget = calculateTrayTarget(glyph.id);

    beginMorphToDot(canvasElement, currentRect, trayTarget, getMinimizeDuration())
        .then(() => {
            resetGlyphElement(canvasElement, glyph, 'Canvas', onMorphComplete);
        })
        .catch(error => {
            log.warn(seg, `[Canvas] Animation failed for ${glyph.id}: ${error instanceof Error ? error.message : String(error)}`);
        });
}

/**
 * @deprecated Renamed to {@link morphDotToWorkspace} — the canvas it named is the workspace, one suffix from `canvasPlaced` and the opposite thing.
 *
 * Every morph now says both ends, in the names the table holds. This is the
 * same function, so a consumer still on it is unaffected.
 */
export const morphToCanvas: typeof morphDotToWorkspace = morphDotToWorkspace;

/**
 * @deprecated Renamed to {@link morphWorkspaceToDot} — same canvas, same suffix, and the destination was unsaid.
 *
 * Every morph now says both ends, in the names the table holds. This is the
 * same function, so a consumer still on it is unaffected.
 */
export const morphFromCanvas: typeof morphWorkspaceToDot = morphWorkspaceToDot;
