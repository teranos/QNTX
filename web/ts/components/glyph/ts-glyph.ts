/**
 * TypeScript Glyph - CodeMirror-based JS/TS editor on canvas
 *
 * Browser-native JavaScript execution via AsyncFunction constructor.
 * No server round-trip needed — scripts run directly in the browser
 * and can create attestations in local IndexedDB via the injected `qntx` API.
 *
 * SECURITY MODEL: User code runs with full browser privileges (same as devtools).
 * No sandbox, no CSP enforcement, no execution timeout. This is intentional —
 * the qntx API needs IndexedDB, network, and DOM access to function.
 * ts-glyph is a power-user tool, not a public-facing sandbox.
 */

import type { Glyph } from '@qntx/glyphs';
import { log, SEG } from '../../logger';
import { uiState } from '../../state/ui';
import { createAutoSave } from './glyph-autosave';
import { syncStateManager } from '../../state/sync-state';
import { connectivityManager } from '../../connectivity';
import { createGlyphUI } from './glyph-ui';
import { putAttestation, queryAttestations, parseQuery, generateASUID } from '../../qntx-wasm';
import type { Attestation } from '../../qntx-wasm';

export const TS_DEFAULT_CODE = `// Generate a random attestation — each run creates a unique ASUID
const subjects = ["alice", "bob", "charlie", "diana", "eve"]
const actions = ["discovered", "verified", "challenged", "confirmed", "witnessed"]
const domains = ["cryptography", "graph-theory", "distributed-systems", "formal-proofs", "zero-knowledge"]

const who = subjects[Math.floor(Math.random() * subjects.length)]
const did = actions[Math.floor(Math.random() * actions.length)]
const where = domains[Math.floor(Math.random() * domains.length)]
const confidence = Math.round(Math.random() * 100)

const result = await qntx.attest({
    subjects: [who],
    predicates: [did],
    contexts: [where],
    attributes: { confidence, note: who + " " + did + " something in " + where }
})
qntx.log(result.id)
qntx.log(who + " " + did + " [" + where + "] confidence=" + confidence + "%")
`;

/** AsyncFunction constructor — supports `await` in user code */
const AsyncFunction = Object.getPrototypeOf(async function () {}).constructor;

/**
 * Build the `qntx` API object injected into user scripts.
 *
 * Provides:
 *  - qntx.attest(opts) — create attestation in IndexedDB + enqueue sync
 *  - qntx.query(queryString) — parse AX query, run against local IndexedDB
 *  - qntx.log(...args) — append to output collector
 */
function buildQntxApi(outputLines: string[]) {
    return {
        /** Create an attestation in local IndexedDB */
        async attest(opts: {
            subjects: string[];
            predicates: string[];
            contexts?: string[];
            actors?: string[];
            attributes?: Record<string, unknown>;
        }): Promise<Attestation> {
            const now = Math.floor(Date.now() / 1000);
            const subjects = opts.subjects;
            const predicates = opts.predicates;
            const contexts = opts.contexts ?? ['_'];
            const actors = opts.actors ?? ['ts-glyph'];
            const { full: asuid } = generateASUID('AS', subjects[0] ?? '', predicates[0] ?? '', contexts[0] ?? '');
            const attestation: Attestation = {
                id: asuid,
                subjects,
                predicates,
                contexts,
                actors,
                timestamp: now,
                source: 'ts-glyph',
                attributes: opts.attributes ?? {},
                created_at: now,
                signature: '' as unknown as Uint8Array, // base64 string for proto JSON wire format
                signer_did: '',
            };

            await putAttestation(attestation);

            // Enqueue for server sync (lazy import to avoid circular deps)
            try {
                const { syncQueue } = await import('../../api/attestation-sync');
                syncQueue.add(attestation.id);
            } catch {
                // Sync module not loaded — attestation is still in IndexedDB
            }

            return attestation;
        },

        /** Query local IndexedDB attestations with an AX query string */
        async query(queryString: string): Promise<Attestation[]> {
            const result = parseQuery(queryString);
            if (!result.ok) {
                throw new Error(`Query parse error: ${result.error}`);
            }
            return queryAttestations(result.query);
        },

        /** Append to script output */
        log(...args: unknown[]): void {
            outputLines.push(args.map(a =>
                typeof a === 'object' ? JSON.stringify(a, null, 2) : String(a)
            ).join(' '));
        },
    };
}

/**
 * Create a TypeScript/JavaScript editor glyph with CodeMirror
 */
export async function createTsGlyph(glyph: Glyph): Promise<HTMLElement> {
    // Load code from canvas state or use default
    const existingGlyph = uiState.getCanvasGlyphs().find(g => g.id === glyph.id);
    const code = existingGlyph?.content ?? TS_DEFAULT_CODE;

    const lineCount = code.split('\n').length;
    const lineHeight = 24;
    const titleBarH = 36;
    const minHeight = 120;
    const maxHeight = 600;
    const calculatedHeight = Math.min(maxHeight, Math.max(minHeight, titleBarH + lineCount * lineHeight + 40));

    // Run button
    const runButton = document.createElement('button');
    runButton.textContent = '\u25B6';
    runButton.className = 'titlebar-btn';
    runButton.title = 'Run JavaScript code';

    // Orange tint — local-only glyph (ts-glyph always runs in-browser)
    if (!glyph.color) glyph.color = 'rgba(61, 45, 20, 0.92)';

    const ui = createGlyphUI(glyph, 'ts');
    const { element, content } = ui.glyph({
        defaults: { x: 200, y: 200, width: 400, height: calculatedHeight },
        titleBar: { label: 'ts', actions: [runButton], color: '#5c3d1a', labelColor: '#f0c878' },
        resizable: true,
        className: 'canvas-ts-glyph',
    });
    element.style.minWidth = '200px';
    element.style.minHeight = '120px';
    element.style.zIndex = '1';
    element.dataset.localActive = 'true';


    // Execute JavaScript on click
    runButton.addEventListener('click', async () => {
        const editor = (element as any).editor;
        if (!editor) {
            ui.log.error('Editor not initialized');
            return;
        }

        const currentCode = editor.state.doc.toString();
        const startTime = performance.now();
        const outputLines: string[] = [];
        const qntxApi = buildQntxApi(outputLines);

        try {
            const fn = new AsyncFunction('qntx', currentCode);
            const returnValue = await fn(qntxApi);

            const duration = Math.round(performance.now() - startTime);
            const stdout = outputLines.join('\n') +
                (returnValue !== undefined ? `\n${typeof returnValue === 'object' ? JSON.stringify(returnValue, null, 2) : String(returnValue)}` : '');

            ui.spawnResult({
                success: true,
                stdout: stdout.trim(),
                stderr: '',
                result: returnValue,
                error: null,
                duration_ms: duration,
            });
        } catch (error) {
            const duration = Math.round(performance.now() - startTime);
            ui.spawnResult({
                success: false,
                stdout: outputLines.join('\n'),
                stderr: '',
                result: null,
                error: error instanceof Error ? error.message : String(error),
                duration_ms: duration,
            });
        }
    });

    // Editor container
    const editorContainer = document.createElement('div');
    editorContainer.className = 'ts-glyph-editor';
    editorContainer.style.flex = '1';
    editorContainer.style.overflow = 'hidden';
    content.appendChild(editorContainer);

    // Initialize CodeMirror
    try {
        const { EditorView, keymap } = await import('@codemirror/view');
        const { EditorState } = await import('@codemirror/state');
        const { defaultKeymap } = await import('@codemirror/commands');
        const { oneDark } = await import('@codemirror/theme-one-dark');
        const { javascript } = await import('@codemirror/lang-javascript');

        const { save } = createAutoSave(glyph.id, () => editor.state.doc.toString(), 'TsGlyph');
        const autoSaveExtension = EditorView.updateListener.of((update) => {
            if (update.docChanged) save();
        });

        const editor = new EditorView({
            state: EditorState.create({
                doc: code,
                extensions: [
                    keymap.of(defaultKeymap),
                    javascript({ typescript: true }),
                    oneDark,
                    EditorView.lineWrapping,
                    autoSaveExtension,
                ],
            }),
            parent: editorContainer,
        });

        (element as any).editor = editor;

        if (!existingGlyph?.content) {
            const canvasGlyph = uiState.getCanvasGlyphs().find(g => g.id === glyph.id);
            if (canvasGlyph) {
                uiState.addCanvasGlyph({ ...canvasGlyph, content: code });
                log.debug(SEG.GLYPH, `[TsGlyph] Saved initial code for new glyph ${glyph.id}`);
            }
        }

        log.debug(SEG.GLYPH, `[TsGlyph] CodeMirror initialized for ${glyph.id}`);
    } catch (error) {
        log.error(SEG.GLYPH, `[TsGlyph] Failed to initialize CodeMirror:`, error);
        editorContainer.textContent = 'Error loading editor';
    }

    syncStateManager.subscribe(glyph.id, (state) => {
        element.dataset.syncState = state;
    });
    connectivityManager.subscribe((state) => {
        element.dataset.connectivityMode = state;
    });

    return element;
}

