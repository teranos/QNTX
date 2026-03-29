/**
 * CodeMirror 6 Editor — AX query editing with semantic highlighting
 *
 * Syntax highlighting via parse_response over main WebSocket.
 * Completions and hover: planned for panel manifestation via browser WASM.
 */

import { EditorView, keymap, lineNumbers, highlightActiveLine, highlightActiveLineGutter, Decoration, DecorationSet } from '@codemirror/view';
import { EditorState, StateField, StateEffect, RangeSetBuilder } from '@codemirror/state';
import { defaultKeymap, history, historyKeymap, indentWithTab } from '@codemirror/commands';
import { syntaxHighlighting, defaultHighlightStyle, bracketMatching } from '@codemirror/language';
import { autocompletion, completionKeymap, closeBrackets } from '@codemirror/autocomplete';
import { lintKeymap } from '@codemirror/lint';
import { requestParse } from './ats-semantic-tokens-client.ts';
import type { Diagnostic, SemanticToken } from '../types/lsp';
import { log, SEG } from './logger.ts';

let editorView: EditorView | null = null;

const updateSyntaxDecorations = StateEffect.define<DecorationSet>();

const syntaxDecorationsField = StateField.define({
    create() {
        return Decoration.none;
    },
    update(decorations, tr) {
        // Map decorations through document changes
        decorations = decorations.map(tr.changes);

        // Apply new decorations from semantic tokens
        for (let effect of tr.effects) {
            if (effect.is(updateSyntaxDecorations)) {
                decorations = effect.value;
            }
        }

        return decorations;
    },
    provide: f => EditorView.decorations.from(f)
});

/**
 * Initialize CodeMirror 6 editor
 */
export function initCodeMirrorEditor(): EditorView | null {
    const container = document.getElementById('codemirror-container');
    if (!container) {
        log.error(SEG.ERROR, 'CodeMirror container not found');
        return null;
    }

    const startState = EditorState.create({
        doc: '',
        extensions: [
            lineNumbers(),
            highlightActiveLineGutter(),
            highlightActiveLine(),
            history(),
            bracketMatching(),
            closeBrackets(),
            autocompletion(),
            syntaxHighlighting(defaultHighlightStyle),
            syntaxDecorationsField,
            keymap.of([
                ...defaultKeymap,
                ...historyKeymap,
                ...completionKeymap,
                ...lintKeymap,
                indentWithTab
            ]),
            EditorView.updateListener.of((update) => {
                if (update.docChanged) {
                    handleDocumentChange(update);
                }
            })
        ]
    });

    editorView = new EditorView({
        state: startState,
        parent: container
    });

    log.debug(SEG.UI, 'CodeMirror editor initialized');
    return editorView;
}

/**
 * Handle document changes — request parse for syntax highlighting
 */
function handleDocumentChange(update: any): void {
    const doc = update.state.doc.toString();

    // Request parse for syntax highlighting (via existing WebSocket custom protocol)
    if (editorView) {
        const cursorPos = editorView.state.selection.main.head;
        requestParse(doc, 1, cursorPos);
    }
}

/**
 * Update diagnostics and trigger linter re-evaluation
 * Called from parse response handler to display inline errors/warnings
 */
export function updateDiagnosticsDisplay(_diagnostics: Diagnostic[]): void {
    if (!editorView) return;

    // Force linter re-evaluation by dispatching an empty transaction
    // This causes the linter to be re-run and display updated diagnostics
    // Note: Diagnostics are currently not stored as the linter is not yet enabled
    editorView.dispatch({});
}

/**
 * Apply syntax highlighting from parse response tokens
 */
export function applySyntaxHighlighting(tokens: SemanticToken[]): void {
    if (!editorView || !tokens || tokens.length === 0) return;

    const builder = new RangeSetBuilder<Decoration>();

    // Convert tokens to decorations using their actual Range positions
    for (const token of tokens) {
        // Use the token's Range.Start.Offset and calculate end
        const from = token.range.start.offset;
        const to = from + token.text.length;

        // Create mark decoration with CSS class
        const decoration = Decoration.mark({
            class: `ats-${token.semantic_type}`
        });

        builder.add(from, to, decoration);
    }

    const decorations = builder.finish();

    // Dispatch effect to update decorations
    editorView.dispatch({
        effects: updateSyntaxDecorations.of(decorations)
    });
}

/**
 * Get current editor content
 */
export function getEditorContent(): string {
    if (!editorView) return '';
    return editorView.state.doc.toString();
}

/**
 * Set editor content
 */
export function setEditorContent(content: string): void {
    if (!editorView) return;

    editorView.dispatch({
        changes: {
            from: 0,
            to: editorView.state.doc.length,
            insert: content
        }
    });
}

/**
 * Cleanup editor
 */
export function destroyEditor(): void {
    if (editorView) {
        editorView.destroy();
        editorView = null;
    }
}
