/**
 * Note Element - Lightweight markdown notes on canvas
 *
 * Post-it style notes with basic markdown support:
 * - Bold, italic, code (marks)
 * - Headings, paragraphs (nodes)
 * - Bullet and numbered lists
 *
 * Visual style: Light beige/yellow background with dark text (post-it aesthetic)
 */

import type { Element } from '@teranos/elements';
import { setupElementResizeObserver } from '@teranos/elements';
import { log, SEG } from '../../logger';
import { uiState } from '../../state/ui';
import { createAutoSave } from './element-autosave';
import { storeCleanup, preventDrag } from '@teranos/elements';
import { tooltip } from '../tooltip';
import { canvasPlaced } from '@teranos/elements';
import { wireExpandToWindow } from '@teranos/elements';
import { Prose } from '../../sym';
import { EditorState } from 'prosemirror-state';
import { EditorView } from 'prosemirror-view';
import { history, undo, redo } from 'prosemirror-history';
import { keymap } from 'prosemirror-keymap';
import { baseKeymap } from 'prosemirror-commands';
import { noteSchema } from '../../prose/note-schema.ts';
import { noteMarkdownParser, noteMarkdownSerializer } from '../../prose/note-markdown.ts';

// Tear edge clip-path — computed once, reused on restore
const tearClipPath = (() => {
    const points: string[] = ['0% 0.5%'];
    for (let i = 1; i < 100; i++) {
        const ty = 0.3 + Math.sin(i * 0.5) * 0.15 + (Math.sin(i * 1.3) * 0.1);
        points.push(`${i}% ${ty}%`);
    }
    points.push('100% 0.5%');
    return `polygon(${points.join(', ')}, 100% calc(100% - 10px), calc(100% - 10px) 100%, 0% 100%)`;
})();

function applyPostItStyle(element: HTMLElement, item: Element): void {
    element.style.backgroundColor = item.color ?? '#f5edb8';
    element.style.color = item.textColor ?? '#2a2a2a';
    element.style.backdropFilter = 'blur(2px)';
    element.style.border = item.border ?? '1px solid #d4c59a';
    element.style.borderRadius = '2px';
    element.style.boxShadow = '2px 2px 8px rgba(0, 0, 0, 0.15)';
    element.style.cursor = 'move';
    element.style.clipPath = tearClipPath;
}

/**
 * Create a note element element and populate it
 */
export async function createNoteElement(item: Element): Promise<HTMLElement> {
    const element = document.createElement('div');
    await setupNoteElement(element, item);
    return element;
}

/**
 * Populate an element as a note element.
 * Can be called on a fresh element (createNoteElement) or an existing one (conversion).
 * Caller must runCleanup() and clear children before calling on an existing element.
 */
export async function setupNoteElement(element: HTMLElement, item: Element): Promise<void> {
    // The post-it border is visual identity on the datum — like color, the
    // window and the tray dot wear it too
    item.border ??= '1px solid #d4c59a';

    // Load saved content from canvas state
    const existingElement = uiState.getCanvasElement(item.id);
    const defaultContent = '# Note\n\nStart typing...';
    const savedContent = existingElement?.content;

    // Save initial content immediately if this is a new element
    // This prevents race condition with auto-save if user starts typing quickly
    const contentToUse = savedContent ?? defaultContent;
    if (!savedContent && existingElement) {
        uiState.addCanvasElement({ ...existingElement, content: defaultContent });
        log.debug(SEG.ELEMENT, `[Note Element] Saved initial content for new element ${item.id}`);
    }

    // Reset inline styles (important when repopulating after conversion)
    element.style.cssText = '';

    // Post-it identity — survives all manifestation transitions
    if (!item.color) item.color = '#f5edb8';
    if (!item.textColor) item.textColor = '#2a2a2a';

    canvasPlaced({
        element,
        item: item,
        className: 'canvas-note-element',
        defaults: { x: 300, y: 200, width: 320, height: 280 },
        resizable: { minWidth: 120, minHeight: 100 },
        resizeHandleClass: 'resize-handle--small',
        logLabel: 'NoteElement',
    });

    applyPostItStyle(element, item);

    // Fold-mark title bar — looks like a crease in the paper, buttons appear on hover
    const foldBar = document.createElement('div');
    foldBar.className = 'title-bar note-fold-bar';
    foldBar.style.height = '24px';
    foldBar.style.minHeight = '24px';
    foldBar.style.padding = '0 4px';
    foldBar.style.background = 'transparent';
    foldBar.style.borderBottom = '1px dashed #d4c59a';
    foldBar.style.borderRadius = '0';
    foldBar.style.cursor = 'move';
    foldBar.style.display = 'flex';
    foldBar.style.alignItems = 'center';
    foldBar.style.justifyContent = 'flex-end';
    foldBar.style.gap = '2px';

    // Expand button — morph to window
    const expandBtn = document.createElement('button');
    expandBtn.textContent = '\u2B06'; // ⬆
    expandBtn.title = 'Expand to window';
    expandBtn.style.cssText = 'width:20px;height:18px;font-size:11px;padding:0;background:transparent;border:none;color:#8a7a5a;cursor:pointer;opacity:0;transition:opacity 0.15s ease;display:flex;align-items:center;justify-content:center;';
    preventDrag(expandBtn);

    // Close button
    const closeBtn = document.createElement('button');
    closeBtn.textContent = '\u00D7'; // ×
    closeBtn.title = 'Close';
    closeBtn.style.cssText = 'width:16px;height:12px;font-size:11px;padding:0;background:transparent;border:none;color:#8a7a5a;cursor:pointer;opacity:0;transition:opacity 0.15s ease;display:flex;align-items:center;justify-content:center;';
    preventDrag(closeBtn);

    // Show all buttons on hover — including standard window controls added by morphCanvasPlacedToWindow
    foldBar.addEventListener('mouseenter', () => {
        expandBtn.style.opacity = '1';
        closeBtn.style.opacity = '1';
        const windowControls = foldBar.querySelector('.window-controls') as HTMLElement | null;
        if (windowControls) windowControls.style.opacity = '1';
    });
    foldBar.addEventListener('mouseleave', () => {
        expandBtn.style.opacity = '0';
        closeBtn.style.opacity = '0';
        const windowControls = foldBar.querySelector('.window-controls') as HTMLElement | null;
        if (windowControls) windowControls.style.opacity = '0';
    });

    // Expand handler
    wireExpandToWindow({
        element,
        expandBtn,
        elementId: item.id,
        title: 'Note',
        symbol: Prose,
        color: item.color,
        textColor: item.textColor,
        border: item.border,
        renderContent: () => {
            const content = document.createElement('div');
            content.textContent = 'Note (minimized)';
            return content;
        },
        logLabel: 'NoteElement',
        onRestoreToCanvas: () => applyPostItStyle(element, item),
        stopPropagation: true,
    });

    // Close handler
    closeBtn.addEventListener('click', (e) => {
        e.stopPropagation();
        element.remove();
        uiState.removeCanvasElement(item.id);
        log.debug(SEG.ELEMENT, `[NoteElement] Closed ${item.id}`);
    });

    foldBar.appendChild(expandBtn);
    foldBar.appendChild(closeBtn);
    element.appendChild(foldBar);

    // Editor container
    const editorContainer = document.createElement('div');
    editorContainer.className = 'note-editor-container';
    editorContainer.style.flex = '1';
    editorContainer.style.padding = '4px';
    editorContainer.style.overflow = 'auto';
    editorContainer.style.fontSize = '14px';
    editorContainer.style.fontFamily = '-apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, "Helvetica Neue", Arial, sans-serif';
    editorContainer.style.color = '#2a2a2a'; // Almost black text
    editorContainer.style.lineHeight = '1.2'; // Tight line spacing for note aesthetic
    editorContainer.style.boxSizing = 'border-box';
    editorContainer.style.cursor = 'move'; // Default cursor for dragging on padding

    // ProseMirror editor styling
    editorContainer.style.setProperty('--note-strong-color', '#1a1a1a');
    editorContainer.style.setProperty('--note-em-color', '#3a3a3a');
    editorContainer.style.setProperty('--note-code-bg', '#f0e5a8');
    editorContainer.style.setProperty('--note-code-color', '#5a4a3a');
    editorContainer.style.setProperty('--note-heading-color', '#4a3a2a');

    // Remove default ProseMirror margins and set cursor
    const style = document.createElement('style');
    style.textContent = `
        .note-editor-container .ProseMirror {
            padding: 0;
            margin: 0;
            outline: none;
            cursor: text;
            white-space: pre-wrap;
            word-break: break-word;
            overflow-wrap: anywhere;
        }
        .note-editor-container .ProseMirror p {
            margin: 0 0 0.1em 0;
        }
        .note-editor-container .ProseMirror p:last-child {
            margin-bottom: 0;
        }
        .note-editor-container .ProseMirror h1 {
            margin: 0 0 0.2em 0;
            font-size: 1.4em;
            font-weight: bold;
        }
        .note-editor-container .ProseMirror strong {
            font-weight: bold;
        }
        .note-editor-container .ProseMirror em {
            font-style: italic;
        }
        .note-editor-container .ProseMirror code {
            font-family: monospace;
            background-color: var(--note-code-bg);
            color: var(--note-code-color);
            padding: 2px 4px;
            border-radius: 2px;
        }
        .note-editor-container .ProseMirror ul,
        .note-editor-container .ProseMirror ol {
            margin: 0.1em 0;
            padding-left: 1.5em;
        }
        .note-editor-container .ProseMirror li {
            margin: 0;
        }
    `;
    editorContainer.appendChild(style);

    // Parse markdown to ProseMirror document
    let doc;
    try {
        doc = noteMarkdownParser.parse(contentToUse);
    } catch (error: unknown) {
        // Log error with context: element ID, content length, and snippet of problematic content
        const contentSnippet = contentToUse.length > 100
            ? contentToUse.substring(0, 100) + '...'
            : contentToUse;
        log.error(SEG.ELEMENT, `[Note Element] Failed to parse markdown for ${item.id}`, {
            error,
            contentLength: contentToUse.length,
            contentSnippet
        });
        // Fallback to error message in editor
        doc = noteSchema.node('doc', null, [
            noteSchema.node('paragraph', null, [
                noteSchema.text('Error loading note - check console for details')
            ])
        ]);
    }

    // Create editor state
    // NOTE: Markdown formatting not rendering for user input - see #435
    const state = EditorState.create({
        doc,
        plugins: [
            history(),
            keymap({
                'Mod-z': undo,
                'Mod-y': redo,
                'Mod-Shift-z': redo
            }),
            keymap(baseKeymap)
        ]
    });

    // Create editor view with auto-save
    const { save, cancel: cancelAutoSave } = createAutoSave(item.id, () => noteMarkdownSerializer.serialize(editorView.state.doc), 'Note Element');

    const editorView = new EditorView(editorContainer, {
        state,
        dispatchTransaction: (transaction) => {
            const newState = editorView.state.apply(transaction);
            editorView.updateState(newState);
            if (transaction.docChanged) save();
        },
        editable: () => true,
        attributes: {
            spellcheck: 'false'
        }
    });

    // Prevent drag only when clicking on actual editor content, not padding
    editorContainer.addEventListener('mousedown', (e) => {
        const target = e.target as HTMLElement;
        // Only stop propagation if clicking on ProseMirror content
        if (target.closest('.ProseMirror') || target.classList.contains('ProseMirror')) {
            e.stopPropagation();
        }
        // Clicking on padding area allows drag to work
    });

    // Assemble element (no title bar — draggable by element itself via canvasPlaced)
    element.appendChild(editorContainer);

    // Set up ResizeObserver for auto-sizing element to content
    // Note elements have no title bar — use 8px padding offset; observe ProseMirror child
    const proseMirror = editorContainer.querySelector('.ProseMirror');
    if (proseMirror) {
        setupElementResizeObserver(element, proseMirror as HTMLElement, `Note ${item.id}`, 8);
    } else {
        log.warn(SEG.ELEMENT, `[Note ${item.id}] ProseMirror element not found for ResizeObserver`);
    }

    // Register cleanup for conversions (drag/resize cleanup handled by canvasPlaced)
    storeCleanup(element, () => editorView.destroy());
    storeCleanup(element, cancelAutoSave);
    storeCleanup(element, () => {
        const observer = (element as any).__resizeObserver;
        if (observer) {
            observer.disconnect();
            delete (element as any).__resizeObserver;
        }
    });

    // Attach tooltip support
    tooltip.attach(element);
}

