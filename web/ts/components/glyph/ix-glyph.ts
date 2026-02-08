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
import { applyCanvasGlyphLayout, makeDraggable, makeResizable, preventDrag } from './glyph-interaction';
import { forceTriggerJob } from '../../pulse/api';
import { getScriptStorage } from '../../storage/script-storage';
import { PULSE_EVENTS } from '../../pulse/events';
import type { ExecutionStartedDetail, ExecutionCompletedDetail, ExecutionFailedDetail } from '../../pulse/events';
import { MAX_VIEWPORT_HEIGHT_RATIO } from './glyph';

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
        log.error(SEG.GLYPH, `[IX] Failed to parse stored status for ${glyphId}:`, e);
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
    element.className = 'canvas-ix-glyph canvas-glyph';
    element.dataset.glyphId = glyph.id;
    element.dataset.glyphSymbol = IX;

    const x = glyph.x ?? 200;
    const y = glyph.y ?? 200;

    // Default size for IX glyph
    const width = glyph.width ?? 360;
    const height = glyph.height ?? 180;

    applyCanvasGlyphLayout(element, { x, y, width, height, useMinHeight: true });
    element.style.overflow = 'visible';

    // Textarea (declared early so play button can reference it)
    const textarea = document.createElement('textarea');
    textarea.placeholder = 'Enter URL, file path, or data source...';
    textarea.value = savedInput; // Restore saved content
    textarea.style.flex = '1';
    textarea.style.padding = '8px';
    textarea.style.fontSize = '13px';
    textarea.style.fontFamily = 'monospace';
    textarea.style.backgroundColor = 'var(--bg-almost-black)';
    textarea.style.color = 'var(--glyph-prompt-accent)';
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
            log.debug(SEG.GLYPH, `[IX Glyph] Auto-saved input for ${glyph.id}`);
        }, 500);
    });

    preventDrag(textarea);

    // Status display section (declared early so helpers can reference it)
    const statusSection = document.createElement('div');
    statusSection.className = 'ix-status-section';
    statusSection.style.display = 'none'; // Hidden by default
    statusSection.style.marginTop = '8px';
    statusSection.style.padding = '8px';
    statusSection.style.borderRadius = '4px';
    statusSection.style.fontSize = '12px';
    statusSection.style.fontFamily = 'monospace';
    statusSection.style.whiteSpace = 'pre-wrap';
    statusSection.style.wordBreak = 'break-word';
    statusSection.style.overflowWrap = 'break-word';

    // Track current scheduledJobId for event filtering
    let currentScheduledJobId: string | undefined = savedStatus.scheduledJobId;

    // Helper function to update glyph visual state
    function updateStatus(status: IxGlyphStatus): void {
        // Update background color
        switch (status.state) {
            case 'running':
                element.style.backgroundColor = 'var(--glyph-status-running-bg)';
                break;
            case 'success':
                element.style.backgroundColor = 'var(--glyph-status-success-bg)';
                break;
            case 'error':
                element.style.backgroundColor = 'var(--glyph-status-error-bg)';
                break;
            default:
                element.style.backgroundColor = 'var(--bg-secondary)';
        }

        // Update status section
        if (status.state !== 'idle' && status.message) {
            statusSection.style.display = 'block';
            statusSection.textContent = status.message;

            switch (status.state) {
                case 'running':
                    statusSection.style.color = 'var(--glyph-status-running-text)';
                    statusSection.style.backgroundColor = 'var(--glyph-status-running-section-bg)';
                    break;
                case 'success':
                    statusSection.style.color = 'var(--glyph-status-success-text)';
                    statusSection.style.backgroundColor = 'var(--glyph-status-success-section-bg)';
                    break;
                case 'error':
                    statusSection.style.color = 'var(--glyph-status-error-text)';
                    statusSection.style.backgroundColor = 'var(--glyph-status-error-section-bg)';
                    break;
            }
        } else {
            statusSection.style.display = 'none';
        }

        // Save to localStorage
        saveIxStatus(glyph.id, status);

        log.debug(SEG.GLYPH, `[IX Glyph] Updated status for ${glyph.id}:`, status);
    }

    // Apply saved status on load
    updateStatus(savedStatus);

    // Title bar (matches ax-glyph pattern: symbol is drag handle, bar is compact)
    const titleBar = document.createElement('div');
    titleBar.className = 'ix-glyph-title-bar';
    titleBar.style.padding = '4px 4px 4px 8px';
    titleBar.style.backgroundColor = 'var(--bg-tertiary)';
    titleBar.style.userSelect = 'none';
    titleBar.style.fontSize = '14px';
    titleBar.style.display = 'flex';
    titleBar.style.alignItems = 'center';
    titleBar.style.gap = '8px';

    // Symbol (draggable area) — purple-toned to reflect pulse/ingestion lineage
    const symbol = document.createElement('span');
    symbol.textContent = IX;
    symbol.style.cursor = 'move';
    symbol.style.fontWeight = 'bold';
    symbol.style.flexShrink = '0';
    symbol.style.color = 'var(--glyph-prompt-accent)';

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
            log.debug(SEG.GLYPH, '[IX] No input provided');
            return;
        }

        log.debug(SEG.GLYPH, `[IX] Executing: ${input}`);

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

            log.debug(SEG.GLYPH, `[IX] Job created successfully`, {
                jobId: job.id,
                atsCode: atsCode
            });

        } catch (error) {
            log.error(SEG.GLYPH, '[IX] Failed to create job:', error);
            const errorMsg = error instanceof Error ? error.message : String(error);

            // Show error state
            updateStatus({
                state: 'error',
                message: `Failed to create job: ${errorMsg}`,
                timestamp: Date.now(),
            });
        }
    });

    titleBar.appendChild(symbol);
    titleBar.appendChild(playBtn);

    // Content area
    const content = document.createElement('div');
    content.style.flex = '1';
    content.style.padding = '12px';
    content.style.display = 'flex';
    content.style.flexDirection = 'column';
    content.style.overflow = 'visible';

    // Assemble
    content.appendChild(textarea);
    content.appendChild(statusSection);

    element.appendChild(titleBar);
    element.appendChild(content);

    // Resize handle
    const resizeHandle = document.createElement('div');
    resizeHandle.className = 'ix-glyph-resize-handle glyph-resize-handle';
    element.appendChild(resizeHandle);

    // Wire up Pulse execution event listeners
    const handleExecutionStarted = (e: Event) => {
        const detail = (e as CustomEvent<ExecutionStartedDetail>).detail;
        log.debug(SEG.GLYPH, `[IX Glyph ${glyph.id}] Got execution started event:`, {
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
        log.debug(SEG.GLYPH, `[IX Glyph ${glyph.id}] Got execution failed event:`, {
            eventScheduledJobId: detail.scheduledJobId,
            currentScheduledJobId,
            matches: detail.scheduledJobId === currentScheduledJobId,
            error: detail.errorMessage
        });
        if (detail.scheduledJobId !== currentScheduledJobId) return;

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

    // Make draggable via symbol (matches ax-glyph pattern)
    makeDraggable(element, symbol, glyph, { logLabel: 'IX Glyph' });

    // Make resizable via handle
    makeResizable(element, resizeHandle, glyph, { logLabel: 'IX Glyph' });

    // Set up ResizeObserver for auto-sizing glyph to content
    setupCanvasGlyphResizeObserver(element, content, glyph.id, 'IX');

    return element;
}

/**
 * Set up ResizeObserver to auto-size canvas glyph to match content height
 * Works alongside manual resize handles - user can still drag to resize
 */
function setupCanvasGlyphResizeObserver(
    glyphElement: HTMLElement,
    contentElement: HTMLElement,
    glyphId: string,
    glyphType: string
): void {
    // Cleanup any existing observer to prevent memory leaks on re-render
    const existingObserver = (glyphElement as any).__resizeObserver;
    if (existingObserver && typeof existingObserver.disconnect === 'function') {
        existingObserver.disconnect();
        delete (glyphElement as any).__resizeObserver;
        log.debug(SEG.GLYPH, `[${glyphType} ${glyphId}] Disconnected existing ResizeObserver`);
    }

    const maxHeight = window.innerHeight * MAX_VIEWPORT_HEIGHT_RATIO;

    const resizeObserver = new ResizeObserver(entries => {
        for (const entry of entries) {
            const contentHeight = entry.contentRect.height;
            const totalHeight = Math.min(contentHeight, maxHeight);

            // Update minHeight instead of height to allow manual resize
            glyphElement.style.minHeight = `${totalHeight}px`;

            log.debug(SEG.GLYPH, `[${glyphType} ${glyphId}] Auto-resized to ${totalHeight}px (content: ${contentHeight}px)`);
        }
    });

    resizeObserver.observe(contentElement);

    // Store observer for cleanup
    (glyphElement as any).__resizeObserver = resizeObserver;
}


