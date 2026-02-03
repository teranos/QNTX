/**
 * Glyph to GridStack Widget Adapter
 *
 * This module adapts existing glyph components to work as GridStack widgets,
 * enabling them to participate in the grid-based melding system.
 */

import type { Glyph } from './glyph';
import { addGlyphToGrid } from './gridstack-melding';
import { log, SEG } from '../../logger';
import { AX, SO, IX, Pulse } from '@generated/sym.js';

/**
 * Convert a glyph element to be GridStack-compatible
 */
export function prepareGlyphForGrid(glyph: Glyph, element: HTMLElement): HTMLElement {
    // Wrap the glyph content in a GridStack-compatible container
    const wrapper = document.createElement('div');
    wrapper.className = 'grid-stack-item';
    wrapper.dataset.glyphId = glyph.id;
    wrapper.dataset.glyphSymbol = glyph.symbol || '';

    // Create content container
    const content = document.createElement('div');
    content.className = 'grid-stack-item-content';

    // Add glyph-specific classes for styling
    if (glyph.symbol) {
        content.classList.add(`glyph-type-${glyph.symbol.toLowerCase()}`);
    }

    // Handle different glyph types
    if (glyph.manifestationType === 'ax' || glyph.symbol === AX) {
        content.classList.add('ax-glyph-widget');
        addAxGlyphFeatures(content, glyph, element);
    } else if (glyph.symbol === SO) {
        content.classList.add('prompt-glyph-widget');
        addPromptGlyphFeatures(content, glyph, element);
    } else if (glyph.symbol === IX) {
        content.classList.add('ix-glyph-widget');
        addIxGlyphFeatures(content, glyph, element);
    } else {
        // Generic glyph handling
        content.appendChild(element);
    }

    wrapper.appendChild(content);

    log.debug(SEG.UI, `[Adapter] Prepared glyph ${glyph.id} for grid`, {
        symbol: glyph.symbol,
        type: glyph.manifestationType
    });

    return wrapper;
}

/**
 * Add Ax glyph specific features for grid widget
 */
function addAxGlyphFeatures(content: HTMLElement, glyph: Glyph, originalElement: HTMLElement): void {
    // Create title bar for dragging
    const titleBar = document.createElement('div');
    titleBar.className = 'glyph-title-bar';
    titleBar.innerHTML = `
        <span class="glyph-symbol">⍰</span>
        <span class="glyph-title">Ax Query</span>
        <div class="glyph-actions">
            <button class="glyph-minimize" title="Minimize">_</button>
            <button class="glyph-close" title="Close">×</button>
        </div>
    `;
    content.appendChild(titleBar);

    // Add original content
    const contentBody = document.createElement('div');
    contentBody.className = 'glyph-content-body';
    contentBody.appendChild(originalElement);
    content.appendChild(contentBody);

    // Add meld indicator
    const meldIndicator = document.createElement('div');
    meldIndicator.className = 'meld-indicator';
    meldIndicator.dataset.side = 'right'; // Ax melds on the right side
    content.appendChild(meldIndicator);

    // Handle actions
    titleBar.querySelector('.glyph-close')?.addEventListener('click', (e) => {
        e.stopPropagation();
        removeGlyphFromGrid(glyph.id);
    });
}

/**
 * Add Prompt glyph specific features for grid widget
 */
function addPromptGlyphFeatures(content: HTMLElement, glyph: Glyph, originalElement: HTMLElement): void {
    // Create title bar
    const titleBar = document.createElement('div');
    titleBar.className = 'glyph-title-bar';
    titleBar.innerHTML = `
        <span class="glyph-symbol">⌗</span>
        <span class="glyph-title">Prompt</span>
        <div class="glyph-actions">
            <button class="glyph-minimize" title="Minimize">_</button>
            <button class="glyph-close" title="Close">×</button>
        </div>
    `;
    content.appendChild(titleBar);

    // Add original content
    const contentBody = document.createElement('div');
    contentBody.className = 'glyph-content-body';
    contentBody.appendChild(originalElement);
    content.appendChild(contentBody);

    // Add meld indicator on left side (receives Ax input)
    const meldIndicator = document.createElement('div');
    meldIndicator.className = 'meld-indicator';
    meldIndicator.dataset.side = 'left'; // Prompt receives on the left
    content.appendChild(meldIndicator);

    // Handle actions
    titleBar.querySelector('.glyph-close')?.addEventListener('click', (e) => {
        e.stopPropagation();
        removeGlyphFromGrid(glyph.id);
    });
}

/**
 * Add IX glyph specific features for grid widget
 */
function addIxGlyphFeatures(content: HTMLElement, glyph: Glyph, originalElement: HTMLElement): void {
    // Create title bar
    const titleBar = document.createElement('div');
    titleBar.className = 'glyph-title-bar';
    titleBar.innerHTML = `
        <span class="glyph-symbol">⍳</span>
        <span class="glyph-title">IX Transform</span>
        <div class="glyph-actions">
            <button class="glyph-run" title="Run">▶</button>
            <button class="glyph-close" title="Close">×</button>
        </div>
    `;
    content.appendChild(titleBar);

    // Add original content
    const contentBody = document.createElement('div');
    contentBody.className = 'glyph-content-body';
    contentBody.appendChild(originalElement);
    content.appendChild(contentBody);

    // Handle actions
    titleBar.querySelector('.glyph-close')?.addEventListener('click', (e) => {
        e.stopPropagation();
        removeGlyphFromGrid(glyph.id);
    });

    titleBar.querySelector('.glyph-run')?.addEventListener('click', (e) => {
        e.stopPropagation();
        executeIxGlyph(glyph);
    });
}

/**
 * Remove a glyph from the grid
 */
function removeGlyphFromGrid(glyphId: string): void {
    const element = document.querySelector(`[data-glyph-id="${glyphId}"]`);
    if (element) {
        // GridStack will handle the removal through its API
        // This would be called from the gridstack-melding module
        element.remove();
        log.debug(SEG.UI, `[Adapter] Removed glyph ${glyphId} from grid`);
    }
}

/**
 * Execute an IX glyph
 */
function executeIxGlyph(glyph: Glyph): void {
    log.debug(SEG.UI, `[Adapter] Executing IX glyph ${glyph.id}`);
    // TODO: Implement IX execution logic
}

/**
 * Create a melded widget view
 */
export function createMeldedWidget(glyph1: Glyph, glyph2: Glyph): HTMLElement {
    const container = document.createElement('div');
    container.className = 'grid-stack-item melded-widget';
    container.dataset.meldedFrom = `${glyph1.id},${glyph2.id}`;

    const content = document.createElement('div');
    content.className = 'grid-stack-item-content melded-content';

    // Create melded title bar
    const titleBar = document.createElement('div');
    titleBar.className = 'glyph-title-bar melded-title-bar';
    titleBar.innerHTML = `
        <span class="glyph-symbol">${glyph1.symbol || '?'} ⟷ ${glyph2.symbol || '?'}</span>
        <span class="glyph-title">Melded Pipeline</span>
        <div class="glyph-actions">
            <button class="glyph-unmeld" title="Pull Apart">↔</button>
            <button class="glyph-close" title="Close">×</button>
        </div>
    `;
    content.appendChild(titleBar);

    // Create side-by-side layout for melded glyphs
    const meldedBody = document.createElement('div');
    meldedBody.className = 'melded-body';

    // Left glyph
    const leftContainer = document.createElement('div');
    leftContainer.className = 'melded-glyph-left';
    leftContainer.dataset.glyphId = glyph1.id;
    const leftContent = glyph1.renderContent();
    leftContainer.appendChild(leftContent);

    // Connection indicator
    const connection = document.createElement('div');
    connection.className = 'melded-connection';
    connection.innerHTML = '→';

    // Right glyph
    const rightContainer = document.createElement('div');
    rightContainer.className = 'melded-glyph-right';
    rightContainer.dataset.glyphId = glyph2.id;
    const rightContent = glyph2.renderContent();
    rightContainer.appendChild(rightContent);

    meldedBody.appendChild(leftContainer);
    meldedBody.appendChild(connection);
    meldedBody.appendChild(rightContainer);

    content.appendChild(meldedBody);
    container.appendChild(content);

    // Handle unmeld action
    titleBar.querySelector('.glyph-unmeld')?.addEventListener('click', (e) => {
        e.stopPropagation();
        unmeldGlyphs(glyph1.id, glyph2.id);
    });

    log.debug(SEG.UI, `[Adapter] Created melded widget for ${glyph1.id} + ${glyph2.id}`);

    return container;
}

/**
 * Unmeld glyphs
 */
function unmeldGlyphs(glyphId1: string, glyphId2: string): void {
    log.debug(SEG.UI, `[Adapter] Unmelding glyphs ${glyphId1} and ${glyphId2}`);
    // This will be handled by the gridstack-melding module
    // The actual unmeld logic is there
}

/**
 * Apply GridStack styles to the document
 */
export function injectGridStackStyles(): void {
    const styleId = 'gridstack-glyph-styles';
    if (document.getElementById(styleId)) return;

    const styles = document.createElement('style');
    styles.id = styleId;
    styles.textContent = `
        /* GridStack container adjustments */
        .grid-stack {
            background: transparent;
        }

        /* Grid item styling */
        .grid-stack-item {
            transition: left 0.3s, top 0.3s, width 0.3s, height 0.3s;
        }

        .grid-stack-item.ui-draggable-dragging {
            transition: none;
            z-index: 100;
        }

        /* Glyph widget styling */
        .grid-stack-item-content {
            background: rgba(40, 40, 40, 0.95);
            border: 1px solid rgba(255, 255, 255, 0.1);
            border-radius: 8px;
            overflow: hidden;
            display: flex;
            flex-direction: column;
            box-shadow: 0 4px 16px rgba(0, 0, 0, 0.3);
        }

        /* Title bar */
        .glyph-title-bar {
            display: flex;
            align-items: center;
            padding: 8px 12px;
            background: rgba(30, 30, 30, 0.8);
            border-bottom: 1px solid rgba(255, 255, 255, 0.1);
            cursor: move;
            user-select: none;
        }

        .glyph-symbol {
            font-size: 18px;
            margin-right: 8px;
            opacity: 0.8;
        }

        .glyph-title {
            flex: 1;
            font-size: 14px;
            font-weight: 500;
            color: rgba(255, 255, 255, 0.9);
        }

        .glyph-actions {
            display: flex;
            gap: 8px;
        }

        .glyph-actions button {
            background: none;
            border: none;
            color: rgba(255, 255, 255, 0.6);
            cursor: pointer;
            padding: 4px 8px;
            font-size: 14px;
            border-radius: 4px;
            transition: all 0.2s;
        }

        .glyph-actions button:hover {
            background: rgba(255, 255, 255, 0.1);
            color: rgba(255, 255, 255, 0.9);
        }

        /* Content body */
        .glyph-content-body {
            flex: 1;
            overflow: auto;
            padding: 12px;
        }

        /* Meld indicators */
        .meld-indicator {
            position: absolute;
            width: 4px;
            height: 100%;
            background: transparent;
            transition: background 0.3s;
            pointer-events: none;
        }

        .meld-indicator[data-side="left"] {
            left: 0;
            top: 0;
        }

        .meld-indicator[data-side="right"] {
            right: 0;
            top: 0;
        }

        .meld-indicator.active {
            background: linear-gradient(180deg,
                transparent,
                rgba(255, 150, 50, 0.5),
                rgba(255, 150, 50, 0.8),
                rgba(255, 150, 50, 0.5),
                transparent
            );
        }

        /* Melded widget styling */
        .melded-widget .melded-content {
            background: linear-gradient(135deg,
                rgba(255, 100, 50, 0.05),
                rgba(255, 150, 50, 0.05)
            );
            border: 2px solid rgba(255, 120, 50, 0.4);
        }

        .melded-title-bar {
            background: linear-gradient(90deg,
                rgba(255, 100, 50, 0.1),
                rgba(255, 150, 50, 0.1)
            );
        }

        .melded-body {
            display: flex;
            align-items: center;
            padding: 16px;
            gap: 16px;
            height: 100%;
        }

        .melded-glyph-left,
        .melded-glyph-right {
            flex: 1;
            height: 100%;
            overflow: auto;
            background: rgba(30, 30, 30, 0.5);
            border-radius: 4px;
            padding: 8px;
        }

        .melded-connection {
            font-size: 24px;
            color: rgba(255, 150, 50, 0.8);
            font-weight: bold;
        }

        /* Proximity glow effects */
        .grid-stack-item.proximity-near {
            box-shadow: 0 0 20px 5px rgba(255, 240, 150, 0.3);
        }

        .grid-stack-item.proximity-ready {
            box-shadow: 0 0 30px 10px rgba(255, 150, 50, 0.5);
        }

        .grid-stack-item.proximity-melding {
            box-shadow: 0 0 40px 15px rgba(255, 100, 50, 0.7);
        }

        /* Specific glyph type styling */
        .ax-glyph-widget .glyph-symbol {
            color: rgba(100, 200, 255, 0.8);
        }

        .prompt-glyph-widget .glyph-symbol {
            color: rgba(255, 200, 100, 0.8);
        }

        .ix-glyph-widget .glyph-symbol {
            color: rgba(150, 255, 150, 0.8);
        }

        /* Resize handle styling */
        .grid-stack-item .ui-resizable-se {
            background: rgba(255, 255, 255, 0.2);
            border-radius: 0 0 8px 0;
        }
    `;

    document.head.appendChild(styles);
}