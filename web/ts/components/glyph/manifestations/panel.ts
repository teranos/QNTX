/**
 * Panel Manifestation - Full-width slide-in panel with overlay
 *
 * A glyph with manifestationType 'panel' morphs from its tray dot into
 * a full-width slide-in panel, emerging from the OPPOSITE edge of the
 * system drawer (#system-drawer):
 * - Desktop: system drawer at bottom -> panel slides from top
 * - Mobile: system drawer flips to top -> panel slides from bottom
 *
 * Same single DOM element axiom as window.ts — the glyph element itself
 * becomes the panel, no cloning.
 */

import { log, SEG } from '../../../logger';
import { stripHtml } from '../../../html-utils';
import type { Glyph } from '../glyph';
import {
    setWindowState,
    hasProximityText,
    setProximityText,
    setGlyphId
} from '../dataset';
import { beginMaximizeMorph, beginMinimizeMorph } from '../morph-transaction';
import {
    getMaximizeDuration,
    getMinimizeDuration,
    CANVAS_GLYPH_CONTENT_PADDING,
    PANEL_Z_INDEX
} from '../glyph';

// Type-safe element state — avoids `as any` on DOM elements
const escapeHandlers = new WeakMap<HTMLElement, (e: KeyboardEvent) => void>();
const minimizing = new WeakSet<HTMLElement>();

/**
 * Detect whether panel should slide from top or bottom.
 * If system drawer is at the bottom of viewport -> panel from top.
 * If system drawer is at the top (mobile) -> panel from bottom.
 */
function detectSlideDirection(): 'from-top' | 'from-bottom' {
    const drawer = document.getElementById('system-drawer');
    if (!drawer) return 'from-top'; // Default: desktop layout

    const rect = drawer.getBoundingClientRect();
    const viewportMid = window.innerHeight / 2;
    // If drawer center is below viewport midpoint, it's at the bottom -> slide from top
    return (rect.top + rect.height / 2) > viewportMid ? 'from-top' : 'from-bottom';
}

/**
 * Morph a glyph to a full-width panel with overlay
 */
export function morphToPanel(
    glyphElement: HTMLElement,
    glyph: Glyph,
    verifyElement: (id: string, element: HTMLElement) => void,
    onRemove: (id: string) => void,
    onMinimize: (element: HTMLElement, glyph: Glyph) => void
): void {
    // AXIOM CHECK: Verify this is the correct element
    verifyElement(glyph.id, glyphElement);

    const elements = document.querySelectorAll(`[data-glyph-id="${glyph.id}"]`);
    if (elements.length !== 1) {
        throw new Error(
            `AXIOM VIOLATION: Expected exactly 1 element for ${glyph.id}, found ${elements.length}`
        );
    }

    const glyphRect = glyphElement.getBoundingClientRect();
    const direction = detectSlideDirection();

    // Panel dimensions: full viewport width, 70% height
    const panelWidth = window.innerWidth;
    const panelHeight = Math.round(window.innerHeight * 0.7);

    // Target position based on slide direction
    const targetX = 0;
    const targetY = direction === 'from-top' ? 0 : window.innerHeight - panelHeight;

    // THE GLYPH ITSELF BECOMES THE PANEL - NO CLONING
    glyphElement.remove();

    if (hasProximityText(glyphElement)) {
        glyphElement.textContent = '';
        setProximityText(glyphElement, false);
    }

    glyphElement.className = 'glyph-morphing-to-panel';
    glyphElement.style.position = 'fixed';
    glyphElement.style.zIndex = PANEL_Z_INDEX; // Above system drawer (10002)

    document.body.appendChild(glyphElement);
    setWindowState(glyphElement, true);

    // Create overlay
    const overlay = document.createElement('div');
    overlay.className = 'glyph-panel-overlay';
    overlay.dataset.forGlyph = glyph.id;
    document.body.appendChild(overlay);
    // Fade in overlay
    requestAnimationFrame(() => overlay.classList.add('glyph-panel-overlay--visible'));

    // Close on overlay click
    overlay.addEventListener('click', () => morphFromPanel(
        glyphElement,
        glyph,
        verifyElement,
        onMinimize
    ));

    // Close on Escape
    const escapeHandler = (e: KeyboardEvent) => {
        if (e.key === 'Escape') {
            document.removeEventListener('keydown', escapeHandler);
            morphFromPanel(glyphElement, glyph, verifyElement, onMinimize);
        }
    };
    document.addEventListener('keydown', escapeHandler);
    escapeHandlers.set(glyphElement, escapeHandler);

    beginMaximizeMorph(
        glyphElement,
        glyphRect,
        { x: targetX, y: targetY, width: panelWidth, height: panelHeight },
        getMaximizeDuration()
    ).then(() => {
        log.debug(SEG.GLYPH, `[Panel] Animation committed for ${glyph.id}`);

        const directionClass = direction === 'from-top' ? 'glyph-panel--from-top' : 'glyph-panel--from-bottom';
        glyphElement.className = `glyph-panel ${directionClass}`;
        glyphElement.style.cssText = '';
        glyphElement.style.position = 'fixed';
        glyphElement.style.left = `${targetX}px`;
        glyphElement.style.top = `${targetY}px`;
        glyphElement.style.width = `${panelWidth}px`;
        glyphElement.style.height = `${panelHeight}px`;
        glyphElement.style.zIndex = PANEL_Z_INDEX;

        // Title bar
        const titleBar = document.createElement('div');
        titleBar.className = 'glyph-title-bar';

        const titleText = document.createElement('span');
        titleText.textContent = stripHtml(glyph.title);
        titleBar.appendChild(titleText);

        // Minimize button
        const minimizeBtn = document.createElement('button');
        minimizeBtn.textContent = '\u2212';
        minimizeBtn.onclick = () => morphFromPanel(
            glyphElement,
            glyph,
            verifyElement,
            onMinimize
        );
        titleBar.appendChild(minimizeBtn);

        // Close button if glyph has onClose
        if (glyph.onClose) {
            const closeBtn = document.createElement('button');
            closeBtn.textContent = '\u00d7';
            closeBtn.onclick = () => {
                // Clean up overlay and escape handler
                removeOverlay(glyph.id);
                const handler = escapeHandlers.get(glyphElement);
                if (handler) {
                    document.removeEventListener('keydown', handler);
                    escapeHandlers.delete(glyphElement);
                }
                onRemove(glyph.id);
                glyphElement.remove();
                try { glyph.onClose!(); } catch (error) {
                    log.error(SEG.GLYPH, `[Panel ${glyph.id}] Error in onClose:`, error);
                }
            };
            titleBar.appendChild(closeBtn);
        }

        glyphElement.appendChild(titleBar);

        // Content area
        try {
            const content = glyph.renderContent();
            content.style.padding = `${CANVAS_GLYPH_CONTENT_PADDING}px`;
            content.style.flex = '1';
            content.style.overflow = 'auto';
            glyphElement.appendChild(content);
        } catch (error) {
            log.error(SEG.GLYPH, `[Panel ${glyph.id}] Error rendering content:`, error);
            const errorContent = document.createElement('div');
            errorContent.style.padding = '8px';
            errorContent.style.flex = '1';
            errorContent.style.overflow = 'auto';
            errorContent.style.color = 'var(--color-error)';
            errorContent.style.fontFamily = 'var(--font-mono)';
            errorContent.innerHTML = `
                <div style="margin-bottom: 8px; font-weight: bold;">Error rendering content</div>
                <div style="opacity: 0.8; font-size: 12px;">${error instanceof Error ? error.message : String(error)}</div>
            `;
            glyphElement.appendChild(errorContent);
        }
    }).catch(error => {
        log.warn(SEG.GLYPH, `[Panel] Animation failed for ${glyph.id}:`, error);
        removeOverlay(glyph.id);
        const handler = escapeHandlers.get(glyphElement);
        if (handler) {
            document.removeEventListener('keydown', handler);
            escapeHandlers.delete(glyphElement);
        }
        // Reattach to tray so the glyph isn't orphaned
        setWindowState(glyphElement, false);
        glyphElement.remove();
        glyphElement.style.cssText = '';
        glyphElement.className = 'glyph-run-glyph';
        setGlyphId(glyphElement, glyph.id);
        onMinimize(glyphElement, glyph);
    });
}

/**
 * Morph a panel back into a glyph (dot)
 */
export function morphFromPanel(
    panelElement: HTMLElement,
    glyph: Glyph,
    verifyElement: (id: string, element: HTMLElement) => void,
    onMorphComplete: (element: HTMLElement, glyph: Glyph) => void
): void {
    // Re-entrance guard: overlay click + Escape can fire in quick succession
    if (minimizing.has(panelElement)) return;
    minimizing.add(panelElement);

    verifyElement(glyph.id, panelElement);
    log.debug(SEG.GLYPH, `[Panel] Minimizing ${glyph.id}`);

    const currentRect = panelElement.getBoundingClientRect();

    // Clean up escape handler
    const handler = escapeHandlers.get(panelElement);
    if (handler) {
        document.removeEventListener('keydown', handler);
        escapeHandlers.delete(panelElement);
    }

    // Fade out overlay
    removeOverlay(glyph.id);

    // Clear content
    panelElement.innerHTML = '';

    // Calculate target position (tray dot)
    const trayElement = document.querySelector('.glyph-run');
    let targetX = window.innerWidth - 50;
    let targetY = window.innerHeight / 2;
    if (trayElement) {
        const trayRect = trayElement.getBoundingClientRect();
        targetX = trayRect.right - 20;
        targetY = trayRect.top + trayRect.height / 2;
    }

    beginMinimizeMorph(panelElement, currentRect, { x: targetX, y: targetY }, getMinimizeDuration())
        .then(() => {
            log.debug(SEG.GLYPH, `[Panel] Animation complete for ${glyph.id}`);
            minimizing.delete(panelElement);
            setWindowState(panelElement, false);
            setProximityText(panelElement, false);
            panelElement.remove();
            panelElement.style.cssText = '';
            panelElement.className = 'glyph-run-glyph';
            setGlyphId(panelElement, glyph.id);
            onMorphComplete(panelElement, glyph);
        })
        .catch(error => {
            log.warn(SEG.GLYPH, `[Panel] Animation failed for ${glyph.id}:`, error);
            minimizing.delete(panelElement);
        });
}

/**
 * Remove the overlay for a given glyph
 */
function removeOverlay(glyphId: string): void {
    const overlay = document.querySelector(`.glyph-panel-overlay[data-for-glyph="${glyphId}"]`);
    if (overlay) {
        overlay.classList.remove('glyph-panel-overlay--visible');
        // Remove after fade-out transition
        setTimeout(() => overlay.remove(), 200);
    }
}
