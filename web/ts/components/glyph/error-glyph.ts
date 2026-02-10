/**
 * Error Glyph - Diagnostic panel for failed glyph rendering
 *
 * Error glyphs are ephemeral - they exist only in the DOM to show
 * diagnostic information and are never persisted to state.
 *
 * When a glyph fails to render (e.g., result glyph missing execution data),
 * an error glyph is spawned in its place with diagnostic context.
 */

import type { Glyph } from './glyph';
import { SO } from '@generated/sym.js';
import { log, SEG } from '../../logger';
import { uiState } from '../../state/ui';
import { applyCanvasGlyphLayout, storeCleanup, cleanupResizeObserver, runCleanup } from './glyph-interaction';
import { createPromptGlyph } from './prompt-glyph';
import { MAX_VIEWPORT_HEIGHT_RATIO, CANVAS_GLYPH_TITLE_BAR_HEIGHT } from './glyph';

/**
 * Error context for diagnostic display
 */
export interface ErrorContext {
    /** Type of error (e.g., "missing_data", "parse_failed") */
    type: string;
    /** Human-readable error message */
    message: string;
    /** Additional diagnostic data */
    details?: Record<string, unknown>;
}

/**
 * Create an error glyph showing diagnostic information
 *
 * Error glyphs are ephemeral - not added to uiState, exist only in DOM.
 * They provide in-canvas error feedback with dismiss functionality.
 *
 * @param failedGlyphId - ID of the glyph that failed to render
 * @param failedSymbol - Symbol of the failed glyph
 * @param position - Position where error glyph should appear
 * @param error - Error context with diagnostic info
 */
export function createErrorGlyph(
    failedGlyphId: string,
    failedSymbol: string,
    position: { x: number; y: number },
    error: ErrorContext
): HTMLElement {
    const element = document.createElement('div');
    element.className = 'canvas-error-glyph canvas-glyph';
    const errorId = `error-${crypto.randomUUID()}`;
    element.dataset.glyphId = errorId;
    element.dataset.glyphSymbol = 'error';

    const width = 420;
    const minHeight = 150;

    // Apply canvas layout with minHeight for auto-sizing
    applyCanvasGlyphLayout(element, { x: position.x, y: position.y, width, height: minHeight, useMinHeight: true });

    // Error styling - darker red like erroring AX glyphs
    // No background on parent - let header and content provide backgrounds
    element.style.border = '1px solid #ff6060'; // Bright red border
    element.style.color = '#e09999'; // Brighter red for content
    element.style.fontFamily = 'monospace';
    element.style.fontSize = '11px';
    element.style.overflow = 'hidden';
    element.style.display = 'flex';
    element.style.flexDirection = 'column';

    // Header with symbol, title, convert, and dismiss buttons
    const header = document.createElement('div');
    header.style.padding = '4px 4px 4px 8px';
    header.style.borderBottom = '1px solid #ff6060';
    header.style.display = 'flex';
    header.style.justifyContent = 'space-between';
    header.style.alignItems = 'center';
    header.style.backgroundColor = '#1a0f0f'; // 10% darker than bg-tertiary for error state
    header.style.cursor = 'move';
    header.style.userSelect = 'none';
    header.style.flexShrink = '0';

    // Symbol (red X) and title
    const leftSection = document.createElement('div');
    leftSection.style.display = 'flex';
    leftSection.style.alignItems = 'center';
    leftSection.style.gap = '8px';

    const symbol = document.createElement('span');
    symbol.textContent = '✕';
    symbol.style.color = '#ff6060'; // More pure red for error emphasis
    symbol.style.fontSize = '16px';
    symbol.style.fontWeight = 'bold';
    leftSection.appendChild(symbol);

    const title = document.createElement('span');
    title.style.fontWeight = 'bold';
    title.style.fontSize = '12px';
    title.style.color = '#ff6060'; // More pure red than default error text
    title.textContent = 'Glyph Rendering Error';
    leftSection.appendChild(title);

    header.appendChild(leftSection);

    // Button section
    const buttonSection = document.createElement('div');
    buttonSection.style.display = 'flex';
    buttonSection.style.gap = '4px';

    // Copy button
    const copyBtn = document.createElement('button');
    copyBtn.className = 'glyph-play-btn';
    copyBtn.textContent = '📋';
    copyBtn.title = 'Copy error details';
    copyBtn.addEventListener('click', async (e) => {
        e.stopPropagation();
        const errorText = content.textContent || '';
        await navigator.clipboard.writeText(errorText);
        log.info(SEG.GLYPH, '[ErrorGlyph] Copied error details to clipboard');
    });
    buttonSection.appendChild(copyBtn);

    // Convert to prompt button
    const convertBtn = document.createElement('button');
    convertBtn.className = 'glyph-play-btn';
    convertBtn.textContent = '⟶';
    convertBtn.title = 'Convert to prompt for debugging';
    convertBtn.addEventListener('click', async (e) => {
        e.stopPropagation();
        try {
            await convertErrorToPrompt(element, failedGlyphId, failedSymbol, error);
        } catch (err) {
            // Visual feedback on conversion failure
            convertBtn.textContent = '⚠';
            convertBtn.title = `Conversion failed: ${err instanceof Error ? err.message : String(err)}`;
            setTimeout(() => {
                convertBtn.textContent = '⟶';
                convertBtn.title = 'Convert to prompt for debugging';
            }, 3000);
        }
    });
    buttonSection.appendChild(convertBtn);

    // Dismiss button
    const dismissBtn = document.createElement('button');
    dismissBtn.className = 'glyph-play-btn';
    dismissBtn.textContent = '✕';
    dismissBtn.title = 'Dismiss and remove broken glyph from state';
    dismissBtn.addEventListener('click', (e) => {
        e.stopPropagation();
        // Remove the broken glyph from state
        uiState.removeCanvasGlyph(failedGlyphId);
        // Clean up event listeners before removing from DOM
        runCleanup(element);
        element.remove();
        log.info(SEG.GLYPH, `[ErrorGlyph] Dismissed and removed broken glyph ${failedGlyphId}`);
    });
    buttonSection.appendChild(dismissBtn);

    header.appendChild(buttonSection);
    element.appendChild(header);

    // Content area - auto-sizing
    const content = document.createElement('div');
    content.className = 'error-glyph-content';
    content.style.padding = '12px';
    content.style.whiteSpace = 'pre-wrap';
    content.style.lineHeight = '1.5';
    content.style.flex = '1';
    content.style.overflow = 'auto';
    content.style.backgroundColor = 'rgba(36, 18, 18, 0.85)'; // 15% transparency
    content.style.color = '#ff8282'; // More pure red
    content.style.fontSize = '11px';

    const lines = [
        `Failed Glyph: ${failedSymbol}`,
        `ID: ${failedGlyphId}`,
        `Position: (${position.x}, ${position.y})`,
        '',
        `Error Type: ${error.type}`,
        `Message: ${error.message}`,
    ];

    if (error.details) {
        lines.push('', 'Details:');
        for (const [key, value] of Object.entries(error.details)) {
            lines.push(`  ${key}: ${JSON.stringify(value)}`);
        }
    }

    content.textContent = lines.join('\n');
    element.appendChild(content);

    // Set up ResizeObserver for auto-sizing to content
    setupErrorGlyphResizeObserver(element, content, errorId);

    // Make draggable via header (manual implementation - no uiState persistence)
    let isDragging = false;
    let dragStartX = 0;
    let dragStartY = 0;
    let elementStartX = position.x;
    let elementStartY = position.y;

    header.addEventListener('mousedown', (e) => {
        // Don't start drag if clicking on buttons
        if (buttonSection.contains(e.target as Node)) return;

        isDragging = true;
        dragStartX = e.clientX;
        dragStartY = e.clientY;
        elementStartX = parseInt(element.style.left) || position.x;
        elementStartY = parseInt(element.style.top) || position.y;
        e.preventDefault();
    });

    const handleMouseMove = (e: MouseEvent) => {
        if (!isDragging) return;
        const container = element.parentElement;
        if (!container) return;

        const deltaX = e.clientX - dragStartX;
        const deltaY = e.clientY - dragStartY;
        const newX = elementStartX + deltaX;
        const newY = elementStartY + deltaY;

        element.style.left = `${newX}px`;
        element.style.top = `${newY}px`;
    };

    const handleMouseUp = () => {
        if (isDragging) {
            isDragging = false;
            log.debug(SEG.GLYPH, '[ErrorGlyph] Drag ended (not persisted - ephemeral)');
        }
    };

    document.addEventListener('mousemove', handleMouseMove);
    document.addEventListener('mouseup', handleMouseUp);

    // Store cleanup function for drag handlers
    storeCleanup(element, () => {
        document.removeEventListener('mousemove', handleMouseMove);
        document.removeEventListener('mouseup', handleMouseUp);
    });

    return element;
}

/**
 * Set up ResizeObserver to auto-size error glyph to content
 */
function setupErrorGlyphResizeObserver(
    glyphElement: HTMLElement,
    contentElement: HTMLElement,
    glyphId: string
): void {
    // Cleanup any existing observer
    cleanupResizeObserver(glyphElement);

    const titleBarHeight = CANVAS_GLYPH_TITLE_BAR_HEIGHT;
    const maxHeight = window.innerHeight * MAX_VIEWPORT_HEIGHT_RATIO;

    const resizeObserver = new ResizeObserver(entries => {
        for (const entry of entries) {
            const contentHeight = entry.contentRect.height;
            const totalHeight = Math.min(contentHeight + titleBarHeight, maxHeight);

            // Update minHeight to allow manual resize
            glyphElement.style.minHeight = `${totalHeight}px`;

            log.debug(SEG.GLYPH, `[Error ${glyphId}] Auto-resized to ${totalHeight}px (content: ${contentHeight}px)`);
        }
    });

    resizeObserver.observe(contentElement);

    // Store observer for cleanup
    (glyphElement as any).__resizeObserver = resizeObserver;
}

/**
 * Convert error glyph to prompt glyph with debugging template
 */
async function convertErrorToPrompt(
    errorElement: HTMLElement,
    failedGlyphId: string,
    failedSymbol: string,
    error: ErrorContext
): Promise<void> {
    const container = errorElement.parentElement;
    if (!container) {
        log.error(SEG.GLYPH, '[ErrorGlyph] Cannot convert - no parent container');
        return;
    }

    const canvasRect = container.getBoundingClientRect();
    const errorRect = errorElement.getBoundingClientRect();
    const x = Math.round(errorRect.left - canvasRect.left);
    const y = Math.round(errorRect.top - canvasRect.top);
    const width = Math.round(errorRect.width);
    const height = Math.round(errorRect.height);

    // Create debugging prompt template
    const promptTemplate = [
        '---',
        'model: "anthropic/claude-haiku-4.5"',
        'temperature: 0.7',
        'max_tokens: 2000',
        '---',
        '',
        '# Debug Error',
        '',
        `## Failed Glyph: ${failedSymbol}`,
        `Glyph ID: ${failedGlyphId}`,
        '',
        `## Error Type: ${error.type}`,
        `Message: ${error.message}`,
        '',
        '## Details',
        error.details ? Object.entries(error.details).map(([k, v]) => `- ${k}: ${JSON.stringify(v)}`).join('\n') : 'No additional details',
        '',
        '## Investigation',
        '',
        'Help me debug this error. What should I check?',
    ].join('\n');

    try {
        // Create new prompt glyph
        const promptGlyph: Glyph = {
            id: `prompt-${crypto.randomUUID()}`,
            title: 'Debug Prompt',
            symbol: SO,
            x, y, width, height,
            renderContent: () => {
                const el = document.createElement('div');
                el.textContent = 'Prompt glyph';
                return el;
            }
        };

        // The prompt template will be saved when createPromptGlyph sets up the glyph
        // No need to save here - uiState.addCanvasGlyph will be called with the code

        // Create and append prompt element
        const promptElement = await createPromptGlyph(promptGlyph);

        // Only remove error glyph after successful prompt creation
        uiState.removeCanvasGlyph(failedGlyphId);
        runCleanup(errorElement);
        errorElement.remove();

        container.appendChild(promptElement);

        // Update state
        uiState.addCanvasGlyph({
            id: promptGlyph.id,
            symbol: SO,
            x, y, width, height,
        });

        log.info(SEG.GLYPH, `[ErrorGlyph] Converted error to debug prompt ${promptGlyph.id}`);
    } catch (err) {
        log.error(SEG.GLYPH, '[ErrorGlyph] Failed to convert to prompt:', err);
        // Re-throw to allow caller to provide user feedback
        throw err;
    }
}
