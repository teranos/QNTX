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

    // Default code
    const defaultCode = '# Python editor\nprint("Hello from canvas!")\n';

    // Calculate initial size based on content
    const lineCount = defaultCode.split('\n').length;
    const lineHeight = 24; // Approximate height per line in CodeMirror
    const titleBarHeight = 36;
    const minHeight = 120;
    const maxHeight = 600;
    const calculatedHeight = Math.min(maxHeight, Math.max(minHeight, titleBarHeight + lineCount * lineHeight + 40));

    // Style element - auto-sized based on content
    element.style.position = 'absolute';
    element.style.left = `${gridX * GRID_SIZE}px`;
    element.style.top = `${gridY * GRID_SIZE}px`;
    element.style.width = '400px';
    element.style.height = `${calculatedHeight}px`;
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
    titleBar.textContent = 'py';
    titleBar.style.padding = '8px';
    titleBar.style.backgroundColor = 'var(--bg-tertiary)';
    titleBar.style.cursor = 'move';
    titleBar.style.userSelect = 'none';
    titleBar.style.fontWeight = 'bold';
    titleBar.style.fontSize = '14px';

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

    // Initialize CodeMirror
    // TODO: Add run button in title bar that executes code via /api/python/execute
    // TODO: Add output panel below editor to show execution results
    // TODO: Load code from persistence layer (filesystem/database) instead of defaultCode
    // TODO: Auto-save code changes to persistence layer (debounced)
    try {
        const { EditorView, keymap } = await import('@codemirror/view');
        const { EditorState } = await import('@codemirror/state');
        const { defaultKeymap } = await import('@codemirror/commands');
        const { oneDark } = await import('@codemirror/theme-one-dark');
        const { python } = await import('@codemirror/lang-python');

        // Create editor (prefixed with _ since not used yet - will be needed for run button)
        const _editor = new EditorView({
            state: EditorState.create({
                doc: defaultCode,
                extensions: [
                    keymap.of(defaultKeymap),
                    python(),
                    oneDark,
                    EditorView.lineWrapping
                ]
            }),
            parent: editorContainer
        });

        // TODO: Store editor reference on element for later access (needed for run button)
        // (element as any).editor = _editor;

        log.debug(SEG.UI, `[PyGlyph] CodeMirror initialized for ${glyph.id}`);
    } catch (error) {
        log.error(SEG.UI, `[PyGlyph] Failed to initialize CodeMirror:`, error);
        editorContainer.textContent = 'Error loading editor';
    }

    // Make draggable by title bar
    makeDraggable(element, titleBar, glyph);

    // Make resizable by handle
    makeResizable(element, resizeHandle);

    return element;
}

/**
 * Make an element draggable by a handle
 */
function makeDraggable(element: HTMLElement, handle: HTMLElement, glyph: Glyph): void {
    let isDragging = false;
    let dragStartX = 0;
    let dragStartY = 0;
    let elementStartX = 0;
    let elementStartY = 0;

    const handleMouseMove = (e: MouseEvent) => {
        if (!isDragging) return;

        const deltaX = e.clientX - dragStartX;
        const deltaY = e.clientY - dragStartY;
        const newX = elementStartX + deltaX;
        const newY = elementStartY + deltaY;

        element.style.left = `${newX}px`;
        element.style.top = `${newY}px`;
    };

    const handleMouseUp = () => {
        if (!isDragging) return;
        isDragging = false;

        element.style.opacity = '1';
        element.style.zIndex = '1';

        // Save position
        const rect = element.getBoundingClientRect();
        const gridX = Math.round(rect.left / GRID_SIZE);
        const gridY = Math.round(rect.top / GRID_SIZE);
        glyph.gridX = gridX;
        glyph.gridY = gridY;

        if (glyph.symbol) {
            uiState.addCanvasGlyph({
                id: glyph.id,
                symbol: glyph.symbol,
                gridX,
                gridY
            });
        }

        log.debug(SEG.UI, `[PyGlyph] Finished dragging ${glyph.id}`);

        document.removeEventListener('mousemove', handleMouseMove);
        document.removeEventListener('mouseup', handleMouseUp);
    };

    handle.addEventListener('mousedown', (e) => {
        e.preventDefault();
        e.stopPropagation();
        isDragging = true;

        dragStartX = e.clientX;
        dragStartY = e.clientY;
        const rect = element.getBoundingClientRect();
        elementStartX = rect.left;
        elementStartY = rect.top;

        element.style.opacity = '0.8';
        element.style.zIndex = '1000';

        document.addEventListener('mousemove', handleMouseMove);
        document.addEventListener('mouseup', handleMouseUp);

        log.debug(SEG.UI, `[PyGlyph] Started dragging ${glyph.id}`);
    });
}

/**
 * Make an element resizable by a handle
 */
function makeResizable(element: HTMLElement, handle: HTMLElement): void {
    let isResizing = false;
    let startX = 0;
    let startY = 0;
    let startWidth = 0;
    let startHeight = 0;

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

        element.style.opacity = '1';

        log.debug(SEG.UI, `[PyGlyph] Finished resizing`);

        document.removeEventListener('mousemove', handleMouseMove);
        document.removeEventListener('mouseup', handleMouseUp);
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

        element.style.opacity = '0.9';

        document.addEventListener('mousemove', handleMouseMove);
        document.addEventListener('mouseup', handleMouseUp);

        log.debug(SEG.UI, `[PyGlyph] Started resizing`);
    });
}
