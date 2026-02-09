/**
 * Python Glyph - CodeMirror-based Python editor on canvas
 *
 * These are resizable code preview glyphs that live on the canvas workspace.
 * They show a small amount of actual code and are spatially positioned.
 *
 * Future vision:
 * - Clicking a py glyph will spawn a full 'programmature' manifestation
 * - That manifestation can minimize to tray like windows
 * - The canvas py glyph remains as a spatial reference/preview
 *
 * TODO: Integration points
 * 1. Click handler to spawn programmature manifestation (manifestations/programmature.ts)
 * 2. Code persistence - save/load glyph content to filesystem or database
 * 3. Run button integration with /api/python/execute endpoint
 * 4. Multi-language support - extract common logic for go, rs, ts variants
 * 5. Auto-resize based on actual editor content (listen to CodeMirror changes)
 * 6. Syntax error indicators in title bar
 * 7. File path association (show file path in title bar)
 */

import type { Glyph } from './glyph';
import { log, SEG } from '../../logger';
import { uiState } from '../../state/ui';
import { getScriptStorage } from '../../storage/script-storage';
import { apiFetch } from '../../api';
import { createResultGlyph, type ExecutionResult } from './result-glyph';
import { autoMeldResultBelow } from './meld/meld-system';
import { syncStateManager } from '../../state/sync-state';
import { connectivityManager } from '../../connectivity';
import { canvasPlaced } from './manifestations/canvas-placed';

/**
 * Create a Python editor glyph with CodeMirror
 *
 * TODO: Accept code content as parameter instead of always using defaultCode
 * TODO: Store editor reference for later access (code execution, content updates)
 */
export async function createPyGlyph(glyph: Glyph): Promise<HTMLElement> {
    // Load code from storage or use default
    const storage = getScriptStorage();
    const defaultCode = '# Python editor\nprint("Hello from canvas!")\n';
    const savedCode = await storage.load(glyph.id);
    const code = savedCode ?? defaultCode;

    // Calculate initial height based on content (if no saved size)
    const lineCount = code.split('\n').length;
    const lineHeight = 24;
    const titleBarH = 36;
    const minHeight = 120;
    const maxHeight = 600;
    const calculatedHeight = Math.min(maxHeight, Math.max(minHeight, titleBarH + lineCount * lineHeight + 40));

    // Run button
    const runButton = document.createElement('button');
    runButton.textContent = '▶';
    runButton.className = 'glyph-play-btn';
    runButton.title = 'Run Python code';

    const { element } = canvasPlaced({
        glyph,
        className: 'canvas-py-glyph',
        defaults: { x: 200, y: 200, width: 400, height: calculatedHeight },
        titleBar: { label: 'py', actions: [runButton] },
        resizable: true,
        logLabel: 'PyGlyph',
    });
    element.style.minWidth = '200px';
    element.style.minHeight = '120px';
    element.style.zIndex = '1';

    // Python brand colors on title bar
    const titleBar = element.querySelector('.canvas-glyph-title-bar') as HTMLElement;
    if (titleBar) {
        titleBar.style.backgroundColor = '#2a5578';
        const labelSpan = titleBar.querySelector('span:first-child') as HTMLElement;
        if (labelSpan) {
            labelSpan.style.color = '#FFD43B';
            labelSpan.style.fontWeight = 'bold';
            labelSpan.style.flex = '1';
        }
    }

    // Execute Python code on click
    runButton.addEventListener('click', async () => {
        const editor = (element as any).editor;
        if (!editor) {
            log.error(SEG.GLYPH, '[PyGlyph] Editor not initialized');
            return;
        }

        const currentCode = editor.state.doc.toString();

        try {
            const response = await apiFetch('/api/python/execute', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({
                    code: currentCode,
                    capture_variables: false
                })
            });

            // Try to parse response body as ExecutionResult (even on 400)
            let result: ExecutionResult;
            try {
                result = await response.json();
            } catch (e) {
                // If we can't parse the body, throw a generic error
                throw new Error(`Execution failed: ${response.statusText}`);
            }

            // If response is not ok and we don't have a valid ExecutionResult, throw
            if (!response.ok && !result) {
                throw new Error(`Execution failed: ${response.statusText}`);
            }

            // TODO: Create attestation for script execution (success or failure)
            // Call attest() with:
            //   subjects: [`script:${glyph.id}`]
            //   predicates: [result.success ? "executed" : "failed"]
            //   contexts: ["canvas", "python"]
            //   attributes: {
            //     code: currentCode,
            //     stdout: result.stdout,
            //     stderr: result.stderr,
            //     error: result.error,
            //     duration_ms: result.duration_ms
            //   }
            // This creates audit trail of all Python executions on canvas.

            // Create result glyph for successful execution
            createAndDisplayResultGlyph(element, result);
        } catch (error) {
            log.error(SEG.GLYPH, '[PyGlyph] Execution failed:', error);
            log.error(SEG.ERROR, '[Python Execution Error]', error);

            // Create error result glyph for network/parse failures
            const errorResult: ExecutionResult = {
                success: false,
                stdout: '',
                stderr: '',
                result: null,
                error: error instanceof Error ? error.message : String(error),
                duration_ms: 0
            };
            createAndDisplayResultGlyph(element, errorResult);
        }
    });

    // Editor container
    const editorContainer = document.createElement('div');
    editorContainer.className = 'py-glyph-editor';
    editorContainer.style.flex = '1';
    editorContainer.style.overflow = 'hidden';
    element.appendChild(editorContainer);

    // Initialize CodeMirror with loaded code
    // TODO: Add run button in title bar that executes code via /api/python/execute
    // TODO: Add output panel below editor to show execution results
    try {
        const { EditorView, keymap } = await import('@codemirror/view');
        const { EditorState } = await import('@codemirror/state');
        const { defaultKeymap } = await import('@codemirror/commands');
        const { oneDark } = await import('@codemirror/theme-one-dark');
        const { python } = await import('@codemirror/lang-python');

        // Debounced auto-save extension
        let saveTimeout: number | undefined;
        const autoSaveExtension = EditorView.updateListener.of((update) => {
            if (update.docChanged) {
                // Clear existing timeout
                if (saveTimeout !== undefined) {
                    clearTimeout(saveTimeout);
                }

                // Debounce save for 500ms
                saveTimeout = window.setTimeout(async () => {
                    const currentCode = update.state.doc.toString();
                    await storage.save(glyph.id, currentCode);
                    log.debug(SEG.GLYPH, `[PyGlyph] Auto-saved code for ${glyph.id}`);
                }, 500);
            }
        });

        // Create editor and store reference for content access
        const editor = new EditorView({
            state: EditorState.create({
                doc: code,
                extensions: [
                    keymap.of(defaultKeymap),
                    python(),
                    oneDark,
                    EditorView.lineWrapping,
                    autoSaveExtension
                ]
            }),
            parent: editorContainer
        });

        // Store editor reference for content persistence and run button
        (element as any).editor = editor;

        // Save initial code if this is a new glyph (no saved code)
        if (!savedCode) {
            await storage.save(glyph.id, code);
            log.debug(SEG.GLYPH, `[PyGlyph] Saved initial code for new glyph ${glyph.id}`);
        }

        log.debug(SEG.GLYPH, `[PyGlyph] CodeMirror initialized for ${glyph.id}`);
    } catch (error) {
        log.error(SEG.GLYPH, `[PyGlyph] Failed to initialize CodeMirror:`, error);
        editorContainer.textContent = 'Error loading editor';
    }

    // Subscribe to sync state changes for visual feedback
    syncStateManager.subscribe(glyph.id, (state) => {
        element.dataset.syncState = state;
    });

    // Subscribe to connectivity state changes
    connectivityManager.subscribe((state) => {
        element.dataset.connectivityMode = state;
    });

    return element;
}

/**
 * Create and display a result glyph for Python execution results
 */
function createAndDisplayResultGlyph(pyElement: HTMLElement, result: ExecutionResult): void {
    // Calculate position for result glyph (directly below the py glyph)
    const pyRect = pyElement.getBoundingClientRect();
    const canvas = pyElement.closest('.canvas-workspace') as HTMLElement;
    if (!canvas) {
        log.error(SEG.GLYPH, '[PyGlyph] Cannot spawn result glyph: no canvas-workspace ancestor');
        return;
    }
    const canvasRect = canvas.getBoundingClientRect();

    const x = pyRect.left - canvasRect.left;
    const y = pyRect.bottom - canvasRect.top;

    // Create result glyph metadata
    const resultGlyphId = `result-${crypto.randomUUID()}`;
    const resultGlyph: Glyph = {
        id: resultGlyphId,
        title: 'Python Result',
        symbol: 'result',
        x,
        y,
        width: Math.round(pyRect.width),
        renderContent: () => document.createElement('div')
    };

    // Render result glyph and add to canvas (performMeld needs both on canvas)
    const resultElement = createResultGlyph(resultGlyph, result);
    canvas.appendChild(resultElement);

    // Persist to uiState with execution result
    const resultRect = resultElement.getBoundingClientRect();
    uiState.addCanvasGlyph({
        id: resultGlyphId,
        symbol: 'result',
        x,
        y,
        width: Math.round(resultRect.width),
        height: Math.round(resultRect.height),
        result: result
    });

    // Auto-meld result below py glyph (bottom port)
    const pyGlyphId = pyElement.dataset.glyphId;
    if (pyGlyphId) {
        autoMeldResultBelow(pyElement, pyGlyphId, 'py', 'Python', resultElement, resultGlyphId, 'PyGlyph');
    }
}

