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
import { preventDrag, storeCleanup } from './glyph-interaction';
import { canvasPlaced } from './manifestations/canvas-placed';
import { forceTriggerJob } from '../../pulse/api';
import { getScriptStorage } from '../../storage/script-storage';
import { PULSE_EVENTS } from '../../pulse/events';
import type { ExecutionStartedDetail, ExecutionCompletedDetail, ExecutionFailedDetail } from '../../pulse/events';

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

    // Symbol (draggable area) — created before canvasPlaced to use as drag handle
    const symbol = document.createElement('span');
    symbol.textContent = IX;
    symbol.style.cursor = 'move';
    symbol.style.fontWeight = 'bold';
    symbol.style.flexShrink = '0';
    symbol.style.color = 'var(--accent-lavender)';

    const { element } = canvasPlaced({
        glyph,
        className: 'canvas-ix-glyph',
        defaults: { x: 200, y: 200, width: 360, height: 120 },
        dragHandle: symbol,
        logLabel: 'IX Glyph',
    });
    element.style.height = 'auto';
    element.style.overflow = 'visible';

    // Input field (declared early so play button can reference it)
    const input = document.createElement('input');
    input.type = 'text';
    input.placeholder = 'Enter URL, file path, or data source...';
    input.value = savedInput; // Restore saved content
    input.style.flex = '1';
    input.style.padding = '4px 8px';
    input.style.fontSize = '13px';
    input.style.fontFamily = 'monospace';
    input.style.backgroundColor = 'rgba(25, 25, 30, 0.95)';
    input.style.color = 'var(--accent-lavender)';
    input.style.border = 'none';
    input.style.outline = 'none';
    input.style.borderRadius = '2px';

    // Create canvas once for text measurement (avoid creating on every keystroke)
    const measureCanvas = document.createElement('canvas');
    const measureContext = measureCanvas.getContext('2d');

    // Helper to resize glyph based on input text width
    const resizeToFitText = () => {
        if (!measureContext) return;

        measureContext.font = '13px monospace'; // Match input font
        const textWidth = measureContext.measureText(input.value || input.placeholder).width;

        // Calculate glyph width: text width + padding + symbol + button
        const symbolWidth = 40; // Symbol space
        const buttonWidth = 40; // Play button space
        const padding = 40; // Input padding and gaps
        const minWidth = 200;
        const maxWidth = 800;

        const newWidth = Math.max(minWidth, Math.min(maxWidth, textWidth + symbolWidth + buttonWidth + padding));
        element.style.width = `${newWidth}px`;
        glyph.width = newWidth;
    };

    // Auto-save input with debouncing and resize
    let saveTimeout: number | undefined;
    input.addEventListener('input', () => {
        // Resize immediately for responsive feel
        resizeToFitText();

        if (saveTimeout !== undefined) {
            clearTimeout(saveTimeout);
        }
        saveTimeout = window.setTimeout(async () => {
            const currentInput = input.value;
            await storage.save(glyph.id, currentInput);
            log.debug(SEG.GLYPH, `[IX Glyph] Auto-saved input for ${glyph.id}`);
        }, 500);
    });

    // Initial resize based on saved content
    resizeToFitText();

    preventDrag(input);

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

    // Title bar (custom layout: symbol + input + play button)
    const titleBar = document.createElement('div');
    titleBar.className = 'canvas-glyph-title-bar';

    // Play button
    const playBtn = document.createElement('button');
    playBtn.textContent = '▶';
    playBtn.className = 'glyph-play-btn';
    playBtn.title = 'Execute';
    playBtn.style.flexShrink = '0';

    preventDrag(playBtn);

    playBtn.addEventListener('click', async (e) => {
        e.stopPropagation();
        const inputValue = input.value.trim();
        if (!inputValue) {
            log.debug(SEG.GLYPH, '[IX] No input provided');
            return;
        }

        log.debug(SEG.GLYPH, `[IX] Executing: ${inputValue}`);

        // Set to running state immediately
        updateStatus({
            state: 'running',
            message: 'Creating job...',
            timestamp: Date.now(),
        });

        try {
            // Wrap input as ATS command and trigger one-time Pulse job
            const atsCode = `ix ${inputValue}`;
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
    titleBar.appendChild(input);
    titleBar.appendChild(playBtn);

    // Assemble
    element.appendChild(titleBar);
    element.appendChild(statusSection);

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

    // Register cleanup for event listeners
    storeCleanup(element, () => {
        document.removeEventListener(PULSE_EVENTS.EXECUTION_STARTED, handleExecutionStarted);
        document.removeEventListener(PULSE_EVENTS.EXECUTION_COMPLETED, handleExecutionCompleted);
        document.removeEventListener(PULSE_EVENTS.EXECUTION_FAILED, handleExecutionFailed);
    });

    return element;
}


