/**
 * Canvas Manifestation - Fullscreen, no chrome
 *
 * The canvas manifestation morphs a glyph to fill the entire viewport
 * with no window chrome, title bar, or padding. Used for spatial workspaces,
 * overlays, and other full-screen experiences.
 */

import { log, SEG } from '../../../logger';
import type { Glyph } from '../glyph';
import {
    setWindowState,
    hasProximityText,
    setProximityText
} from '../dataset';
import { beginMaximizeMorph, beginMinimizeMorph } from '../morph-transaction';
import { getMaximizeDuration, getMinimizeDuration } from '../glyph';

/**
 * Morph a glyph to fullscreen canvas (no chrome)
 */
export function morphToCanvas(
    glyphElement: HTMLElement,
    glyph: Glyph,
    verifyElement: (id: string, element: HTMLElement) => void,
    onMinimize: (element: HTMLElement, glyph: Glyph) => void
): void {
    // AXIOM CHECK: Verify this is the correct element
    verifyElement(glyph.id, glyphElement);

    // Get current glyph position and size
    const glyphRect = glyphElement.getBoundingClientRect();

    // Target: full viewport
    const targetX = 0;
    const targetY = 0;
    const targetWidth = window.innerWidth;
    const targetHeight = window.innerHeight;

    // Remove from indicator container and reparent to body
    glyphElement.remove();

    // Clear any proximity text
    if (hasProximityText(glyphElement)) {
        glyphElement.textContent = '';
        setProximityText(glyphElement, false);
    }

    // Apply initial fixed positioning at current state
    glyphElement.className = 'glyph-morphing-to-canvas';
    glyphElement.style.position = 'fixed';
    glyphElement.style.zIndex = '1000';

    // Reparent to document body for morphing
    document.body.appendChild(glyphElement);

    // Mark element as in-window-state (but keep glyph ID)
    setWindowState(glyphElement, true);

    // BEGIN TRANSACTION: Start the morph animation
    beginMaximizeMorph(
        glyphElement,
        glyphRect,
        { x: targetX, y: targetY, width: targetWidth, height: targetHeight },
        getMaximizeDuration()
    ).then(() => {
        // COMMIT PHASE: Animation completed successfully
        log.debug(SEG.UI, `[Canvas] Animation committed for ${glyph.id}`);

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

        // Add minimize button (floating, top-right corner)
        // TODO: Unify button sizing (32px here vs 24px in window)
        const minimizeBtn = document.createElement('button');
        minimizeBtn.textContent = '−';
        minimizeBtn.className = 'canvas-minimize-btn';
        minimizeBtn.style.position = 'fixed';
        minimizeBtn.style.top = '16px';
        minimizeBtn.style.right = '16px';
        minimizeBtn.style.width = '32px';
        minimizeBtn.style.height = '32px';
        minimizeBtn.style.border = 'none';
        minimizeBtn.style.borderRadius = '4px';
        // TODO: Extract color to CSS variable or class
        minimizeBtn.style.backgroundColor = 'var(--bg-secondary)';
        minimizeBtn.style.color = 'var(--text-primary)';
        minimizeBtn.style.cursor = 'pointer';
        minimizeBtn.style.zIndex = '10001';
        minimizeBtn.style.fontSize = '20px';
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
            log.error(SEG.UI, `[Canvas ${glyph.id}] Error rendering content:`, error);
            const errorContent = document.createElement('div');
            errorContent.style.padding = '16px';
            errorContent.style.flex = '1';
            errorContent.style.color = '#ef4444';
            errorContent.innerHTML = `
                <div style="font-weight: bold;">Error rendering content</div>
                <div style="opacity: 0.8; font-size: 12px;">${error instanceof Error ? error.message : String(error)}</div>
            `;
            glyphElement.appendChild(errorContent);
        }
    }).catch(error => {
        // ROLLBACK: Animation failed
        log.warn(SEG.UI, `[Canvas] Animation failed for ${glyph.id}:`, error);
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
    // AXIOM CHECK: Verify this is the correct element
    verifyElement(glyph.id, canvasElement);
    log.debug(SEG.UI, `[Canvas] Minimizing ${glyph.id}`);

    // Get current canvas state
    const currentRect = canvasElement.getBoundingClientRect();

    // Clear canvas content
    canvasElement.innerHTML = '';
    canvasElement.textContent = '';

    // Calculate target position (tray)
    const trayElement = document.querySelector('.glyph-run');
    let targetX = window.innerWidth - 50;
    let targetY = window.innerHeight / 2;

    if (trayElement) {
        const trayRect = trayElement.getBoundingClientRect();
        targetX = trayRect.right - 20;
        targetY = trayRect.top + trayRect.height / 2;
    }

    // Begin minimize animation
    beginMinimizeMorph(canvasElement, currentRect, { x: targetX, y: targetY }, getMinimizeDuration())
        .then(() => {
            log.debug(SEG.UI, `[Canvas] Animation complete for ${glyph.id}`);

            // Clear state
            setWindowState(canvasElement, false);
            setProximityText(canvasElement, false);

            // Remove from body
            canvasElement.remove();

            // Clear styles
            canvasElement.style.cssText = '';

            // Apply glyph class
            canvasElement.className = 'glyph-run-glyph';

            // Re-attach to indicator container
            onMorphComplete(canvasElement, glyph);
        })
        .catch(error => {
            log.warn(SEG.UI, `[Canvas] Animation failed for ${glyph.id}:`, error);
        });
}
