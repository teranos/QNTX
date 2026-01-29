/**
 * Prompt Glyph - LLM prompt template editor on canvas
 *
 * Renders a prompt template editor directly on canvas with:
 * - Template textarea with {{placeholder}} support
 * - Ax query input for selecting attestations
 * - Execute button that calls /api/prompt/preview
 * - Inline result display with token counts
 *
 * Execution Architecture:
 * - Prompt glyphs execute via /api/prompt/preview (X-sampling)
 * - Template supports YAML frontmatter for model/temperature/max_tokens config
 * - Ax query selects attestations to run the prompt against
 * - Results show interpolated prompts and LLM responses inline
 *
 * Design Parallel with Prose:
 * - Prose has a PromptPreviewPanel as a side panel for prompt files
 * - Canvas has prompt glyphs as spatial, self-contained prompt editors
 * - Both use the same /api/prompt/preview backend endpoint
 * - Canvas glyphs are more compact: template + ax query + run, all visible at once
 */

import type { Glyph } from './glyph';
import { SO } from '@generated/sym.js';
import { log, SEG } from '../../logger';
import { uiState } from '../../state/ui';
import { GRID_SIZE } from './grid-constants';
import { getScriptStorage } from '../../storage/script-storage';
import { apiFetch } from '../../api';

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
        log.error(SEG.UI, `[Prompt] Failed to parse stored status for ${glyphId}:`, e);
        return null;
    }
}

/**
 * Save ax query for a prompt glyph
 */
function saveAxQuery(glyphId: string, query: string): void {
    localStorage.setItem(`prompt-ax-${glyphId}`, query);
}

/**
 * Load ax query for a prompt glyph
 */
function loadAxQuery(glyphId: string): string {
    return localStorage.getItem(`prompt-ax-${glyphId}`) ?? '';
}

/**
 * Create a prompt glyph with template editor on canvas
 */
export async function createPromptGlyph(glyph: Glyph): Promise<HTMLElement> {
    // Load saved template from storage
    const storage = getScriptStorage();
    const defaultTemplate = '---\nmodel: "anthropic/claude-sonnet-4"\ntemperature: 0.7\nmax_tokens: 1000\n---\nSummarize this attestation:\n\nSubject: {{subject}}\nPredicate: {{predicate}}\nContext: {{context}}\n';
    const savedTemplate = await storage.load(glyph.id) ?? defaultTemplate;

    // Load saved ax query and status
    const savedAxQuery = loadAxQuery(glyph.id);
    const savedStatus = loadPromptStatus(glyph.id) ?? { state: 'idle' };

    const element = document.createElement('div');
    element.className = 'canvas-prompt-glyph';
    element.dataset.glyphId = glyph.id;

    const gridX = glyph.gridX ?? 5;
    const gridY = glyph.gridY ?? 5;

    const width = glyph.width ?? 420;
    const height = glyph.height ?? 340;

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

    // Template textarea (declared early for play button reference)
    const textarea = document.createElement('textarea');
    textarea.placeholder = '---\nmodel: "anthropic/claude-sonnet-4"\n---\nYour prompt template with {{subject}}, {{predicate}}, {{context}}...';
    textarea.value = savedTemplate;
    textarea.style.flex = '1';
    textarea.style.padding = '8px';
    textarea.style.fontSize = '13px';
    textarea.style.fontFamily = 'monospace';
    textarea.style.backgroundColor = '#1a1b1a';
    textarea.style.color = '#d4b8ff'; // Purple tint for prompt templates
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
            log.debug(SEG.UI, `[Prompt Glyph] Auto-saved template for ${glyph.id}`);
        }, 500);
    });

    textarea.addEventListener('mousedown', (e) => {
        e.stopPropagation();
    });

    // Ax query input
    const axInput = document.createElement('input');
    axInput.type = 'text';
    axInput.placeholder = 'ax query (e.g., find all, subject=user)';
    axInput.value = savedAxQuery;
    axInput.style.padding = '6px 8px';
    axInput.style.fontSize = '12px';
    axInput.style.fontFamily = 'monospace';
    axInput.style.backgroundColor = '#1a1b1a';
    axInput.style.color = '#a8d8ea'; // Blue tint for queries
    axInput.style.border = '1px solid var(--border-color)';
    axInput.style.borderRadius = '4px';
    axInput.style.marginTop = '4px';

    // Auto-save ax query
    let axSaveTimeout: number | undefined;
    axInput.addEventListener('input', () => {
        if (axSaveTimeout !== undefined) {
            clearTimeout(axSaveTimeout);
        }
        axSaveTimeout = window.setTimeout(() => {
            saveAxQuery(glyph.id, axInput.value);
            log.debug(SEG.UI, `[Prompt Glyph] Auto-saved ax query for ${glyph.id}`);
        }, 500);
    });

    axInput.addEventListener('mousedown', (e) => {
        e.stopPropagation();
    });

    // Results section
    const resultsSection = document.createElement('div');
    resultsSection.className = 'prompt-results-section';
    resultsSection.style.display = 'none';
    resultsSection.style.marginTop = '4px';
    resultsSection.style.padding = '8px';
    resultsSection.style.borderRadius = '4px';
    resultsSection.style.fontSize = '12px';
    resultsSection.style.fontFamily = 'monospace';
    resultsSection.style.maxHeight = '200px';
    resultsSection.style.overflowY = 'auto';

    // Status display
    const statusSection = document.createElement('div');
    statusSection.className = 'prompt-status-section';
    statusSection.style.display = 'none';
    statusSection.style.padding = '4px 8px';
    statusSection.style.fontSize = '11px';
    statusSection.style.fontFamily = 'monospace';

    function updateStatus(status: PromptGlyphStatus): void {
        switch (status.state) {
            case 'running':
                element.style.backgroundColor = '#1f2a3d';
                break;
            case 'success':
                element.style.backgroundColor = '#1f3d1f';
                break;
            case 'error':
                element.style.backgroundColor = '#3d1f1f';
                break;
            default:
                element.style.backgroundColor = 'var(--bg-secondary)';
        }

        if (status.state !== 'idle' && status.message) {
            statusSection.style.display = 'block';
            statusSection.textContent = status.message;

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

        savePromptStatus(glyph.id, status);
        log.debug(SEG.UI, `[Prompt Glyph] Updated status for ${glyph.id}:`, status);
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
    symbol.textContent = SO;
    symbol.style.fontSize = '16px';
    symbol.style.color = '#d4b8ff'; // Purple for prompt/SO
    symbol.style.fontWeight = 'bold';

    const title = document.createElement('span');
    title.textContent = 'Prompt';
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
    playBtn.title = 'Execute prompt preview';

    playBtn.addEventListener('click', async (e) => {
        e.stopPropagation();
        const template = textarea.value.trim();
        const axQuery = axInput.value.trim() || 'find all';

        if (!template) {
            log.debug(SEG.UI, '[Prompt] No template provided');
            return;
        }

        log.debug(SEG.UI, `[Prompt] Executing preview with ax query: ${axQuery}`);

        updateStatus({
            state: 'running',
            message: 'Running preview...',
            timestamp: Date.now(),
        });

        // Clear previous results
        resultsSection.style.display = 'none';
        resultsSection.innerHTML = '';

        try {
            const response = await apiFetch('/api/prompt/preview', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({
                    ax_query: axQuery,
                    template: template,
                    sample_size: 3,
                }),
            });

            if (!response.ok) {
                const errorText = await response.text();
                throw new Error(`API error: ${response.status} - ${errorText}`);
            }

            const data = await response.json() as any;

            // Render results
            if (data.samples && data.samples.length > 0) {
                resultsSection.style.display = 'block';
                resultsSection.innerHTML = '';

                let totalTokens = 0;
                for (let i = 0; i < data.samples.length; i++) {
                    const sample = data.samples[i];
                    totalTokens += sample.total_tokens || 0;

                    const sampleEl = document.createElement('div');
                    sampleEl.style.marginBottom = '8px';
                    sampleEl.style.padding = '6px';
                    sampleEl.style.backgroundColor = '#1a1b1a';
                    sampleEl.style.borderRadius = '3px';
                    sampleEl.style.borderLeft = sample.error
                        ? '3px solid #ff6b6b'
                        : '3px solid #a8e6a1';

                    const header = document.createElement('div');
                    header.style.color = '#888';
                    header.style.fontSize = '10px';
                    header.style.marginBottom = '4px';
                    header.textContent = `#${i + 1}${sample.total_tokens ? ` (${sample.total_tokens} tokens)` : ''}`;

                    const content = document.createElement('div');
                    content.style.color = sample.error ? '#ff6b6b' : '#e0e0e0';
                    content.style.whiteSpace = 'pre-wrap';
                    content.style.wordBreak = 'break-word';
                    content.textContent = sample.error || sample.response || 'No response';

                    sampleEl.appendChild(header);
                    sampleEl.appendChild(content);
                    resultsSection.appendChild(sampleEl);
                }

                updateStatus({
                    state: data.failure_count > 0 && data.success_count === 0 ? 'error' : 'success',
                    message: `${data.success_count}/${data.samples.length} samples, ${totalTokens} tokens, ${data.total_attestations} attestations matched`,
                    timestamp: Date.now(),
                });
            } else {
                updateStatus({
                    state: 'success',
                    message: `No attestations matched ax query: ${axQuery}`,
                    timestamp: Date.now(),
                });
            }

        } catch (error) {
            log.error(SEG.UI, '[Prompt] Preview failed:', error);
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
    content.appendChild(axInput);
    content.appendChild(resultsSection);
    content.appendChild(statusSection);

    element.appendChild(titleBar);
    element.appendChild(content);

    // Resize handle
    const resizeHandle = document.createElement('div');
    resizeHandle.className = 'prompt-glyph-resize-handle';
    resizeHandle.style.position = 'absolute';
    resizeHandle.style.bottom = '0';
    resizeHandle.style.right = '0';
    resizeHandle.style.width = '16px';
    resizeHandle.style.height = '16px';
    resizeHandle.style.cursor = 'nwse-resize';
    resizeHandle.style.backgroundColor = 'var(--bg-tertiary)';
    resizeHandle.style.borderTopLeftRadius = '4px';
    element.appendChild(resizeHandle);

    // Save initial template if new glyph
    if (!(await storage.load(glyph.id))) {
        await storage.save(glyph.id, defaultTemplate);
    }

    // Make draggable and resizable
    makeDraggable(element, titleBar, glyph);
    makeResizable(element, resizeHandle, glyph);

    return element;
}

/**
 * Make element draggable via handle
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

        const canvas = element.parentElement;
        const canvasRect = canvas?.getBoundingClientRect() ?? { left: 0, top: 0 };
        const elementRect = element.getBoundingClientRect();
        const gridX = Math.round((elementRect.left - canvasRect.left) / GRID_SIZE);
        const gridY = Math.round((elementRect.top - canvasRect.top) / GRID_SIZE);

        glyph.gridX = gridX;
        glyph.gridY = gridY;

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

        log.debug(SEG.UI, `[Prompt Glyph] Moved to grid (${gridX}, ${gridY})`);
    });
}

/**
 * Make element resizable via handle
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

        const newWidth = Math.max(280, startWidth + deltaX);
        const newHeight = Math.max(200, startHeight + deltaY);

        element.style.width = `${newWidth}px`;
        element.style.height = `${newHeight}px`;
    };

    const handleMouseUp = () => {
        if (!isResizing) return;
        isResizing = false;

        element.classList.remove('is-resizing');

        const rect = element.getBoundingClientRect();
        const finalWidth = Math.round(rect.width);
        const finalHeight = Math.round(rect.height);

        glyph.width = finalWidth;
        glyph.height = finalHeight;

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

        log.debug(SEG.UI, `[Prompt Glyph] Finished resizing to ${finalWidth}x${finalHeight}`);

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

        log.debug(SEG.UI, `[Prompt Glyph] Started resizing`);
    });
}
