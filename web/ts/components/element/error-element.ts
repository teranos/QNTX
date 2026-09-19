/**
 * Error Element - Diagnostic panel for failed element rendering
 *
 * Error elements are ephemeral - they exist only in the DOM to show
 * diagnostic information and are never persisted to state.
 *
 * When an element fails to render (e.g., result element missing execution data),
 * an error element is spawned in its place with diagnostic context.
 */

import type { Element } from '@teranos/elements';
import { SO } from '../../sym';
import { log, SEG } from '../../logger';
import { uiState } from '../../state/ui';
import { applyCanvasElementLayout, storeCleanup, setupElementResizeObserver, runCleanup } from '@teranos/elements';
import { createPromptElement } from './prompt-element';

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
 * Create an error element showing diagnostic information
 *
 * Error elements are ephemeral - not added to uiState, exist only in DOM.
 * They provide in-canvas error feedback with dismiss functionality.
 *
 * @param failedElementId - ID of the element that failed to render
 * @param failedSymbol - Symbol of the failed element
 * @param position - Position where error element should appear
 * @param error - Error context with diagnostic info
 */
export function createErrorElement(
    failedElementId: string,
    failedSymbol: string,
    position: { x: number; y: number },
    error: ErrorContext
): HTMLElement {
    const element = document.createElement('div');
    element.className = 'canvas-error-element canvas-element';
    const errorId = `error-${crypto.randomUUID()}`;
    element.dataset.elementId = errorId;
    element.dataset.symbol = 'error';

    const width = 420;
    const minHeight = 150;

    // Apply canvas layout with minHeight for auto-sizing
    applyCanvasElementLayout(element, { x: position.x, y: position.y, width, height: minHeight, useMinHeight: true });

    // Error styling - darker red like erroring AX elements
    // No background on parent - let header and content provide backgrounds
    element.style.border = '1px solid #ff6060'; // Bright red border
    element.style.color = '#e09999'; // Brighter red for content
    element.style.fontFamily = 'var(--font-mono)';
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
    title.textContent = 'Element Rendering Error';
    leftSection.appendChild(title);

    header.appendChild(leftSection);

    // Button section
    const buttonSection = document.createElement('div');
    buttonSection.style.display = 'flex';
    buttonSection.style.gap = '4px';

    // Copy button
    const copyBtn = document.createElement('button');
    copyBtn.className = 'titlebar-btn';
    copyBtn.textContent = '📋';
    copyBtn.title = 'Copy error details';
    copyBtn.addEventListener('click', async (e) => {
        e.stopPropagation();
        const errorText = content.textContent || '';
        await navigator.clipboard.writeText(errorText);
        log.info(SEG.ELEMENT, '[ErrorElement] Copied error details to clipboard');
    });
    buttonSection.appendChild(copyBtn);

    // Convert to prompt button
    const convertBtn = document.createElement('button');
    convertBtn.className = 'titlebar-btn';
    convertBtn.textContent = '⟶';
    convertBtn.title = 'Convert to prompt for debugging';
    convertBtn.addEventListener('click', async (e) => {
        e.stopPropagation();
        try {
            await convertErrorToPrompt(element, failedElementId, failedSymbol, error);
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
    dismissBtn.className = 'titlebar-btn';
    dismissBtn.textContent = '✕';
    dismissBtn.title = 'Dismiss and remove broken element from state';
    dismissBtn.addEventListener('click', (e) => {
        e.stopPropagation();
        // Remove the broken element from state
        uiState.removeCanvasElement(failedElementId);
        // Clean up event listeners before removing from DOM
        runCleanup(element);
        element.remove();
        log.info(SEG.ELEMENT, `[ErrorElement] Dismissed and removed broken element ${failedElementId}`);
    });
    buttonSection.appendChild(dismissBtn);

    header.appendChild(buttonSection);
    element.appendChild(header);

    // Content area - auto-sizing
    const content = document.createElement('div');
    content.className = 'error-element-content content-area';
    content.style.padding = '12px';
    content.style.whiteSpace = 'pre-wrap';
    content.style.lineHeight = '1.5';
    content.style.backgroundColor = 'rgba(36, 18, 18, 0.85)'; // 15% transparency
    content.style.color = '#ff8282'; // More pure red
    content.style.fontSize = '11px';

    const lines = [
        `Failed Element: ${failedSymbol}`,
        `ID: ${failedElementId}`,
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
    setupElementResizeObserver(element, content, `Error ${errorId}`);

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
            log.debug(SEG.ELEMENT, '[ErrorElement] Drag ended (not persisted - ephemeral)');
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
 * Convert error element to prompt element with debugging template
 */
async function convertErrorToPrompt(
    errorElement: HTMLElement,
    failedElementId: string,
    failedSymbol: string,
    error: ErrorContext
): Promise<void> {
    const container = errorElement.parentElement;
    if (!container) {
        log.error(SEG.ELEMENT, '[ErrorElement] Cannot convert - no parent container');
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
        `## Failed Element: ${failedSymbol}`,
        `Element ID: ${failedElementId}`,
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
        // Create new prompt element
        const promptItem: Element = {
            id: `prompt-${crypto.randomUUID()}`,
            title: 'Debug Prompt',
            symbol: SO,
            x, y, width, height,
            renderContent: () => {
                const el = document.createElement('div');
                el.textContent = 'Prompt element';
                return el;
            }
        };

        // Add to uiState BEFORE createPromptElement (setupPromptElement reads content from uiState)
        uiState.addCanvasElement({
            id: promptItem.id,
            symbol: SO,
            x, y, width, height,
            content: promptTemplate,
        });

        // Create and append prompt element
        const promptElement = await createPromptElement(promptItem);

        // Only remove error element after successful prompt creation
        uiState.removeCanvasElement(failedElementId);
        runCleanup(errorElement);
        errorElement.remove();

        container.appendChild(promptElement);

        log.info(SEG.ELEMENT, `[ErrorElement] Converted error to debug prompt ${promptItem.id}`);
    } catch (err) {
        log.error(SEG.ELEMENT, '[ErrorElement] Failed to convert to prompt:', err);
        // Re-throw to allow caller to provide user feedback
        throw err;
    }
}
