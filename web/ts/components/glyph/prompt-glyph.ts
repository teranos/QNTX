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
import { applyCanvasGlyphLayout, makeDraggable, makeResizable, preventDrag } from './glyph-interaction';
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
    // Load saved template from storage
    const storage = getScriptStorage();
    const defaultTemplate = '---\nmodel: "anthropic/claude-haiku-4.5"\ntemperature: 0.7\nmax_tokens: 1000\n---\nWrite a haiku about quantum computing.\n';
    const savedTemplate = await storage.load(glyph.id) ?? defaultTemplate;

    // Load saved status
    const savedStatus = loadPromptStatus(glyph.id) ?? { state: 'idle' };

    const element = document.createElement('div');
    element.className = 'canvas-prompt-glyph';
    element.dataset.glyphId = glyph.id;
    element.dataset.glyphSymbol = SO;

    const x = glyph.x ?? 200;
    const y = glyph.y ?? 200;

    const width = glyph.width ?? 420;
    const height = glyph.height ?? 340;

    element.className += ' canvas-glyph';
    applyCanvasGlyphLayout(element, { x, y, width, height });

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

    // Results section
    const resultsSection = document.createElement('div');
    resultsSection.className = 'prompt-results-section';
    resultsSection.style.display = 'none';
    resultsSection.style.marginTop = '4px';
    resultsSection.style.padding = '8px';
    resultsSection.style.borderRadius = '4px';
    resultsSection.style.fontSize = '12px';
    resultsSection.style.fontFamily = 'monospace';
    resultsSection.style.flex = '1';
    resultsSection.style.overflow = 'auto';

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

    // Title bar
    const titleBar = document.createElement('div');
    titleBar.className = 'canvas-glyph-title-bar';

    const symbol = document.createElement('span');
    symbol.textContent = SO;
    symbol.style.fontSize = '16px';
    symbol.style.color = 'var(--accent-lavender)';
    symbol.style.fontWeight = 'bold';

    const title = document.createElement('span');
    title.textContent = 'Prompt';
    title.style.fontSize = '13px';
    title.style.flex = '1';
    title.style.color = 'var(--text-on-dark-emphasis)';
    title.style.fontWeight = 'bold';

    // Play button
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

        // Clear previous results
        resultsSection.style.display = 'none';
        resultsSection.innerHTML = '';

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
            const endTime = Date.now();
            const elapsedMs = endTime - startTime;
            const elapsedSeconds = (elapsedMs / 1000).toFixed(2);

            // Render result
            resultsSection.style.display = 'block';
            resultsSection.innerHTML = '';

            const resultEl = document.createElement('div');
            resultEl.style.padding = '6px';
            resultEl.style.backgroundColor = 'var(--bg-almost-black)';
            resultEl.style.borderRadius = '3px';
            resultEl.style.borderLeft = data.error
                ? '3px solid var(--glyph-status-error-text)'
                : '3px solid var(--glyph-status-success-text)';

            const header = document.createElement('div');
            header.style.color = '#888';
            header.style.fontSize = '10px';
            header.style.marginBottom = '4px';
            header.textContent = data.total_tokens ? `${data.total_tokens} tokens` : '';

            const content = document.createElement('div');
            content.style.color = data.error ? 'var(--glyph-status-error-text)' : 'var(--text-on-dark)';
            content.style.whiteSpace = 'pre-wrap';
            content.style.wordBreak = 'break-word';
            content.textContent = data.error || data.response || 'No response';

            resultEl.appendChild(header);
            resultEl.appendChild(content);
            resultsSection.appendChild(resultEl);

            updateStatus({
                state: data.error ? 'error' : 'success',
                message: data.error ? 'Failed' : `${elapsedSeconds}s`,
                timestamp: endTime,
            });

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

    titleBar.appendChild(symbol);
    titleBar.appendChild(title);
    titleBar.appendChild(playBtn);

    // Content area
    const content = document.createElement('div');
    content.style.flex = '1';
    content.style.padding = '8px';
    content.style.display = 'flex';
    content.style.flexDirection = 'column';
    content.style.overflow = 'hidden';

    content.appendChild(textarea);
    content.appendChild(resultsSection);
    content.appendChild(statusSection);

    element.appendChild(titleBar);
    element.appendChild(content);

    // Resize handle
    const resizeHandle = document.createElement('div');
    resizeHandle.className = 'prompt-glyph-resize-handle glyph-resize-handle';
    element.appendChild(resizeHandle);

    // Save initial template if new glyph
    if (!(await storage.load(glyph.id))) {
        await storage.save(glyph.id, defaultTemplate);
    }

    // Make draggable and resizable
    makeDraggable(element, titleBar, glyph, { logLabel: 'PromptGlyph' });
    makeResizable(element, resizeHandle, glyph, {
        logLabel: 'PromptGlyph',
        minWidth: 280,
        minHeight: 200
    });

    // Attach tooltip support
    tooltip.attach(element);

    return element;
}
