/**
 * Shared content rendering for glyph manifestations.
 *
 * Two paths: restore from stash (same DOM nodes) or fresh render via callbacks.
 * Used by window.ts and panel.ts.
 */

import { getLogger, getLogSegment, stripHtml } from '../config';
import type { Glyph } from '../glyph';
import { CANVAS_GLYPH_CONTENT_PADDING } from '../glyph';
import { restoreContent } from './stash';

export interface RenderContentResult {
    titleBar: HTMLElement;
    contentElement: HTMLElement | null;
}

export function renderGlyphContent(
    element: HTMLElement,
    glyph: Glyph,
    logLabel: string
): RenderContentResult {
    const log = getLogger();
    const seg = getLogSegment();
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

        log.debug(seg, `[${logLabel}] Restored stashed content for ${glyph.id}`);
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
            const contentArea = document.createElement('div');
            contentArea.classList.add('glyph-content-area');
            contentArea.style.padding = `${CANVAS_GLYPH_CONTENT_PADDING}px`;
            contentArea.appendChild(content);
            element.appendChild(contentArea);
            contentElement = contentArea;
        } catch (error) {
            log.error(seg, `[${logLabel} ${glyph.id}] Error rendering content: ${error instanceof Error ? error.message : String(error)}`);
            const errorContent = document.createElement('div');
            errorContent.className = 'glyph-content-area';
            errorContent.style.color = 'var(--color-error)';
            errorContent.style.fontFamily = 'var(--font-mono)';

            const errorTitle = document.createElement('div');
            errorTitle.style.marginBottom = '8px';
            errorTitle.style.fontWeight = 'bold';
            errorTitle.textContent = 'Error rendering content';
            errorContent.appendChild(errorTitle);

            const errorMsg = document.createElement('div');
            errorMsg.style.opacity = '0.8';
            errorMsg.style.fontSize = '12px';
            errorMsg.textContent = error instanceof Error ? error.message : String(error);
            errorContent.appendChild(errorMsg);

            element.appendChild(errorContent);
            contentElement = errorContent;
        }
    }

    return { titleBar, contentElement };
}

function createGenericTitleBar(title: string): HTMLElement {
    const titleBar = document.createElement('div');
    titleBar.className = 'glyph-title-bar';
    const titleText = document.createElement('span');
    titleText.textContent = stripHtml(title);
    titleText.style.flex = '1';
    titleBar.appendChild(titleText);
    return titleBar;
}
