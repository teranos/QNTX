/**
 * IX Glyph - Ingest form directly on canvas
 *
 * Shows textarea with ix argument (URL, file path) and execute button.
 * Editable directly on canvas - no hidden windows, no extra clicks.
 *
 * Execution Architecture:
 * - IX glyphs execute via Pulse scheduling (one-time jobs)
 * - Play button wraps input as `ix ${source}` and calls forceTriggerJob()
 * - Uses existing /api/pulse/schedules endpoint with interval_seconds: 0
 * - Job execution creates attestations which appear in main attestation store
 * - Observability: IX jobs tracked in Pulse UI alongside scheduled ATS blocks
 *
 * Design Parallel with Prose:
 * - Prose has ATS code blocks that create scheduled Pulse jobs
 * - Canvas has IX glyphs that create one-time Pulse jobs
 * - Both use same backend execution path (Pulse scheduler)
 * - Difference: ATS blocks can be recurring, IX glyphs are always one-shot
 * - Future: IX glyphs could also support scheduling (recurring ingestion)
 *
 * Future enhancements:
 * - Show preview of attestations before execution (dry-run mode)
 * - Display type of ix operation inferred from input (URL, file path, API)
 * - Poll job status and show progress badge (queued → running → complete)
 * - Create result glyph on completion showing attestation count
 * - Link to created attestations for exploration
 * - Optional: Add scheduling UI like ATS blocks (recurring ingestion)
 */

import type { Glyph } from './glyph';
import { IX } from '@generated/sym.js';
import { log, SEG } from '../../logger';
import { uiState } from '../../state/ui';
import { GRID_SIZE } from './grid-constants';
import { forceTriggerJob } from '../../pulse/api';

/**
 * Create an IX glyph with input form on canvas
 */
export async function createIxGlyph(glyph: Glyph): Promise<HTMLElement> {
    const element = document.createElement('div');
    element.className = 'canvas-ix-glyph';
    element.dataset.glyphId = glyph.id;

    const gridX = glyph.gridX ?? 5;
    const gridY = glyph.gridY ?? 5;

    // Default size for IX glyph
    const width = glyph.width ?? 360;
    const height = glyph.height ?? 180;

    // Style element
    element.style.position = 'absolute';
    element.style.left = `${gridX * GRID_SIZE}px`;
    element.style.top = `${gridY * GRID_SIZE}px`;
    element.style.width = `${width}px`;
    element.style.height = `${height}px`;
    element.style.backgroundColor = 'var(--bg-secondary)';
    element.style.border = '1px solid var(--border-color)';
    element.style.borderRadius = '4px';
    element.style.display = 'flex';
    element.style.flexDirection = 'column';
    element.style.overflow = 'hidden';

    // Textarea (declared early so play button can reference it)
    const textarea = document.createElement('textarea');
    textarea.placeholder = 'Enter URL, file path, or data source...';
    textarea.style.flex = '1';
    textarea.style.padding = '8px';
    textarea.style.fontSize = '13px';
    textarea.style.fontFamily = 'monospace';
    textarea.style.backgroundColor = '#1a1b1a';
    textarea.style.color = '#a8e6a1';
    textarea.style.border = '1px solid var(--border-color)';
    textarea.style.borderRadius = '4px';
    textarea.style.resize = 'none';

    // Prevent drag from starting on textarea
    textarea.addEventListener('mousedown', (e) => {
        e.stopPropagation();
    });

    // Title bar
    const titleBar = document.createElement('div');
    titleBar.className = 'canvas-glyph-title-bar';
    titleBar.style.height = '32px';
    titleBar.style.backgroundColor = 'var(--bg-tertiary)';
    titleBar.style.borderBottom = '1px solid var(--border-color)';
    titleBar.style.display = 'flex';
    titleBar.style.alignItems = 'center';
    titleBar.style.padding = '0 8px';
    titleBar.style.gap = '8px';
    titleBar.style.cursor = 'move';
    titleBar.style.flexShrink = '0';

    const symbol = document.createElement('span');
    symbol.textContent = IX;
    symbol.style.fontSize = '16px';

    const title = document.createElement('span');
    title.textContent = 'Ingest';
    title.style.fontSize = '13px';
    title.style.flex = '1';

    // Play button
    const playBtn = document.createElement('button');
    playBtn.textContent = '▶';
    playBtn.style.width = '24px';
    playBtn.style.height = '24px';
    playBtn.style.padding = '0';
    playBtn.style.fontSize = '12px';
    playBtn.style.backgroundColor = 'var(--bg-secondary)';
    playBtn.style.color = 'var(--text-primary)';
    playBtn.style.border = '1px solid var(--border-color)';
    playBtn.style.borderRadius = '4px';
    playBtn.style.cursor = 'pointer';
    playBtn.style.display = 'flex';
    playBtn.style.alignItems = 'center';
    playBtn.style.justifyContent = 'center';
    playBtn.title = 'Execute';

    playBtn.addEventListener('click', async (e) => {
        e.stopPropagation();
        const input = textarea.value.trim();
        if (!input) {
            log.debug(SEG.UI, '[IX] No input provided');
            return;
        }

        log.debug(SEG.UI, `[IX] Executing: ${input}`);

        try {
            // Wrap input as ATS command and trigger one-time Pulse job
            const atsCode = `ix ${input}`;
            const job = await forceTriggerJob(atsCode);

            log.debug(SEG.UI, `[IX] Job created successfully`, {
                jobId: job.id,
                atsCode: atsCode
            });

            // TODO: Phase 2 - Show job status badge and poll for completion
            // - updateGlyphBadge('queued', job.id)
            // - pollJobStatus(job.id, handleStatusUpdate)
            // - Create result glyph on completion

        } catch (error) {
            log.error(SEG.UI, '[IX] Failed to create job:', error);
            // TODO: Phase 2 - Show error badge on glyph instead of console
            console.error('[IX Execution Error]', error);
        }
    });

    titleBar.appendChild(symbol);
    titleBar.appendChild(title);
    titleBar.appendChild(playBtn);

    // Content area
    const content = document.createElement('div');
    content.style.flex = '1';
    content.style.padding = '12px';
    content.style.display = 'flex';
    content.style.flexDirection = 'column';
    content.style.overflow = 'auto';

    // Assemble
    content.appendChild(textarea);

    element.appendChild(titleBar);
    element.appendChild(content);

    // Resize handle
    const resizeHandle = document.createElement('div');
    resizeHandle.className = 'ix-glyph-resize-handle';
    resizeHandle.style.position = 'absolute';
    resizeHandle.style.bottom = '0';
    resizeHandle.style.right = '0';
    resizeHandle.style.width = '16px';
    resizeHandle.style.height = '16px';
    resizeHandle.style.cursor = 'nwse-resize';
    resizeHandle.style.backgroundColor = 'var(--bg-tertiary)';
    resizeHandle.style.borderTopLeftRadius = '4px';
    element.appendChild(resizeHandle);

    // Make draggable via title bar
    makeDraggable(element, titleBar, glyph);

    // Make resizable via handle
    makeResizable(element, resizeHandle, glyph);

    return element;
}

/**
 * Make element draggable via handle
 *
 * Design decision: IX glyphs (and py glyphs) use free-form dragging without live grid snapping.
 * This provides smoother UX for larger content glyphs compared to grid-glyph.ts which snaps
 * during drag. Grid position is calculated only on mouseup for persistence.
 *
 * Rationale: Free-form placement is preferred over grid-snapped dragging for content glyphs.
 * Symbol-only glyphs (grid-glyph.ts) still use grid snapping for visual alignment.
 */
function makeDraggable(element: HTMLElement, handle: HTMLElement, glyph: Glyph): void {
    let isDragging = false;
    let startX = 0;
    let startY = 0;
    let initialLeft = 0;
    let initialTop = 0;

    handle.addEventListener('mousedown', (e) => {
        e.preventDefault();
        isDragging = true;

        startX = e.clientX;
        startY = e.clientY;
        initialLeft = element.offsetLeft;
        initialTop = element.offsetTop;

        element.style.opacity = '0.7';
    });

    document.addEventListener('mousemove', (e) => {
        if (!isDragging) return;

        const deltaX = e.clientX - startX;
        const deltaY = e.clientY - startY;

        element.style.left = `${initialLeft + deltaX}px`;
        element.style.top = `${initialTop + deltaY}px`;
    });

    document.addEventListener('mouseup', () => {
        if (!isDragging) return;
        isDragging = false;

        element.style.opacity = '1';

        // Calculate grid position and persist
        const canvas = element.parentElement;
        const canvasRect = canvas?.getBoundingClientRect() ?? { left: 0, top: 0 };
        const elementRect = element.getBoundingClientRect();
        const gridX = Math.round((elementRect.left - canvasRect.left) / GRID_SIZE);
        const gridY = Math.round((elementRect.top - canvasRect.top) / GRID_SIZE);

        glyph.gridX = gridX;
        glyph.gridY = gridY;

        // Persist to uiState
        if (glyph.symbol) {
            uiState.addCanvasGlyph({
                id: glyph.id,
                symbol: glyph.symbol,
                gridX,
                gridY,
                width: element.offsetWidth,
                height: element.offsetHeight
            });
        }

        log.debug(SEG.UI, `[IX Glyph] Moved to grid (${gridX}, ${gridY})`);
    });
}

/**
 * Make an element resizable by a handle
 */
function makeResizable(element: HTMLElement, handle: HTMLElement, glyph: Glyph): void {
    let isResizing = false;
    let startX = 0;
    let startY = 0;
    let startWidth = 0;
    let startHeight = 0;
    let abortController: AbortController | null = null;

    const handleMouseMove = (e: MouseEvent) => {
        if (!isResizing) return;

        const deltaX = e.clientX - startX;
        const deltaY = e.clientY - startY;

        const newWidth = Math.max(200, startWidth + deltaX);
        const newHeight = Math.max(120, startHeight + deltaY);

        element.style.width = `${newWidth}px`;
        element.style.height = `${newHeight}px`;
    };

    const handleMouseUp = () => {
        if (!isResizing) return;
        isResizing = false;

        element.classList.remove('is-resizing');

        // Save final size
        const rect = element.getBoundingClientRect();
        const finalWidth = Math.round(rect.width);
        const finalHeight = Math.round(rect.height);

        glyph.width = finalWidth;
        glyph.height = finalHeight;

        // Persist to uiState
        if (glyph.symbol && glyph.gridX !== undefined && glyph.gridY !== undefined) {
            uiState.addCanvasGlyph({
                id: glyph.id,
                symbol: glyph.symbol,
                gridX: glyph.gridX,
                gridY: glyph.gridY,
                width: finalWidth,
                height: finalHeight
            });
        }

        log.debug(SEG.UI, `[IX Glyph] Finished resizing to ${finalWidth}x${finalHeight}`);

        abortController?.abort();
        abortController = null;
    };

    handle.addEventListener('mousedown', (e) => {
        e.preventDefault();
        e.stopPropagation();
        isResizing = true;

        startX = e.clientX;
        startY = e.clientY;
        const rect = element.getBoundingClientRect();
        startWidth = rect.width;
        startHeight = rect.height;

        element.classList.add('is-resizing');

        abortController = new AbortController();
        document.addEventListener('mousemove', handleMouseMove, { signal: abortController.signal });
        document.addEventListener('mouseup', handleMouseUp, { signal: abortController.signal });

        log.debug(SEG.UI, `[IX Glyph] Started resizing`);
    });
}

