/**
 * CodeMirror 6 Editor with LSP Integration
 *
 * Uses pre-built bundle from js/vendor/codemirror-bundle.js
 * See internal/server/web/README.md for architecture details
 */

// Import from vendored bundle (like d3.v7.min.js pattern)
import {
    EditorView,
    keymap,
    lineNumbers,
    highlightActiveLine,
    highlightActiveLineGutter,
    EditorState,
    Compartment,
    StateField,
    StateEffect,
    RangeSetBuilder,
    Decoration,
    ViewPlugin,
    defaultKeymap,
    history,
    historyKeymap,
    indentWithTab,
    syntaxHighlighting,
    defaultHighlightStyle,
    bracketMatching,
    autocompletion,
    completionKeymap,
    closeBrackets,
    lintKeymap,
    languageServer
} from './vendor/codemirror-bundle.js';

// Linter - TODO: enable once we have a proper way to import this
// import { linter } from '@codemirror/lint';

// DISABLED: LSP WebSocket transport conflicts with main WebSocket
// import { createLSPClient } from './lsp-websocket-transport.js';
import { sendMessage, validateBackendURL } from './websocket.ts';
import { requestParse } from './ats-semantic-tokens-client.ts';
import type { Diagnostic, SemanticToken } from '../types/lsp';

let editorView: EditorView | null = null;
let queryTimeout: ReturnType<typeof setTimeout> | null = null;
let parseTimeout: ReturnType<typeof setTimeout> | null = null;
let currentDiagnostics: Diagnostic[] = [];

// Syntax highlighting via LSP semantic tokens
// TODO(issue #132): codemirror-languageserver doesn't support semantic tokens yet (v1.18.1)
// For now, we manually request them from LSP server

const updateSyntaxDecorations = StateEffect.define();

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

// CodeMirror diagnostic type
interface CodeMirrorDiagnostic {
    from: number;
    to: number;
    severity: 'error' | 'warning' | 'info';
    message: string;
}

/**
 * Create a linter function that converts ATS diagnostics to CodeMirror format
 * Called whenever the editor content changes to provide diagnostics to CM6 linter
 */
function createAtsLinter(): () => CodeMirrorDiagnostic[] {
    return (): CodeMirrorDiagnostic[] => {
        // Return diagnostics in CodeMirror format
        return currentDiagnostics.map((diag: Diagnostic): CodeMirrorDiagnostic => {
            // Map severity string to CodeMirror severity level
            let severity: 'error' | 'warning' | 'info' = 'error';
            if (diag.severity === 'warning') {
                severity = 'warning';
            } else if (diag.severity === 'info' || diag.severity === 'hint') {
                severity = 'info';
            }

            // Build message with suggestions if available
            let message = diag.message || 'Parse error';
            if (diag.suggestions && diag.suggestions.length > 0) {
                message += '\n\nSuggestions:\n• ' + diag.suggestions.join('\n• ');
            }

            return {
                from: diag.range.start.offset,
                to: diag.range.end.offset,
                severity: severity,
                message: message
            };
        });
    };
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
    const serverUri = `${protocol}//${backendHost}/lsp`;

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
            languageServer({
                serverUri: serverUri,
                rootUri: 'file:///',
                documentUri: 'inmemory://ats-query',
                languageId: 'ats'
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
    if (parseTimeout) {
        clearTimeout(parseTimeout);
    }
    parseTimeout = setTimeout(() => {
        if (editorView) {
            const cursorPos = editorView.state.selection.main.head;
            requestParse(doc, 1, cursorPos);
        }
    }, 150); // 150ms debounce for syntax highlighting

    // Execute query with debounce (like old textarea editor)
    if (queryTimeout) {
        clearTimeout(queryTimeout);
    }
    queryTimeout = setTimeout(() => {
        sendMultiLineQueries(doc);
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
 * Update diagnostics and trigger linter re-evaluation
 * Called from parse response handler to display inline errors/warnings
 */
export function updateDiagnosticsDisplay(diagnostics: Diagnostic[]): void {
    if (!editorView) return;

    // Store diagnostics for linter to use
    currentDiagnostics = diagnostics || [];

    // Force linter re-evaluation by dispatching an empty transaction
    // This causes the linter to be re-run and display updated diagnostics
    editorView.dispatch({});
}

/**
 * Apply syntax highlighting from parse response tokens
 * TODO(issue #132): Request semantic tokens from LSP instead of parse_response
 * For now using old parse_response until codemirror-languageserver adds semantic token support
 */
export function applySyntaxHighlighting(tokens: SemanticToken[]): void {
    if (!editorView || !tokens || tokens.length === 0) return;

    const builder = new RangeSetBuilder();

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
 * Cleanup editor and LSP client
 */
export function destroyEditor(): void {
    if (editorView) {
        editorView.destroy();
        editorView = null;
    }
    // LSP client is managed by languageServer() extension, no manual cleanup needed
}