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
import { GRID_SIZE } from './grid-constants';
import { makeDraggable, makeResizable } from './glyph-interaction';
import { getScriptStorage } from '../../storage/script-storage';
import { apiFetch } from '../../api';
import { createResultGlyph, type ExecutionResult } from './result-glyph';

/**
 * Create a Python editor glyph with CodeMirror
 *
 * TODO: Accept code content as parameter instead of always using defaultCode
 * TODO: Store editor reference for later access (code execution, content updates)
 */
export async function createPyGlyph(glyph: Glyph): Promise<HTMLElement> {
    const element = document.createElement('div');
    element.className = 'canvas-py-glyph';
    element.dataset.glyphId = glyph.id;

    const gridX = glyph.gridX ?? 5;
    const gridY = glyph.gridY ?? 5;

    // Load code from storage or use default
    const storage = getScriptStorage();
    const defaultCode = '# Python editor\nprint("Hello from canvas!")\n';
    const savedCode = await storage.load(glyph.id);
    const code = savedCode ?? defaultCode;

    // Calculate initial size based on content (if no saved size)
    const lineCount = code.split('\n').length;
    const lineHeight = 24; // Approximate height per line in CodeMirror
    const titleBarHeight = 36;
    const minHeight = 120;
    const maxHeight = 600;
    const calculatedHeight = Math.min(maxHeight, Math.max(minHeight, titleBarHeight + lineCount * lineHeight + 40));

    // Use saved size if available, otherwise use defaults
    const width = glyph.width ?? 400;
    const height = glyph.height ?? calculatedHeight;

    // Style element - auto-sized based on content or restored from saved size
    element.style.position = 'absolute';
    element.style.left = `${gridX * GRID_SIZE}px`;
    element.style.top = `${gridY * GRID_SIZE}px`;
    element.style.width = `${width}px`;
    element.style.height = `${height}px`;
    element.style.minWidth = '200px';
    element.style.minHeight = '120px';
    element.style.backgroundColor = 'var(--bg-secondary)';
    element.style.borderRadius = '4px';
    element.style.border = '1px solid var(--border-color)';
    element.style.display = 'flex';
    element.style.flexDirection = 'column';
    element.style.overflow = 'hidden';
    element.style.zIndex = '1';

    // Title bar for dragging
    const titleBar = document.createElement('div');
    titleBar.className = 'py-glyph-title-bar';
    titleBar.style.padding = '8px';
    titleBar.style.backgroundColor = 'var(--bg-tertiary)';
    titleBar.style.cursor = 'move';
    titleBar.style.userSelect = 'none';
    titleBar.style.fontWeight = 'bold';
    titleBar.style.fontSize = '14px';
    titleBar.style.display = 'flex';
    titleBar.style.alignItems = 'center';
    titleBar.style.justifyContent = 'space-between';

    // Label
    const label = document.createElement('span');
    label.textContent = 'py';
    titleBar.appendChild(label);

    // Run button
    const runButton = document.createElement('button');
    runButton.textContent = '▶';
    runButton.title = 'Run Python code';
    runButton.style.background = 'var(--bg-hover)';
    runButton.style.border = '1px solid var(--border-color)';
    runButton.style.borderRadius = '3px';
    runButton.style.padding = '2px 8px';
    runButton.style.cursor = 'pointer';
    runButton.style.fontSize = '12px';
    runButton.style.color = 'var(--text-primary)';

    // Prevent drag when clicking button
    runButton.addEventListener('mousedown', (e) => {
        e.stopPropagation();
    });

    // Execute Python code on click
    runButton.addEventListener('click', async () => {
        const editor = (element as any).editor;
        if (!editor) {
            log.error(SEG.UI, '[PyGlyph] Editor not initialized');
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
            log.error(SEG.UI, '[PyGlyph] Execution failed:', error);
            console.error('[Python Execution Error]', error);

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

    titleBar.appendChild(runButton);

    // TODO: Add click handler to spawn programmature manifestation
    // titleBar.addEventListener('click', () => spawnProgrammatureManifestation(glyph));

    element.appendChild(titleBar);

    // Editor container
    const editorContainer = document.createElement('div');
    editorContainer.className = 'py-glyph-editor';
    editorContainer.style.flex = '1';
    editorContainer.style.overflow = 'hidden';
    element.appendChild(editorContainer);

    // Resize handle
    const resizeHandle = document.createElement('div');
    resizeHandle.className = 'py-glyph-resize-handle';
    resizeHandle.style.position = 'absolute';
    resizeHandle.style.bottom = '0';
    resizeHandle.style.right = '0';
    resizeHandle.style.width = '16px';
    resizeHandle.style.height = '16px';
    resizeHandle.style.cursor = 'nwse-resize';
    resizeHandle.style.backgroundColor = 'var(--bg-tertiary)';
    resizeHandle.style.borderTopLeftRadius = '4px';
    element.appendChild(resizeHandle);

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
                    log.debug(SEG.UI, `[PyGlyph] Auto-saved code for ${glyph.id}`);
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
            log.debug(SEG.UI, `[PyGlyph] Saved initial code for new glyph ${glyph.id}`);
        }

        log.debug(SEG.UI, `[PyGlyph] CodeMirror initialized for ${glyph.id}`);
    } catch (error) {
        log.error(SEG.UI, `[PyGlyph] Failed to initialize CodeMirror:`, error);
        editorContainer.textContent = 'Error loading editor';
    }

    // Make draggable by title bar
    makeDraggable(element, titleBar, glyph, { logLabel: 'PyGlyph' });

    // Make resizable by handle
    makeResizable(element, resizeHandle, glyph, { logLabel: 'PyGlyph' });

    return element;
}

/**
 * Create and display a result glyph for Python execution results
 */
function createAndDisplayResultGlyph(pyElement: HTMLElement, result: ExecutionResult): void {
    // Calculate position for result glyph (directly below the py glyph)
    const pyRect = pyElement.getBoundingClientRect();
    const canvas = pyElement.parentElement;
    const canvasRect = canvas?.getBoundingClientRect() ?? { left: 0, top: 0 };

    const pyGridX = Math.round((pyRect.left - canvasRect.left) / GRID_SIZE);
    const pyBottomY = pyRect.bottom - canvasRect.top;
    const resultGridY = Math.round(pyBottomY / GRID_SIZE);

    // Create result glyph metadata
    const resultGlyph: Partial<Glyph> & { id: string; symbol: string; gridX: number; gridY: number } = {
        id: `result-${crypto.randomUUID()}`,
        title: 'Python Result',
        symbol: 'result',
        gridX: pyGridX,
        gridY: resultGridY,
        width: Math.round(pyRect.width)
    };

    // Render result glyph
    const resultElement = createResultGlyph(resultGlyph as Glyph, result);
    canvas?.appendChild(resultElement);

    // Persist to uiState with execution result
    const resultRect = resultElement.getBoundingClientRect();
    uiState.addCanvasGlyph({
        id: resultGlyph.id,
        symbol: 'result',
        gridX: resultGlyph.gridX,
        gridY: resultGlyph.gridY,
        width: Math.round(resultRect.width),
        height: Math.round(resultRect.height),
        result: result
    });

    log.debug(SEG.UI, `[PyGlyph] Spawned result glyph at grid (${pyGridX}, ${resultGridY}), duration ${result.duration_ms}ms`);
}

