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
import type { Glyph } from '../glyph';
import { addWindowControls } from './title-bar-controls';
import { stashContent } from './stash';
import { renderGlyphContent } from './render-content';
import {
    setWindowState,
    setGlyphId
} from '../dataset';
import { prepareMorphTo, calculateTrayTarget, resetGlyphElement } from './morphology';
import { beginMaximizeMorph, beginMinimizeMorph } from '../morph-transaction';
import {
    getMaximizeDuration,
    getMinimizeDuration,
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
    const glyphRect = prepareMorphTo(glyphElement, glyph, verifyElement, 'glyph-morphing-to-panel', PANEL_Z_INDEX);

    const direction = detectSlideDirection();

    // Panel dimensions: full viewport width, 70% height
    const panelWidth = window.innerWidth;
    const panelHeight = Math.round(window.innerHeight * 0.7);

    // Target position based on slide direction
    const targetX = 0;
    const targetY = direction === 'from-top' ? 0 : window.innerHeight - panelHeight;

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

        // Restore stashed content or render fresh (shared with window.ts)
        const { titleBar } = renderGlyphContent(glyphElement, glyph, 'Panel');

        // Add window controls (minimize/close) to the title bar
        addWindowControls(titleBar, {
            onMinimize: () => morphFromPanel(glyphElement, glyph, verifyElement, onMinimize),
            onClose: glyph.onClose ? () => {
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
            } : undefined,
        });
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

    // Stash content (strips window controls, preserves glyph identity off-DOM)
    stashContent(panelElement);

    const trayTarget = calculateTrayTarget();

    beginMinimizeMorph(panelElement, currentRect, trayTarget, getMinimizeDuration())
        .then(() => {
            minimizing.delete(panelElement);
            resetGlyphElement(panelElement, glyph, 'Panel', onMorphComplete);
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
