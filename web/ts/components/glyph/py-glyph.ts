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

import type { Glyph } from '@qntx/glyphs';
import { log, SEG } from '../../logger';
import { uiState } from '../../state/ui';
import { createAutoSave } from './glyph-autosave';
import type { ExecutionResult } from './result-glyph';
import { syncStateManager } from '../../state/sync-state';
import { connectivityManager } from '../../connectivity';
import { createGlyphUI } from './glyph-ui';

export const PY_DEFAULT_CODE = `import time
import secrets

foo = ['teach', 'meld', 'attach', 'developer', 'test', 'glyph']
if upstream:
  print(f"Fired! {upstream['subjects']} {upstream['predicates']} {upstream['contexts']}")
  time.sleep(0.35)
  attest(
    subjects=["python"],
    predicates=[secrets.choice(foo)],
    contexts=["qntx"],
    attributes={"key": secrets.choice(foo)}
  )
else:
  print("No upstream — ran manually")
`;

/**
 * Create a Python editor glyph with CodeMirror
 *
 * Code is stored in uiState.canvasGlyphs (synced to IndexedDB + backend)
 */
export async function createPyGlyph(glyph: Glyph): Promise<HTMLElement> {
    // Load code from canvas state or use default
    const existingGlyph = uiState.getCanvasGlyph(glyph.id);
    const code = existingGlyph?.content ?? PY_DEFAULT_CODE;

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
    runButton.className = 'titlebar-btn';
    runButton.title = 'Run Python code';

    const ui = createGlyphUI(glyph, 'python');
    const { element, content } = ui.glyph({
        defaults: { x: 200, y: 200, width: 400, height: calculatedHeight },
        titleBar: { label: 'py', actions: [runButton], color: '#2a5578', labelColor: '#FFD43B' },
        resizable: true,
        className: 'canvas-py-glyph',
    });
    element.style.minWidth = '200px';
    element.style.minHeight = '120px';
    element.style.zIndex = '1';

    // Execute Python code on click
    runButton.addEventListener('click', async () => {
        const editor = (element as any).editor;
        if (!editor) {
            ui.log.error('Editor not initialized');
            return;
        }

        const currentCode = editor.state.doc.toString();

        try {
            const response = await ui.pluginFetch('/execute', {
                method: 'POST',
                body: { content: currentCode, capture_variables: false, glyph_id: glyph.id },
            });

            let result: ExecutionResult;
            try {
                result = await response.json();
            } catch {
                throw new Error(`Execution failed: ${response.statusText}`);
            }

            if (!response.ok && !result) {
                throw new Error(`Execution failed: ${response.statusText}`);
            }

            ui.spawnResult(result);
        } catch (error) {
            ui.log.error('Execution failed:', error);

            ui.spawnResult({
                success: false,
                stdout: '',
                stderr: '',
                result: null,
                error: error instanceof Error ? error.message : String(error),
                duration_ms: 0,
            });
        }
    });

    // Editor container
    const editorContainer = document.createElement('div');
    editorContainer.className = 'py-glyph-editor';
    editorContainer.style.flex = '1';
    editorContainer.style.overflow = 'hidden';
    content.appendChild(editorContainer);

    // Initialize CodeMirror with loaded code
    try {
        const { EditorView, keymap } = await import('@codemirror/view');
        const { EditorState } = await import('@codemirror/state');
        const { defaultKeymap } = await import('@codemirror/commands');
        const { oneDark } = await import('@codemirror/theme-one-dark');
        const { python } = await import('@codemirror/lang-python');

        const { save } = createAutoSave(glyph.id, () => editor.state.doc.toString(), 'PyGlyph');
        const autoSaveExtension = EditorView.updateListener.of((update) => {
            if (update.docChanged) save();
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
        if (!existingGlyph?.content) {
            const canvasGlyph = uiState.getCanvasGlyph(glyph.id);
            if (canvasGlyph) {
                uiState.addCanvasGlyph({ ...canvasGlyph, content: code });
                log.debug(SEG.GLYPH, `[PyGlyph] Saved initial code for new glyph ${glyph.id}`);
            }
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


