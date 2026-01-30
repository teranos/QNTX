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
import { requestParse, PARSE_DEBOUNCE_MS } from './ats-semantic-tokens-client.ts';
import type { Diagnostic, SemanticToken } from '../types/lsp';
import { FuzzySearchView } from './fuzzy-search-view.ts';

let editorView: EditorView | null = null;
let queryTimeout: ReturnType<typeof setTimeout> | null = null;
let parseTimeout: ReturnType<typeof setTimeout> | null = null;
let fuzzySearchView: FuzzySearchView | null = null;
let editorMode: 'ats' | 'fuzzy' = 'ats'; // Track current mode

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
 * Create the mode toggle button
 */
function createModeToggle(): void {
    const queryContainer = document.getElementById('query-container');
    if (!queryContainer) return;

    // Create toggle button container
    const toggleContainer = document.createElement('div');
    toggleContainer.style.cssText = `
        display: flex;
        align-items: center;
        padding: 8px 12px;
        border-bottom: 1px solid #333;
        background: rgba(0, 0, 0, 0.3);
    `;

    // Create toggle button
    const toggleButton = document.createElement('button');
    toggleButton.id = 'mode-toggle';
    toggleButton.style.cssText = `
        padding: 4px 12px;
        background: #1e2021;
        color: #d3c6aa;
        border: 1px solid #475258;
        border-radius: 3px;
        cursor: pointer;
        font-size: 12px;
        transition: all 0.2s ease;
    `;
    toggleButton.textContent = '⋈ ATS Mode';

    toggleButton.onclick = () => {
        toggleMode();
    };

    // Add mode label
    const modeLabel = document.createElement('span');
    modeLabel.style.cssText = `
        margin-left: 12px;
        font-size: 11px;
        color: #888;
    `;
    modeLabel.textContent = 'Graph updates live';
    modeLabel.id = 'mode-label';

    toggleContainer.appendChild(toggleButton);
    toggleContainer.appendChild(modeLabel);

    // Insert before CodeMirror container
    const codemirrorContainer = document.getElementById('codemirror-container');
    if (codemirrorContainer && codemirrorContainer.parentNode) {
        codemirrorContainer.parentNode.insertBefore(toggleContainer, codemirrorContainer);
    }
}

/**
 * Toggle between ATS and Fuzzy search modes
 */
function toggleMode(): void {
    const button = document.getElementById('mode-toggle') as HTMLButtonElement;
    const label = document.getElementById('mode-label');

    if (editorMode === 'ats') {
        editorMode = 'fuzzy';
        if (button) {
            button.textContent = '🔍 Fuzzy Search Mode';
            button.style.background = '#2a3f5f';
        }
        if (label) {
            label.textContent = 'Search RichStringFields';
        }

        // Show fuzzy search view
        if (!fuzzySearchView) {
            fuzzySearchView = new FuzzySearchView();
        }
        fuzzySearchView.show();

        // Clear and re-run current query in fuzzy mode if there's content
        const content = getEditorContent();
        if (content.trim()) {
            sendFuzzySearch(content.trim());
        }
    } else {
        editorMode = 'ats';
        if (button) {
            button.textContent = '⋈ ATS Mode';
            button.style.background = '#1e2021';
        }
        if (label) {
            label.textContent = 'Graph updates live';
        }

        // Hide fuzzy search view
        if (fuzzySearchView) {
            fuzzySearchView.hide();
        }

        // Re-run queries in ATS mode
        const content = getEditorContent();
        if (content.trim()) {
            sendMultiLineQueries(content);
        }
    }
}

/**
 * Initialize CodeMirror 6 editor with LSP support
 */
export function initCodeMirrorEditor(): EditorView | null {
    const container = document.getElementById('codemirror-container');
    if (!container) {
        console.error('CodeMirror container not found');
        return null;
    }

    // Create mode toggle button
    createModeToggle();

    // LSP configuration (async connection, won't block page load)
    // Use backend URL from injected global with validation
    const rawUrl = (window as any).__BACKEND_URL__ || window.location.origin;
    const validatedUrl = validateBackendURL(rawUrl);

    if (!validatedUrl) {
        console.error('[LSP] Invalid backend URL:', rawUrl);
        console.log('[LSP] Falling back to same-origin');
    }

    const backendUrl = validatedUrl || window.location.origin;
    const backendHost = backendUrl.replace(/^https?:\/\//, '');
    const protocol = backendUrl.startsWith('https') ? 'wss:' : 'ws:';
    const serverUri = `${protocol}//${backendHost}/lsp` as `ws://${string}` | `wss://${string}`;

    console.log('[LSP] Configuring connection to', serverUri);

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

    console.log('CodeMirror 6 editor initialized with LSP support');
    return editorView;
}

/**
 * Handle document changes - notify LSP server AND execute query
 */
function handleDocumentChange(update: any): void {
    const doc = update.state.doc.toString();

    // Request parse for syntax highlighting (via existing WebSocket custom protocol)
    // Only in ATS mode - fuzzy search doesn't need LSP parsing
    if (editorMode === 'ats') {
        if (parseTimeout) {
            clearTimeout(parseTimeout);
        }
        parseTimeout = setTimeout(() => {
            if (editorView) {
                const cursorPos = editorView.state.selection.main.head;
                requestParse(doc, 1, cursorPos);
            }
        }, PARSE_DEBOUNCE_MS);
    }

    // Execute query with debounce based on mode
    if (queryTimeout) {
        clearTimeout(queryTimeout);
    }
    queryTimeout = setTimeout(() => {
        if (editorMode === 'ats') {
            sendMultiLineQueries(doc);
        } else {
            // In fuzzy mode, send the entire text as a search query
            sendFuzzySearch(doc.trim());
        }
    }, 300); // 300ms debounce for query execution
}

/**
 * Send multi-line queries (one per line, skipping empty lines)
 * Copied from ats-editor.js to maintain execute-as-you-type functionality
 */
function sendMultiLineQueries(text: string): void {
    const lines = text.split('\n');
    const nonEmptyLines = lines.filter(line => line.trim() !== '');

    // Send each non-empty line as a separate query
    nonEmptyLines.forEach((line: string) => {
        sendMessage({
            type: 'query',
            query: line.trim()
        });
    });

    // Status indicator removed - was low signal to users
}

/**
 * Send fuzzy search query
 */
function sendFuzzySearch(text: string): void {
    if (!text.trim()) {
        // Clear results if empty
        if (fuzzySearchView) {
            fuzzySearchView.clear();
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
 * Handle fuzzy search results from WebSocket
 */
export function handleFuzzySearchResults(message: any): void {
    if (!fuzzySearchView || editorMode !== 'fuzzy') return;

    // Update the fuzzy search view with results
    fuzzySearchView.updateResults(message);
}

/**
 * Cleanup editor and LSP client
 */
export function destroyEditor(): void {
    if (editorView) {
        editorView.destroy();
        editorView = null;
    }
    if (fuzzySearchView) {
        fuzzySearchView = null;
    }
    // LSP client is managed by languageServer() extension, no manual cleanup needed
}