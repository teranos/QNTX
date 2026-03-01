/**
 * Canvas Manifestation - Fullscreen, no chrome
 *
 * The canvas manifestation morphs a glyph to fill the entire viewport
 * with no window chrome, title bar, or padding. Used for spatial workspaces,
 * overlays, and other full-screen experiences.
 */

import { log, SEG } from '../../../logger';
import type { Glyph } from '../glyph';
import { beginMaximizeMorph, beginMinimizeMorph } from '../morph-transaction';
import { getMaximizeDuration, getMinimizeDuration } from '../glyph';
import { prepareMorphTo, calculateTrayTarget, resetGlyphElement } from './morphology';

/**
 * Morph a glyph to fullscreen canvas (no chrome)
 */
export function morphToCanvas(
    glyphElement: HTMLElement,
    glyph: Glyph,
    verifyElement: (id: string, element: HTMLElement) => void,
    onMinimize: (element: HTMLElement, glyph: Glyph) => void
): void {
    const glyphRect = prepareMorphTo(glyphElement, glyph, verifyElement, 'glyph-morphing-to-canvas', '1000');

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
        log.debug(SEG.GLYPH, `[Canvas] Animation committed for ${glyph.id}`);

        // Apply final fullscreen state - NO CHROME
        glyphElement.style.position = 'fixed';
        glyphElement.style.left = '0';
        glyphElement.style.top = '0';
        glyphElement.style.width = '100vw';
        glyphElement.style.height = '100vh';
        glyphElement.style.borderRadius = '0'; // No rounded corners
        glyphElement.style.backgroundColor = 'var(--bg-primary)';
        glyphElement.style.boxShadow = 'none'; // No shadow
        glyphElement.style.padding = '0'; // No padding
        glyphElement.style.opacity = '1';

        // Set up as flex container (content fills entire viewport)
        glyphElement.style.display = 'flex';
        glyphElement.style.flexDirection = 'column';
        glyphElement.classList.add('canvas-fullscreen-adjusted');

        // Add minimize button (floating, top-right corner)
        const minimizeBtn = document.createElement('button');
        minimizeBtn.textContent = '−';
        minimizeBtn.className = 'canvas-minimize-btn';
        minimizeBtn.onclick = () => morphFromCanvas(
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
            log.error(SEG.GLYPH, `[Canvas ${glyph.id}] Error rendering content:`, error);
            const errorContent = document.createElement('div');
            errorContent.style.padding = '16px';
            errorContent.style.flex = '1';
            errorContent.style.color = 'var(--color-error)';
            errorContent.innerHTML = `
                <div style="font-weight: bold;">Error rendering content</div>
                <div style="opacity: 0.8; font-size: 12px;">${error instanceof Error ? error.message : String(error)}</div>
            `;
            glyphElement.appendChild(errorContent);
        }
    }).catch(error => {
        // ROLLBACK: Animation failed
        log.warn(SEG.GLYPH, `[Canvas] Animation failed for ${glyph.id}:`, error);
    });
}

/**
 * Morph canvas back to glyph (dot)
 */
export function morphFromCanvas(
    canvasElement: HTMLElement,
    glyph: Glyph,
    verifyElement: (id: string, element: HTMLElement) => void,
    onMorphComplete: (element: HTMLElement, glyph: Glyph) => void
): void {
    verifyElement(glyph.id, canvasElement);
    log.debug(SEG.GLYPH, `[Canvas] Minimizing ${glyph.id}`);

    // Get current canvas state
    const currentRect = canvasElement.getBoundingClientRect();

    // Clear canvas content
    canvasElement.innerHTML = '';
    canvasElement.textContent = '';

    const trayTarget = calculateTrayTarget();

    beginMinimizeMorph(canvasElement, currentRect, trayTarget, getMinimizeDuration())
        .then(() => {
            resetGlyphElement(canvasElement, glyph, 'Canvas', onMorphComplete);
        })
        .catch(error => {
            log.warn(SEG.GLYPH, `[Canvas] Animation failed for ${glyph.id}:`, error);
        });
}
