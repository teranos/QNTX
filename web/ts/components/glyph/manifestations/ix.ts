/**
 * IX Manifestation - Ingest input form window
 *
 * The IX manifestation is a window-like editor for configuring ingestion.
 * Opens when you click an IX glyph on the canvas.
 * Shows input form with preview of what would be ingested.
 */

import { log, SEG } from '../../../logger';
import { IX } from '@generated/sym.js';
import type { Glyph } from '../glyph';
import {
    setWindowState,
    getLastPosition,
    setLastPosition,
    hasProximityText,
    setProximityText
} from '../dataset';
import { beginMaximizeMorph, beginMinimizeMorph } from '../morph-transaction';
import {
    getMaximizeDuration,
    getMinimizeDuration,
    WINDOW_BORDER_RADIUS,
    WINDOW_BOX_SHADOW,
    TITLE_BAR_HEIGHT,
    TITLE_BAR_PADDING,
    WINDOW_BUTTON_SIZE
} from '../glyph';

/**
 * Morph a glyph to IX manifestation window
 */
export function morphToIx(
    glyphElement: HTMLElement,
    glyph: Glyph,
    verifyElement: (id: string, element: HTMLElement) => void,
    onRemove: (id: string) => void,
    onMinimize: (element: HTMLElement, glyph: Glyph) => void
): void {
    // AXIOM CHECK: Verify this is the correct element
    verifyElement(glyph.id, glyphElement);

    // Get current glyph position and size
    const glyphRect = glyphElement.getBoundingClientRect();

    // Calculate window target position (compact IX editor)
    const windowWidth = 400;
    const windowHeight = 300;

    // Check if we have a remembered position on the element
    const rememberedPos = getLastPosition(glyphElement);

    // Use remembered position, or default position, or center
    const targetX = rememberedPos?.x ?? (window.innerWidth - windowWidth) / 2;
    const targetY = rememberedPos?.y ?? (window.innerHeight - windowHeight) / 2;

    // Remove from canvas and reparent to body
    glyphElement.remove();

    // Clear any proximity text
    if (hasProximityText(glyphElement)) {
        glyphElement.textContent = '';
        setProximityText(glyphElement, false);
    }

    // Apply initial fixed positioning
    glyphElement.className = 'glyph-morphing-to-ix';
    glyphElement.style.position = 'fixed';
    glyphElement.style.zIndex = '1000';

    // Reparent to document body for morphing
    document.body.appendChild(glyphElement);

    // Mark element as in-window-state
    setWindowState(glyphElement, true);

    // BEGIN TRANSACTION: Start the morph animation
    beginMaximizeMorph(
        glyphElement,
        glyphRect,
        { x: targetX, y: targetY, width: windowWidth, height: windowHeight },
        getMaximizeDuration()
    ).then(() => {
        // COMMIT PHASE: Animation completed successfully
        log.debug(SEG.UI, `[IX] Animation committed for ${glyph.id}`);

        // Apply final window state
        glyphElement.style.position = 'fixed';
        glyphElement.style.left = `${targetX}px`;
        glyphElement.style.top = `${targetY}px`;
        glyphElement.style.width = `${windowWidth}px`;
        glyphElement.style.height = `${windowHeight}px`;
        glyphElement.style.borderRadius = WINDOW_BORDER_RADIUS;
        glyphElement.style.backgroundColor = 'var(--bg-primary)';
        glyphElement.style.boxShadow = WINDOW_BOX_SHADOW;
        glyphElement.style.padding = '0';
        glyphElement.style.opacity = '1';

        // Set up window as flex container
        glyphElement.style.display = 'flex';
        glyphElement.style.flexDirection = 'column';

        // Add window chrome (title bar, controls)
        const titleBar = document.createElement('div');
        titleBar.className = 'window-title-bar';
        titleBar.style.height = TITLE_BAR_HEIGHT;
        titleBar.style.backgroundColor = 'var(--bg-secondary)';
        titleBar.style.borderBottom = '1px solid var(--border-color)';
        titleBar.style.display = 'flex';
        titleBar.style.alignItems = 'center';
        titleBar.style.padding = TITLE_BAR_PADDING;
        titleBar.style.flexShrink = '0';

        // Add title with IX symbol
        const titleText = document.createElement('span');
        titleText.textContent = `${IX} ${glyph.title}`;
        titleText.style.flex = '1';
        titleBar.appendChild(titleText);

        // Add minimize button
        const minimizeBtn = document.createElement('button');
        minimizeBtn.textContent = '−';
        minimizeBtn.style.width = WINDOW_BUTTON_SIZE;
        minimizeBtn.style.height = WINDOW_BUTTON_SIZE;
        minimizeBtn.style.border = 'none';
        minimizeBtn.style.background = 'transparent';
        minimizeBtn.style.cursor = 'pointer';
        minimizeBtn.onclick = () => morphFromIx(
            glyphElement,
            glyph,
            verifyElement,
            onMinimize
        );
        titleBar.appendChild(minimizeBtn);

        // Add close button
        const closeBtn = document.createElement('button');
        closeBtn.textContent = '✕';
        closeBtn.style.width = WINDOW_BUTTON_SIZE;
        closeBtn.style.height = WINDOW_BUTTON_SIZE;
        closeBtn.style.border = 'none';
        closeBtn.style.background = 'transparent';
        closeBtn.style.cursor = 'pointer';
        closeBtn.onclick = () => {
            glyphElement.remove();
            onRemove(glyph.id);
        };
        titleBar.appendChild(closeBtn);

        glyphElement.appendChild(titleBar);

        // Add content (IX input form)
        try {
            const content = renderIxForm();
            content.style.flex = '1';
            content.style.overflow = 'auto';
            glyphElement.appendChild(content);
        } catch (error) {
            log.error(SEG.UI, `[IX ${glyph.id}] Error rendering content:`, error);
        }
    }).catch(error => {
        log.warn(SEG.UI, `[IX] Animation failed for ${glyph.id}:`, error);
    });
}

/**
 * Morph IX manifestation back to glyph (dot)
 */
export function morphFromIx(
    ixElement: HTMLElement,
    glyph: Glyph,
    verifyElement: (id: string, element: HTMLElement) => void,
    onMorphComplete: (element: HTMLElement, glyph: Glyph) => void
): void {
    // AXIOM CHECK: Verify this is the correct element
    verifyElement(glyph.id, ixElement);
    log.debug(SEG.UI, `[IX] Minimizing ${glyph.id}`);

    // Remember position
    setLastPosition(
        ixElement,
        parseInt(ixElement.style.left),
        parseInt(ixElement.style.top)
    );

    // Get current window state
    const currentRect = ixElement.getBoundingClientRect();

    // Clear window content
    ixElement.innerHTML = '';
    ixElement.textContent = '';

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
    beginMinimizeMorph(ixElement, currentRect, { x: targetX, y: targetY }, getMinimizeDuration())
        .then(() => {
            log.debug(SEG.UI, `[IX] Animation complete for ${glyph.id}`);

            // Clear state
            setWindowState(ixElement, false);
            setProximityText(ixElement, false);

            // Remove from body
            ixElement.remove();

            // Clear styles
            ixElement.style.cssText = '';

            // Apply glyph class
            ixElement.className = 'glyph-run-glyph';

            // Re-attach to indicator container
            onMorphComplete(ixElement, glyph);
        })
        .catch(error => {
            log.warn(SEG.UI, `[IX] Animation failed for ${glyph.id}:`, error);
        });
}

/**
 * Render IX input form (internal)
 * Returns the HTML element for the IX input form
 */
function renderIxForm(): HTMLElement {
    const container = document.createElement('div');
    container.className = 'ix-form-content';
    container.style.padding = '16px';
    container.style.display = 'flex';
    container.style.flexDirection = 'column';
    container.style.gap = '12px';
    container.style.height = '100%';

    // Label
    const label = document.createElement('label');
    label.textContent = 'Source:';
    label.style.fontSize = '14px';
    label.style.fontWeight = '500';
    label.style.color = 'var(--text-primary)';

    // Textarea input
    const textarea = document.createElement('textarea');
    textarea.className = 'ix-input-textarea';
    textarea.placeholder = 'Enter URL, file path, or data source...\n\nExamples:\n• https://api.example.com/data\n• file:///path/to/data.json\n• /local/path/to/file';
    textarea.rows = 8;
    textarea.style.width = '100%';
    textarea.style.padding = '8px';
    textarea.style.fontSize = '13px';
    textarea.style.fontFamily = 'monospace';
    textarea.style.backgroundColor = 'var(--bg-tertiary)';
    textarea.style.color = 'var(--text-primary)';
    textarea.style.border = '1px solid var(--border-color)';
    textarea.style.borderRadius = '4px';
    textarea.style.resize = 'vertical';
    textarea.style.boxSizing = 'border-box';

    // Button container
    const buttonContainer = document.createElement('div');
    buttonContainer.style.display = 'flex';
    buttonContainer.style.gap = '8px';
    buttonContainer.style.justifyContent = 'flex-end';

    // Execute button
    const executeBtn = document.createElement('button');
    executeBtn.textContent = 'Execute';
    executeBtn.className = 'ix-execute-button';
    executeBtn.style.padding = '8px 16px';
    executeBtn.style.fontSize = '13px';
    executeBtn.style.fontWeight = '500';
    executeBtn.style.backgroundColor = 'var(--accent-primary)';
    executeBtn.style.color = 'var(--text-primary)';
    executeBtn.style.border = 'none';
    executeBtn.style.borderRadius = '4px';
    executeBtn.style.cursor = 'pointer';

    executeBtn.addEventListener('click', () => {
        const input = textarea.value.trim();
        if (!input) {
            log.debug(SEG.UI, '[IX] No input provided');
            return;
        }

        log.debug(SEG.UI, `[IX] Executing: ${input}`);
        // TODO: Wire up to ix backend execution
        // For now, just log
        alert(`IX execution not yet wired up.\n\nInput: ${input}\n\nThis will be sent to the ix backend.`);
    });

    buttonContainer.appendChild(executeBtn);

    // Assemble form
    container.appendChild(label);
    container.appendChild(textarea);
    container.appendChild(buttonContainer);

    return container;
}
