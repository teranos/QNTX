/**
 * Render Content — shared content-rendering for window and panel manifestations.
 *
 * After a morph animation commits, the manifestation needs to populate the
 * glyph element with a title bar and content area. This logic was duplicated
 * across window.ts and panel.ts — now lives here as the single implementation.
 *
 * Two paths:
 * 1. Stash restore: content was previously stashed (minimize → re-maximize).
 *    Restores DOM nodes in-place, finds or creates the title bar.
 * 2. Fresh render: first time this glyph is manifested. Uses renderTitleBar
 *    and renderContent callbacks from the Glyph, with an error boundary.
 */

import { log, SEG } from '../../../logger';
import { stripHtml } from '../../../html-utils';
import type { Glyph } from '../glyph';
import { restoreContent } from './stash';
import { CANVAS_GLYPH_CONTENT_PADDING } from '../glyph';

export interface RenderContentResult {
    titleBar: HTMLElement;
    contentElement: HTMLElement | null;
}

/**
 * Populate a manifested glyph element with title bar and content.
 *
 * @param element - The glyph DOM element (already positioned/styled by the manifestation)
 * @param glyph - Glyph data (title, renderTitleBar, renderContent)
 * @param logLabel - Prefix for log messages (e.g., "Window", "Panel")
 */
export function renderGlyphContent(
    element: HTMLElement,
    glyph: Glyph,
    logLabel: string
): RenderContentResult {
    const restored = restoreContent(element);

    let titleBar: HTMLElement;
    let contentElement: HTMLElement | null = null;

    if (restored) {
        // Content restored from stash — find existing title bar
        titleBar = element.querySelector('.glyph-title-bar') as HTMLElement;
        if (!titleBar) {
            // Stash had no title bar — create generic
            titleBar = createGenericTitleBar(glyph.title);
            element.insertBefore(titleBar, element.firstChild);
        }

        // Find content element (first non-title-bar child)
        for (const child of Array.from(element.children)) {
            if (child !== titleBar) {
                contentElement = child as HTMLElement;
                break;
            }
        }

        log.debug(SEG.GLYPH, `[${logLabel}] Restored stashed content for ${glyph.id}`);
    } else {
        // No stash: initial creation — use renderTitleBar/renderContent callbacks
        if (glyph.renderTitleBar) {
            titleBar = glyph.renderTitleBar();
        } else {
            titleBar = createGenericTitleBar(glyph.title);
        }

        element.appendChild(titleBar);

        // Add content area with error boundary
        try {
            const content = glyph.renderContent();
            content.style.padding = `${CANVAS_GLYPH_CONTENT_PADDING}px`;
            content.style.flex = '1';
            content.style.overflow = 'auto';
            element.appendChild(content);
            contentElement = content;
        } catch (error) {
            log.error(SEG.GLYPH, `[${logLabel} ${glyph.id}] Error rendering content:`, error);
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
            element.appendChild(errorContent);
            contentElement = errorContent;
        }
    }

    return { titleBar, contentElement };
}

/** Create a generic title bar with just the glyph title text. */
function createGenericTitleBar(title: string): HTMLElement {
    const titleBar = document.createElement('div');
    titleBar.className = 'glyph-title-bar';
    const titleText = document.createElement('span');
    titleText.textContent = stripHtml(title);
    titleText.style.flex = '1';
    titleBar.appendChild(titleText);
    return titleBar;
}
