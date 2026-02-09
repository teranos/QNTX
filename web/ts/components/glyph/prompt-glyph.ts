/**
 * Prompt Glyph - LLM prompt template editor on canvas
 *
 * Simple prompt editor with:
 * - Template textarea with YAML frontmatter for model/temperature/max_tokens config
 * - Play button for one-shot execution (testing)
 * - Inline result display
 *
 * Future Vision:
 * - AX glyphs (separate) flow attestations to Prompt glyphs via watchers
 * - Watchers keep executing prompts as matching attestations arrive
 * - For now: simple one-shot execution for testing
 */

import type { Glyph } from './glyph';
import { SO } from '@generated/sym.js';
import { log, SEG } from '../../logger';
import { getScriptStorage } from '../../storage/script-storage';
import { apiFetch } from '../../api';
import { preventDrag, storeCleanup } from './glyph-interaction';
import { canvasPlaced } from './manifestations/canvas-placed';
import { createResultGlyph, type ExecutionResult } from './result-glyph';
import { autoMeldResultBelow } from './meld/meld-system';
import { uiState } from '../../state/ui';
import { tooltip } from '../tooltip';

/**
 * Prompt glyph execution status
 */
interface PromptGlyphStatus {
    state: 'idle' | 'running' | 'success' | 'error';
    message?: string;
    timestamp?: number;
}

/**
 * Save prompt glyph status to localStorage
 */
function savePromptStatus(glyphId: string, status: PromptGlyphStatus): void {
    const key = `prompt-status-${glyphId}`;
    localStorage.setItem(key, JSON.stringify(status));
}

/**
 * Load prompt glyph status from localStorage
 */
function loadPromptStatus(glyphId: string): PromptGlyphStatus | null {
    const key = `prompt-status-${glyphId}`;
    const stored = localStorage.getItem(key);
    if (!stored) return null;

    try {
        return JSON.parse(stored);
    } catch (e) {
        log.error(SEG.GLYPH, `[Prompt] Failed to parse stored status for ${glyphId}:`, e);
        return null;
    }
}


/**
 * Create a prompt glyph with template editor on canvas
 */
export async function createPromptGlyph(glyph: Glyph): Promise<HTMLElement> {
    const element = document.createElement('div');
    await setupPromptGlyph(element, glyph);
    return element;
}

/**
 * Populate an element as a prompt glyph.
 * Can be called on a fresh element (createPromptGlyph) or an existing one (conversion).
 * Caller must runCleanup() and clear children before calling on an existing element.
 */
export async function setupPromptGlyph(element: HTMLElement, glyph: Glyph): Promise<void> {
    // Load saved template from storage
    const storage = getScriptStorage();
    const defaultTemplate = '---\nmodel: "anthropic/claude-haiku-4.5"\ntemperature: 0.7\nmax_tokens: 1000\n---\nWrite a haiku about quantum computing.\n';
    const savedTemplate = await storage.load(glyph.id) ?? defaultTemplate;

    // Load saved status
    const savedStatus = loadPromptStatus(glyph.id) ?? { state: 'idle' };

    // Reset inline styles (important when repopulating after conversion)
    element.style.cssText = '';

    // Template textarea (declared early for play button reference)
    const textarea = document.createElement('textarea');
    textarea.placeholder = '---\nmodel: "anthropic/claude-haiku-4.5"\n---\nYour prompt here...';
    textarea.value = savedTemplate;
    textarea.style.flex = '1';
    textarea.style.padding = '8px';
    textarea.style.fontSize = '13px';
    textarea.style.fontFamily = 'monospace';
    textarea.style.backgroundColor = 'var(--bg-almost-black)';
    textarea.style.color = 'var(--accent-lavender)';
    textarea.style.border = '1px solid var(--border-color)';
    textarea.style.borderRadius = '4px';
    textarea.style.resize = 'none';

    // Auto-save template with debouncing
    let saveTimeout: number | undefined;
    textarea.addEventListener('input', () => {
        if (saveTimeout !== undefined) {
            clearTimeout(saveTimeout);
        }
        saveTimeout = window.setTimeout(async () => {
            await storage.save(glyph.id, textarea.value);
            log.debug(SEG.GLYPH, `[Prompt Glyph] Auto-saved template for ${glyph.id}`);
        }, 500);
    });

    preventDrag(textarea);

    // Status display
    const statusSection = document.createElement('div');
    statusSection.className = 'prompt-status-section';
    statusSection.style.display = 'none';
    statusSection.style.padding = '4px 8px';
    statusSection.style.fontSize = '11px';
    statusSection.style.fontFamily = 'monospace';
    statusSection.style.whiteSpace = 'pre-wrap'; // Allow wrapping, preserve formatting
    statusSection.style.wordBreak = 'break-word'; // Break long words if needed
    statusSection.style.overflowWrap = 'anywhere'; // Allow breaking anywhere to prevent overflow
    statusSection.style.maxWidth = '100%';

    function updateStatus(status: PromptGlyphStatus): void {
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

        savePromptStatus(glyph.id, status);
        log.debug(SEG.GLYPH, `[Prompt Glyph] Updated status for ${glyph.id}:`, status);
    }

    // Apply saved status on load
    updateStatus(savedStatus);

    // Title bar elements
    const playBtn = document.createElement('button');
    playBtn.textContent = '▶';
    playBtn.className = 'glyph-play-btn has-tooltip';
    playBtn.dataset.tooltip = 'Execute prompt';

    playBtn.addEventListener('click', async (e) => {
        e.stopPropagation();
        const template = textarea.value.trim();

        if (!template) {
            log.debug(SEG.GLYPH, '[Prompt] No template provided');
            return;
        }

        // Detect if template has {{variables}}
        const hasVariables = /\{\{[^}]+\}\}/.test(template);

        if (hasVariables) {
            // Template needs attestation data - show message
            updateStatus({
                state: 'error',
                message: 'Template has {{variables}} - connect to AX glyph (coming soon)',
                timestamp: Date.now(),
            });
            return;
        }

        log.debug(SEG.GLYPH, `[Prompt] Executing direct (no variables)`);

        const startTime = Date.now();

        updateStatus({
            state: 'running',
            message: 'Running...',
            timestamp: startTime,
        });

        try {
            const response = await apiFetch('/api/prompt/direct', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({
                    template: template,
                }),
            });

            if (!response.ok) {
                const errorText = await response.text();
                throw new Error(`API error: ${response.status} - ${errorText}`);
            }

            const data = await response.json() as any;
            const elapsedMs = Date.now() - startTime;
            const elapsedSeconds = (elapsedMs / 1000).toFixed(2);

            updateStatus({
                state: data.error ? 'error' : 'success',
                message: data.error ? 'Failed' : `${elapsedSeconds}s`,
                timestamp: Date.now(),
            });

            // Spawn result glyph below the prompt
            const result: ExecutionResult = {
                success: !data.error,
                stdout: data.response ?? '',
                stderr: '',
                result: null,
                error: data.error ?? null,
                duration_ms: elapsedMs,
            };
            spawnResultGlyph(element, result);

        } catch (error) {
            log.error(SEG.GLYPH, '[Prompt] Execution failed:', error);
            const errorMsg = error instanceof Error ? error.message : String(error);
            updateStatus({
                state: 'error',
                message: `Failed: ${errorMsg}`,
                timestamp: Date.now(),
            });
        }
    });

    canvasPlaced({
        element,
        glyph,
        className: 'canvas-prompt-glyph',
        defaults: { x: 200, y: 200, width: 420, height: 340 },
        titleBar: { label: `${SO} Prompt`, actions: [playBtn] },
        resizable: { minWidth: 280, minHeight: 200 },
        logLabel: 'PromptGlyph',
    });

    // Style the label span created by canvasPlaced
    const labelSpan = element.querySelector('.canvas-glyph-title-bar > span:first-child') as HTMLElement;
    if (labelSpan) {
        labelSpan.style.fontSize = '16px';
        labelSpan.style.color = 'var(--accent-lavender)';
        labelSpan.style.fontWeight = 'bold';
        labelSpan.style.flex = '1';
    }

    // Content area
    const content = document.createElement('div');
    content.style.flex = '1';
    content.style.padding = '8px';
    content.style.display = 'flex';
    content.style.flexDirection = 'column';
    content.style.overflow = 'hidden';

    content.appendChild(textarea);
    content.appendChild(statusSection);

    element.appendChild(content);

    // Save initial template if new glyph
    if (!(await storage.load(glyph.id))) {
        await storage.save(glyph.id, defaultTemplate);
    }

    // Register cleanup for conversions (drag/resize handled by canvasPlaced)
    storeCleanup(element, () => {
        if (saveTimeout !== undefined) clearTimeout(saveTimeout);
    });

    // Attach tooltip support
    tooltip.attach(element);

    /**
     * Spawn a result glyph directly below this prompt glyph.
     * Composition-aware: extends existing composition or creates new meld.
     */
    function spawnResultGlyph(promptEl: HTMLElement, result: ExecutionResult): void {
        const promptRect = promptEl.getBoundingClientRect();
        const canvas = promptEl.closest('.canvas-workspace') as HTMLElement;
        if (!canvas) {
            log.error(SEG.GLYPH, '[Prompt] Cannot spawn result glyph: no canvas-workspace ancestor');
            return;
        }
        const canvasRect = canvas.getBoundingClientRect();

        const rx = promptRect.left - canvasRect.left;
        const ry = promptRect.bottom - canvasRect.top;

        const resultGlyphId = `result-${crypto.randomUUID()}`;
        const resultGlyph: Glyph = {
            id: resultGlyphId,
            title: 'Prompt Result',
            symbol: 'result',
            x: rx,
            y: ry,
            width: Math.round(promptRect.width),
            renderContent: () => document.createElement('div')
        };

        const resultElement = createResultGlyph(resultGlyph, result);
        canvas.appendChild(resultElement);

        const resultRect = resultElement.getBoundingClientRect();
        uiState.addCanvasGlyph({
            id: resultGlyphId,
            symbol: 'result',
            x: rx,
            y: ry,
            width: Math.round(resultRect.width),
            height: Math.round(resultRect.height),
            result,
        });

        // Auto-meld result below prompt glyph (bottom port)
        const promptGlyphId = promptEl.dataset.glyphId;
        if (promptGlyphId) {
            autoMeldResultBelow(promptEl, promptGlyphId, 'prompt', 'Prompt', resultElement, resultGlyphId, 'Prompt');
        }
    }
}
