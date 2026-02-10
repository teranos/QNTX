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
import { MAX_VIEWPORT_HEIGHT_RATIO } from './glyph';
import { log, SEG } from '../../logger';
import { uiState } from '../../state/ui';
import { storeCleanup } from './glyph-interaction';
import { tooltip } from '../tooltip';
import { canvasPlaced } from './manifestations/canvas-placed';
import { EditorState } from 'prosemirror-state';
import { EditorView } from 'prosemirror-view';
import { history, undo, redo } from 'prosemirror-history';
import { keymap } from 'prosemirror-keymap';
import { baseKeymap } from 'prosemirror-commands';
import { noteSchema } from '../../prose/note-schema.ts';
import { noteMarkdownParser, noteMarkdownSerializer } from '../../prose/note-markdown.ts';

/**
 * Create a note glyph element and populate it
 */
export async function createNoteGlyph(glyph: Glyph): Promise<HTMLElement> {
    const element = document.createElement('div');
    await setupNoteGlyph(element, glyph);
    return element;
}

/**
 * Populate an element as a note glyph.
 * Can be called on a fresh element (createNoteGlyph) or an existing one (conversion).
 * Caller must runCleanup() and clear children before calling on an existing element.
 */
export async function setupNoteGlyph(element: HTMLElement, glyph: Glyph): Promise<void> {
    // Load saved content from canvas state
    const existingGlyph = uiState.getCanvasGlyphs().find(g => g.id === glyph.id);
    const defaultContent = '# Note\n\nStart typing...';
    const savedContent = existingGlyph?.content;

    // Save initial content immediately if this is a new glyph
    // This prevents race condition with auto-save if user starts typing quickly
    const contentToUse = savedContent ?? defaultContent;
    if (!savedContent && existingGlyph) {
        uiState.addCanvasGlyph({ ...existingGlyph, content: defaultContent });
        log.debug(SEG.GLYPH, `[Note Glyph] Saved initial content for new glyph ${glyph.id}`);
    }

    // Reset inline styles (important when repopulating after conversion)
    element.style.cssText = '';

    canvasPlaced({
        element,
        glyph,
        className: 'canvas-note-glyph',
        defaults: { x: 300, y: 200, width: 320, height: 280 },
        resizable: { minWidth: 120, minHeight: 100 },
        resizeHandleClass: 'glyph-resize-handle--small',
        logLabel: 'NoteGlyph',
    });

    // Post-it note styling: light beige/yellow background with torn top edge
    element.style.backgroundColor = '#f5edb8';
    element.style.border = '1px solid #d4c59a';
    element.style.borderRadius = '2px';
    element.style.boxShadow = '2px 2px 8px rgba(0, 0, 0, 0.15)';
    element.style.cursor = 'move';

    // Torn edge pattern using clip-path (percentage-based, scales with width)
    // Only tear the top edge, keep sides and bottom straight (except corner cutout)
    // Subtle tear effect - small amplitude for natural look
    const tearPoints: string[] = ['0% 0.5%'];
    for (let i = 1; i < 100; i++) {
        const ty = 0.3 + Math.sin(i * 0.5) * 0.15 + (Math.sin(i * 1.3) * 0.1);
        tearPoints.push(`${i}% ${ty}%`);
    }
    tearPoints.push('100% 0.5%');

    // Complete the shape: straight right side, corner cutout at bottom, straight left side
    element.style.clipPath = `polygon(${tearPoints.join(', ')}, 100% calc(100% - 10px), calc(100% - 10px) 100%, 0% 100%)`;

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
        // Log error with context: glyph ID, content length, and snippet of problematic content
        const contentSnippet = contentToUse.length > 100
            ? contentToUse.substring(0, 100) + '...'
            : contentToUse;
        log.error(SEG.GLYPH, `[Note Glyph] Failed to parse markdown for ${glyph.id}`, {
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
                saveTimeout = window.setTimeout(() => {
                    const markdown = noteMarkdownSerializer.serialize(editorView.state.doc);
                    const existing = uiState.getCanvasGlyphs().find(g => g.id === glyph.id);
                    if (existing) {
                        uiState.addCanvasGlyph({ ...existing, content: markdown });
                        log.debug(SEG.GLYPH, `[Note Glyph] Auto-saved content for ${glyph.id}`);
                    }
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

    // Assemble glyph (no title bar — draggable by element itself via canvasPlaced)
    element.appendChild(editorContainer);

    // Set up ResizeObserver for auto-sizing glyph to content
    setupNoteGlyphResizeObserver(element, editorContainer, glyph.id);

    // Register cleanup for conversions (drag/resize cleanup handled by canvasPlaced)
    storeCleanup(element, () => editorView.destroy());
    storeCleanup(element, () => {
        if (saveTimeout !== undefined) clearTimeout(saveTimeout);
    });
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

/**
 * Set up ResizeObserver to auto-size note glyph to match editor content height
 * Works alongside manual resize handles - user can still drag to resize
 */
function setupNoteGlyphResizeObserver(
    glyphElement: HTMLElement,
    editorContainer: HTMLElement,
    glyphId: string
): void {
    // Cleanup any existing observer to prevent memory leaks on re-render
    const existingObserver = (glyphElement as any).__resizeObserver;
    if (existingObserver && typeof existingObserver.disconnect === 'function') {
        existingObserver.disconnect();
        delete (glyphElement as any).__resizeObserver;
        log.debug(SEG.GLYPH, `[Note ${glyphId}] Disconnected existing ResizeObserver`);
    }

    const padding = 8; // 4px padding on top and bottom
    const maxHeight = window.innerHeight * MAX_VIEWPORT_HEIGHT_RATIO;

    const resizeObserver = new ResizeObserver(entries => {
        for (const entry of entries) {
            const contentHeight = entry.contentRect.height;
            const totalHeight = Math.min(contentHeight + padding, maxHeight);

            // Update minHeight instead of height to allow manual resize
            glyphElement.style.minHeight = `${totalHeight}px`;

            log.debug(SEG.GLYPH, `[Note ${glyphId}] Auto-resized to ${totalHeight}px (content: ${contentHeight}px)`);
        }
    });

    // Observe the ProseMirror editor element for content changes
    const proseMirrorElement = editorContainer.querySelector('.ProseMirror');
    if (proseMirrorElement) {
        resizeObserver.observe(proseMirrorElement);
    } else {
        log.warn(SEG.GLYPH, `[Note ${glyphId}] ProseMirror element not found for ResizeObserver`);
    }

    // Store observer for cleanup
    (glyphElement as any).__resizeObserver = resizeObserver;
}
