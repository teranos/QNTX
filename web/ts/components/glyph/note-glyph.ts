/**
 * Note Glyph - Lightweight markdown notes on canvas
 *
 * Post-it style notes with basic markdown support:
 * - Bold, italic, code (marks)
 * - Headings, paragraphs (nodes)
 * - Bullet and numbered lists
 *
 * Visual style: Light beige/yellow background with dark text (post-it aesthetic)
 */

import type { Glyph } from './glyph';
import { Prose } from '@generated/sym.js';
import { log, SEG } from '../../logger';
import { getScriptStorage } from '../../storage/script-storage';
import { makeDraggable, makeResizable } from './glyph-interaction';
import { tooltip } from '../tooltip';
import { EditorState } from 'prosemirror-state';
import { EditorView } from 'prosemirror-view';
import { history, undo, redo } from 'prosemirror-history';
import { keymap } from 'prosemirror-keymap';
import { baseKeymap } from 'prosemirror-commands';
import { noteSchema } from '../../prose/note-schema.ts';
import { noteMarkdownParser, noteMarkdownSerializer } from '../../prose/note-markdown.ts';

/**
 * Create a note glyph with lightweight markdown editor on canvas
 */
export async function createNoteGlyph(glyph: Glyph): Promise<HTMLElement> {
    // Load saved content from storage
    const storage = getScriptStorage();
    const defaultContent = '# Note\n\nStart typing...';
    const savedContent = await storage.load(glyph.id) ?? defaultContent;

    const element = document.createElement('div');
    element.className = 'canvas-note-glyph';
    element.dataset.glyphId = glyph.id;
    element.dataset.glyphSymbol = Prose;

    const x = glyph.x ?? 300;
    const y = glyph.y ?? 200;
    const width = glyph.width ?? 320;
    const height = glyph.height ?? 280;

    element.style.position = 'absolute';
    element.style.left = `${x}px`;
    element.style.top = `${y}px`;
    element.style.width = `${width}px`;
    element.style.height = `${height}px`;

    // Post-it note styling: light beige/yellow background
    element.style.backgroundColor = '#fffacd'; // Lemon chiffon
    element.style.border = '1px solid #e6daa6'; // Darker beige border
    element.style.borderRadius = '2px';
    element.style.boxShadow = '2px 2px 8px rgba(0, 0, 0, 0.15)';
    element.style.display = 'flex';
    element.style.flexDirection = 'column';
    element.style.overflow = 'hidden';
    element.style.cursor = 'move'; // Entire note is draggable

    // Editor container
    const editorContainer = document.createElement('div');
    editorContainer.className = 'note-editor-container';
    editorContainer.style.flex = '1';
    editorContainer.style.padding = '10px';
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
        }
        .note-editor-container .ProseMirror p {
            margin: 0 0 0.1em 0;
        }
        .note-editor-container .ProseMirror p:last-child {
            margin-bottom: 0;
        }
        .note-editor-container .ProseMirror h1 {
            margin: 0 0 0.2em 0;
        }
    `;
    editorContainer.appendChild(style);

    // Parse markdown to ProseMirror document
    let doc;
    try {
        doc = noteMarkdownParser.parse(savedContent);
    } catch (error: unknown) {
        log.error(SEG.GLYPH, `[Note Glyph] Failed to parse markdown for ${glyph.id}:`, error);
        doc = noteSchema.node('doc', null, [
            noteSchema.node('paragraph', null, [
                noteSchema.text('Error loading note')
            ])
        ]);
    }

    // Create editor state
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

    // Auto-save tracking
    let saveTimeout: number | undefined;

    // Create editor view
    const editorView = new EditorView(editorContainer, {
        state,
        dispatchTransaction: (transaction) => {
            const newState = editorView.state.apply(transaction);
            editorView.updateState(newState);

            // Auto-save on content change
            if (transaction.docChanged) {
                if (saveTimeout !== undefined) {
                    clearTimeout(saveTimeout);
                }
                saveTimeout = window.setTimeout(async () => {
                    const markdown = noteMarkdownSerializer.serialize(editorView.state.doc);
                    await storage.save(glyph.id, markdown);
                    log.debug(SEG.GLYPH, `[Note Glyph] Auto-saved content for ${glyph.id}`);
                }, 500);
            }
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

    // Assemble glyph (no title bar)
    element.appendChild(editorContainer);

    // Resize handle - triangular paper fold corner (like real post-it note)
    const resizeHandle = document.createElement('div');
    resizeHandle.className = 'note-glyph-resize-handle';
    resizeHandle.style.position = 'absolute';
    resizeHandle.style.bottom = '0';
    resizeHandle.style.right = '0';
    resizeHandle.style.width = '0';
    resizeHandle.style.height = '0';
    resizeHandle.style.borderStyle = 'solid';
    resizeHandle.style.borderWidth = '0 0 20px 20px';
    resizeHandle.style.borderColor = 'transparent transparent #d4c59a transparent'; // Darker corner for fold
    resizeHandle.style.cursor = 'nwse-resize';
    resizeHandle.style.borderBottomRightRadius = '2px';
    element.appendChild(resizeHandle);

    // Save initial content if new glyph
    if (!(await storage.load(glyph.id))) {
        await storage.save(glyph.id, defaultContent);
    }

    // Make entire note draggable (no title bar) and resizable
    makeDraggable(element, element, glyph, { logLabel: 'NoteGlyph' });
    makeResizable(element, resizeHandle, glyph, {
        logLabel: 'NoteGlyph',
        minWidth: 200,
        minHeight: 150
    });

    // Attach tooltip support
    tooltip.attach(element);

    return element;
}
