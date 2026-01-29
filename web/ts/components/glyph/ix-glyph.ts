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
import { getScriptStorage } from '../../storage/script-storage';
import { PULSE_EVENTS } from '../../pulse/events';
import type { ExecutionStartedDetail, ExecutionCompletedDetail, ExecutionFailedDetail } from '../../pulse/events';
import { createPyGlyph } from './py-glyph';

/**
 * IX glyph execution status (persisted in localStorage)
 */
interface IxGlyphStatus {
    state: 'idle' | 'running' | 'success' | 'error';
    scheduledJobId?: string;
    executionId?: string;
    message?: string;
    timestamp?: number;
}

/**
 * Save IX glyph status to localStorage
 */
function saveIxStatus(glyphId: string, status: IxGlyphStatus): void {
    const key = `ix-status-${glyphId}`;
    localStorage.setItem(key, JSON.stringify(status));
}

/**
 * Load IX glyph status from localStorage
 */
function loadIxStatus(glyphId: string): IxGlyphStatus | null {
    const key = `ix-status-${glyphId}`;
    const stored = localStorage.getItem(key);
    if (!stored) return null;

    try {
        return JSON.parse(stored);
    } catch (e) {
        log.error(SEG.UI, `[IX] Failed to parse stored status for ${glyphId}:`, e);
        return null;
    }
}

/**
 * Create an IX glyph with input form on canvas
 */
export async function createIxGlyph(glyph: Glyph): Promise<HTMLElement> {
    // Load saved input from storage
    const storage = getScriptStorage();
    const savedInput = await storage.load(glyph.id) ?? '';

    // Load saved execution status
    const savedStatus = loadIxStatus(glyph.id) ?? { state: 'idle' };

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
    textarea.value = savedInput; // Restore saved content
    textarea.style.flex = '1';
    textarea.style.padding = '8px';
    textarea.style.fontSize = '13px';
    textarea.style.fontFamily = 'monospace';
    textarea.style.backgroundColor = '#1a1b1a';
    textarea.style.color = '#a8e6a1';
    textarea.style.border = '1px solid var(--border-color)';
    textarea.style.borderRadius = '4px';
    textarea.style.resize = 'none';

    // Auto-save input with debouncing
    let saveTimeout: number | undefined;
    textarea.addEventListener('input', () => {
        if (saveTimeout !== undefined) {
            clearTimeout(saveTimeout);
        }
        saveTimeout = window.setTimeout(async () => {
            const currentInput = textarea.value;
            await storage.save(glyph.id, currentInput);
            log.debug(SEG.UI, `[IX Glyph] Auto-saved input for ${glyph.id}`);
        }, 500);
    });

    // Prevent drag from starting on textarea
    textarea.addEventListener('mousedown', (e) => {
        e.stopPropagation();
    });

    // Status display section (declared early so helpers can reference it)
    const statusSection = document.createElement('div');
    statusSection.className = 'ix-status-section';
    statusSection.style.display = 'none'; // Hidden by default
    statusSection.style.marginTop = '8px';
    statusSection.style.padding = '8px';
    statusSection.style.borderRadius = '4px';
    statusSection.style.fontSize = '12px';
    statusSection.style.fontFamily = 'monospace';
    statusSection.style.whiteSpace = 'pre-wrap'; // Allow line wrapping for long errors
    statusSection.style.wordBreak = 'break-word'; // Break long words if needed
    statusSection.style.overflowY = 'auto'; // Scroll if too long
    statusSection.style.maxHeight = '150px'; // Limit height

    // Track current scheduledJobId for event filtering
    let currentScheduledJobId: string | undefined = savedStatus.scheduledJobId;

    // Helper function to update glyph visual state
    function updateStatus(status: IxGlyphStatus): void {
        // Update background color
        switch (status.state) {
            case 'running':
                element.style.backgroundColor = '#1f2a3d'; // Blue tint
                break;
            case 'success':
                element.style.backgroundColor = '#1f3d1f'; // Green tint
                break;
            case 'error':
                element.style.backgroundColor = '#3d1f1f'; // Red tint
                break;
            default:
                element.style.backgroundColor = 'var(--bg-secondary)'; // Default
        }

        // Update status section
        if (status.state !== 'idle' && status.message) {
            statusSection.style.display = 'block';
            statusSection.textContent = status.message;

            // Color the status section text
            switch (status.state) {
                case 'running':
                    statusSection.style.color = '#6b9bd1';
                    statusSection.style.backgroundColor = '#1a2332';
                    break;
                case 'success':
                    statusSection.style.color = '#a8e6a1';
                    statusSection.style.backgroundColor = '#1a2b1a';
                    break;
                case 'error':
                    statusSection.style.color = '#ff6b6b';
                    statusSection.style.backgroundColor = '#2b1a1a';
                    break;
            }
        } else {
            statusSection.style.display = 'none';
        }

        // Save to localStorage
        saveIxStatus(glyph.id, status);

        log.debug(SEG.UI, `[IX Glyph] Updated status for ${glyph.id}:`, status);
    }

    // Apply saved status on load
    updateStatus(savedStatus);

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
    symbol.style.color = '#ffffff';
    symbol.style.fontWeight = 'bold';

    const title = document.createElement('span');
    title.textContent = 'Ingest';
    title.style.fontSize = '13px';
    title.style.flex = '1';
    title.style.color = '#ffffff';
    title.style.fontWeight = 'bold';

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

        // Set to running state immediately
        updateStatus({
            state: 'running',
            message: 'Creating job...',
            timestamp: Date.now(),
        });

        try {
            // Wrap input as ATS command and trigger one-time Pulse job
            const atsCode = `ix ${input}`;
            const job = await forceTriggerJob(atsCode);

            // Store job ID for event tracking
            currentScheduledJobId = job.id;

            // Update status with job queued
            updateStatus({
                state: 'running',
                scheduledJobId: job.id,
                message: `Job queued: ${job.id}`,
                timestamp: Date.now(),
            });

            log.debug(SEG.UI, `[IX] Job created successfully`, {
                jobId: job.id,
                atsCode: atsCode
            });

        } catch (error) {
            log.error(SEG.UI, '[IX] Failed to create job:', error);
            const errorMsg = error instanceof Error ? error.message : String(error);

            // Debug: Log error details
            if (error instanceof Error) {
                const details = (error as Error & { details?: string[] }).details;
                log.debug(SEG.UI, '[IX] Error details:', {
                    message: errorMsg,
                    hasDetails: !!details,
                    details: details
                });
            }

            // Check if this is a "no ingest script" error
            const isNoScriptError = errorMsg.includes('no ingest script found');

            log.debug(SEG.UI, '[IX] Error check:', {
                isNoScriptError,
                errorMsg
            });

            if (isNoScriptError && error instanceof Error) {
                // Check if error has details attached (from API response)
                const details = (error as Error & { details?: string[] }).details;

                if (details) {
                    // Parse structured error details to extract script_type
                    for (const detailLine of details) {
                        const match = detailLine.match(/script_type=(\w+)/);
                        if (match) {
                            const scriptName = match[1];
                            log.debug(SEG.UI, '[IX] Found script type in details:', scriptName);
                            showCreateHandlerUI(scriptName);
                            return;
                        }
                    }
                } else {
                    log.warn(SEG.UI, '[IX] No details attached to error, falling back to message parsing');
                    // Fallback: try to extract from error message
                    const match = errorMsg.match(/script_type=(\w+)/);
                    if (match) {
                        const scriptName = match[1];
                        log.debug(SEG.UI, '[IX] Found script type in message:', scriptName);
                        showCreateHandlerUI(scriptName);
                        return;
                    }
                }
            }

            // Show standard error state
            updateStatus({
                state: 'error',
                message: `Failed to create job: ${errorMsg}`,
                timestamp: Date.now(),
            });
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
    content.appendChild(statusSection);

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

    /**
     * Show "Create handler" UI when script not found
     */
    function showCreateHandlerUI(scriptName: string): void {
        statusSection.style.display = 'block';
        statusSection.style.color = '#ff6b6b';
        statusSection.style.backgroundColor = '#2b1a1a';
        statusSection.innerHTML = ''; // Clear existing content

        // Error message
        const errorText = document.createElement('div');
        errorText.textContent = `✗ No handler for '${scriptName}'`;
        errorText.style.marginBottom = '8px';
        statusSection.appendChild(errorText);

        // Create handler button
        const createBtn = document.createElement('button');
        createBtn.textContent = 'Create handler';
        createBtn.style.padding = '4px 8px';
        createBtn.style.fontSize = '12px';
        createBtn.style.backgroundColor = 'var(--bg-hover)';
        createBtn.style.color = 'var(--text-primary)';
        createBtn.style.border = '1px solid var(--border-color)';
        createBtn.style.borderRadius = '4px';
        createBtn.style.cursor = 'pointer';

        createBtn.addEventListener('click', async () => {
            await createIngestHandler(scriptName);
        });

        statusSection.appendChild(createBtn);

        // Update background
        element.style.backgroundColor = '#3d1f1f'; // Red tint

        log.debug(SEG.UI, `[IX Glyph] Showing create handler UI for '${scriptName}'`);
    }

    /**
     * Create a new Python ingest handler
     */
    async function createIngestHandler(scriptName: string): Promise<void> {
        const template = generateHandlerTemplate(scriptName);

        // Calculate spawn position (offset from IX glyph)
        const ixGridX = glyph.gridX ?? 5;
        const ixGridY = glyph.gridY ?? 5;
        const pyGridX = ixGridX + 3; // Spawn 3 grid units to the right
        const pyGridY = ixGridY;

        const pyGlyph: Glyph = {
            id: `py-${crypto.randomUUID()}`,
            title: 'Python',
            symbol: 'py',
            gridX: pyGridX,
            gridY: pyGridY,
            handlerFor: scriptName,  // Mark this as a handler for the script type
            renderContent: () => {
                const content = document.createElement('div');
                content.textContent = 'Python handler';
                return content;
            }
        };

        // Save template to storage before rendering
        const storage = getScriptStorage();
        await storage.save(pyGlyph.id, template);

        // Get canvas element
        const canvas = element.parentElement;
        if (!canvas) {
            log.error(SEG.UI, '[IX Glyph] Cannot spawn Python glyph: canvas not found');
            return;
        }

        // Render Python editor glyph (will load template from storage)
        const glyphElement = await createPyGlyph(pyGlyph);
        canvas.appendChild(glyphElement);

        // Get actual rendered size and persist
        const rect = glyphElement.getBoundingClientRect();
        const width = Math.round(rect.width);
        const height = Math.round(rect.height);

        uiState.addCanvasGlyph({
            id: pyGlyph.id,
            symbol: 'py',
            gridX: pyGridX,
            gridY: pyGridY,
            width,
            height
        });

        log.debug(SEG.UI, `[IX Glyph] Created Python handler for '${scriptName}' at grid (${pyGridX}, ${pyGridY})`);

        // Update status to show handler created
        updateStatus({
            state: 'idle',
            message: `Handler editor created →`,
            timestamp: Date.now(),
        });
    }

    /**
     * Generate generic Python ingest handler template
     */
    function generateHandlerTemplate(scriptName: string): string {
        return `# IX handler: ${scriptName}
# Runs periodically when scheduled via: ix ${scriptName}

# TODO: Your ingestion logic here
# Fetch data from source, parse it, create attestations

# Example: Fetch data from webhook or API
# import requests
# response = requests.get("https://api.example.com/data")
# data = response.json()
#
# # Parse and create attestations
# attest(
#     subjects=["source:api.example.com"],
#     predicates=["ingested_via"],
#     contexts=["${scriptName}"],
#     attributes={"count": len(data)}
# )

print("Handler '${scriptName}' executed")
`;
    }

    // Wire up Pulse execution event listeners
    const handleExecutionStarted = (e: Event) => {
        const detail = (e as CustomEvent<ExecutionStartedDetail>).detail;
        log.debug(SEG.UI, `[IX Glyph ${glyph.id}] Got execution started event:`, {
            eventScheduledJobId: detail.scheduledJobId,
            currentScheduledJobId,
            matches: detail.scheduledJobId === currentScheduledJobId
        });
        if (detail.scheduledJobId !== currentScheduledJobId) return;

        updateStatus({
            state: 'running',
            scheduledJobId: detail.scheduledJobId,
            executionId: detail.executionId,
            message: `Running: ${detail.atsCode}`,
            timestamp: detail.timestamp,
        });
    };

    const handleExecutionCompleted = (e: Event) => {
        const detail = (e as CustomEvent<ExecutionCompletedDetail>).detail;
        if (detail.scheduledJobId !== currentScheduledJobId) return;

        const summary = detail.resultSummary || 'Completed successfully';
        updateStatus({
            state: 'success',
            scheduledJobId: detail.scheduledJobId,
            executionId: detail.executionId,
            message: `✓ ${summary} (${detail.durationMs}ms)`,
            timestamp: detail.timestamp,
        });
    };

    const handleExecutionFailed = (e: Event) => {
        const detail = (e as CustomEvent<ExecutionFailedDetail>).detail;
        log.debug(SEG.UI, `[IX Glyph ${glyph.id}] Got execution failed event:`, {
            eventScheduledJobId: detail.scheduledJobId,
            currentScheduledJobId,
            matches: detail.scheduledJobId === currentScheduledJobId,
            error: detail.errorMessage,
            errorDetails: detail.errorDetails
        });
        if (detail.scheduledJobId !== currentScheduledJobId) return;

        // Check if error is "no ingest script found" sentinel error
        const isNoScriptError = detail.errorMessage.includes('no ingest script found');

        if (isNoScriptError && detail.errorDetails) {
            // Parse structured error details to extract script_type
            // Format: "script_type=csv"
            for (const detailLine of detail.errorDetails) {
                const match = detailLine.match(/script_type=(\w+)/);
                if (match) {
                    const scriptName = match[1];
                    showCreateHandlerUI(scriptName);
                    return;
                }
            }
        }

        // Standard error display
        updateStatus({
            state: 'error',
            scheduledJobId: detail.scheduledJobId,
            executionId: detail.executionId,
            message: `✗ ${detail.errorMessage}`,
            timestamp: detail.timestamp,
        });
    };

    // Add event listeners
    document.addEventListener(PULSE_EVENTS.EXECUTION_STARTED, handleExecutionStarted);
    document.addEventListener(PULSE_EVENTS.EXECUTION_COMPLETED, handleExecutionCompleted);
    document.addEventListener(PULSE_EVENTS.EXECUTION_FAILED, handleExecutionFailed);

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

