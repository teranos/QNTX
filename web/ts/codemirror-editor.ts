/**
 * CodeMirror 6 Editor with LSP Integration
 */

import { EditorView, keymap, lineNumbers, highlightActiveLine, highlightActiveLineGutter, Decoration, DecorationSet } from '@codemirror/view';
import { EditorState, StateField, StateEffect, RangeSetBuilder } from '@codemirror/state';
import { defaultKeymap, history, historyKeymap, indentWithTab } from '@codemirror/commands';
import { syntaxHighlighting, defaultHighlightStyle, bracketMatching } from '@codemirror/language';
import { autocompletion, completionKeymap, closeBrackets } from '@codemirror/autocomplete';
import { lintKeymap } from '@codemirror/lint';
import { languageServer } from 'codemirror-languageserver';

// Linter - TODO: enable once we have a proper way to import this
// import { linter } from '@codemirror/lint';

// DISABLED: LSP WebSocket transport conflicts with main WebSocket
// import { createLSPClient } from './lsp-websocket-transport.js';
import { sendMessage, validateBackendURL } from './websocket.ts';
import { requestParse } from './ats-semantic-tokens-client.ts';
import type { Diagnostic, SemanticToken } from '../types/lsp';
import { SearchView } from './search-view.ts';
import { log, SEG } from './logger.ts';

let editorView: EditorView | null = null;
let queryTimeout: ReturnType<typeof setTimeout> | null = null;
let searchView: SearchView | null = null;

// Syntax highlighting via LSP semantic tokens
// TODO(issue #13): codemirror-languageserver doesn't support semantic tokens yet (v1.18.1)
// For now, we manually request them from LSP server

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
 * Initialize CodeMirror 6 editor with LSP support
 */
export function initCodeMirrorEditor(): EditorView | null {
    const container = document.getElementById('codemirror-container');
    if (!container) {
        log.error(SEG.ERROR, 'CodeMirror container not found');
        return null;
    }

    // Initialize search view (always-on, renders results in overlay)
    searchView = new SearchView();
    searchView.show();

    // LSP configuration (async connection, won't block page load)
    // Use backend URL from injected global with validation
    const rawUrl = (window as any).__BACKEND_URL__ || window.location.origin;
    const validatedUrl = validateBackendURL(rawUrl);

    if (!validatedUrl) {
        log.error(SEG.ERROR, '[LSP] Invalid backend URL:', rawUrl);
        log.debug(SEG.UI, '[LSP] Falling back to same-origin');
    }

    const backendUrl = validatedUrl || window.location.origin;
    const backendHost = backendUrl.replace(/^https?:\/\//, '');
    const protocol = backendUrl.startsWith('https') ? 'wss:' : 'ws:';
    const serverUri = `${protocol}//${backendHost}/lsp` as `ws://${string}` | `wss://${string}`;

    log.debug(SEG.UI, '[LSP] Configuring connection to', serverUri);

    // Create editor state with LSP extension
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
            syntaxDecorationsField, // ATS semantic token highlighting (manual LSP request)
            // linter(createAtsLinter()), // ATS parse error diagnostics - TODO: enable when linter is available
            // LSP features: completions, hover, diagnostics (async connection)
            // TODO(issue #9): Interactive hover with related attestations
            // Enhance hover to show clickable related entities (subjects, contexts, predicates)
            // Allow users to explore connections and refine queries by clicking
            languageServer({
                serverUri: serverUri,
                rootUri: 'file:///',
                documentUri: 'inmemory://ats-query',
                languageId: 'ats',
                workspaceFolders: null
            }),
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

    // Create editor view
    editorView = new EditorView({
        state: startState,
        parent: container
    });

    log.debug(SEG.UI, 'CodeMirror 6 editor initialized with LSP support');
    return editorView;
}

/**
 * Handle document changes - notify LSP server AND execute query
 */
function handleDocumentChange(update: any): void {
    const doc = update.state.doc.toString();

    // Request parse for syntax highlighting (via existing WebSocket custom protocol)
    if (editorView) {
        const cursorPos = editorView.state.selection.main.head;
        requestParse(doc, 1, cursorPos);
    }

    // Execute search with debounce
    if (queryTimeout) {
        clearTimeout(queryTimeout);
    }
    queryTimeout = setTimeout(() => {
        sendSearch(doc.trim());
    }, 300);
}

/**
 * Send search query
 */
function sendSearch(text: string): void {
    if (!text.trim()) {
        // Clear results if empty
        if (searchView) {
            searchView.clear();
        }
        return;
    }

    // Send as rich_search message type
    sendMessage({
        type: 'rich_search',
        query: text
    });
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
 * TODO(issue #13): Request semantic tokens from LSP instead of parse_response
 * For now using old parse_response until codemirror-languageserver adds semantic token support
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
 * Handle search results from WebSocket
 */
export function handleSearchResults(message: any): void {
    if (!searchView) return;
    searchView.updateResults(message);
}

/**
 * Cleanup editor and LSP client
 */
export function destroyEditor(): void {
    if (editorView) {
        editorView.destroy();
        editorView = null;
    }
    if (searchView) {
        searchView = null;
    }
    // LSP client is managed by languageServer() extension, no manual cleanup needed
}