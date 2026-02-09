/**
 * TypeScript Glyph - CodeMirror-based JS/TS editor on canvas
 *
 * Browser-native JavaScript execution via AsyncFunction constructor.
 * No server round-trip needed — scripts run directly in the browser
 * and can create attestations in local IndexedDB via the injected `qntx` API.
 */

import type { Glyph } from './glyph';
import { log, SEG } from '../../logger';
import { uiState } from '../../state/ui';
import { getScriptStorage } from '../../storage/script-storage';
import { createResultGlyph, type ExecutionResult } from './result-glyph';
import { autoMeldResultBelow } from './meld/meld-system';
import { syncStateManager } from '../../state/sync-state';
import { connectivityManager } from '../../connectivity';
import { canvasPlaced } from './manifestations/canvas-placed';
import { putAttestation, queryAttestations, parseQuery } from '../../qntx-wasm';
import type { Attestation } from '../../qntx-wasm';

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
            const attestation: Attestation = {
                id: `AS-${crypto.randomUUID()}`,
                subjects: opts.subjects,
                predicates: opts.predicates,
                contexts: opts.contexts ?? ['_'],
                actors: opts.actors ?? ['ts-glyph'],
                timestamp: now,
                source: 'ts-glyph',
                attributes: opts.attributes ? JSON.stringify(opts.attributes) : '{}',
                created_at: now,
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
    const storage = getScriptStorage();
    const defaultCode = '// TypeScript editor\nqntx.log("Hello from canvas!")\n';
    const savedCode = await storage.load(glyph.id);
    const code = savedCode ?? defaultCode;

    const lineCount = code.split('\n').length;
    const lineHeight = 24;
    const titleBarH = 36;
    const minHeight = 120;
    const maxHeight = 600;
    const calculatedHeight = Math.min(maxHeight, Math.max(minHeight, titleBarH + lineCount * lineHeight + 40));

    // Run button
    const runButton = document.createElement('button');
    runButton.textContent = '\u25B6';
    runButton.className = 'glyph-play-btn';
    runButton.title = 'Run JavaScript code';

    const { element } = canvasPlaced({
        glyph,
        className: 'canvas-ts-glyph',
        defaults: { x: 200, y: 200, width: 400, height: calculatedHeight },
        titleBar: { label: 'ts', actions: [runButton] },
        resizable: true,
        logLabel: 'TsGlyph',
    });
    element.style.minWidth = '200px';
    element.style.minHeight = '120px';
    element.style.zIndex = '1';

    // Orange = local-only glyph (ts-glyph always runs in-browser)
    element.style.backgroundColor = 'rgba(61, 45, 20, 0.92)';
    const titleBar = element.querySelector('.canvas-glyph-title-bar') as HTMLElement;
    if (titleBar) {
        titleBar.style.backgroundColor = '#5c3d1a';
        const labelSpan = titleBar.querySelector('span:first-child') as HTMLElement;
        if (labelSpan) {
            labelSpan.style.color = '#f0c878';
            labelSpan.style.fontWeight = 'bold';
            labelSpan.style.flex = '1';
        }
    }

    // Execute JavaScript on click
    runButton.addEventListener('click', async () => {
        const editor = (element as any).editor;
        if (!editor) {
            log.error(SEG.GLYPH, '[TsGlyph] Editor not initialized');
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

            const result: ExecutionResult = {
                success: true,
                stdout: stdout.trim(),
                stderr: '',
                result: returnValue,
                error: null,
                duration_ms: duration,
            };
            createAndDisplayResultGlyph(element, glyph, result);
        } catch (error) {
            const duration = Math.round(performance.now() - startTime);
            const result: ExecutionResult = {
                success: false,
                stdout: outputLines.join('\n'),
                stderr: '',
                result: null,
                error: error instanceof Error ? error.message : String(error),
                duration_ms: duration,
            };
            createAndDisplayResultGlyph(element, glyph, result);
        }
    });

    // Editor container
    const editorContainer = document.createElement('div');
    editorContainer.className = 'ts-glyph-editor';
    editorContainer.style.flex = '1';
    editorContainer.style.overflow = 'hidden';
    element.appendChild(editorContainer);

    // Initialize CodeMirror
    try {
        const { EditorView, keymap } = await import('@codemirror/view');
        const { EditorState } = await import('@codemirror/state');
        const { defaultKeymap } = await import('@codemirror/commands');
        const { oneDark } = await import('@codemirror/theme-one-dark');
        const { javascript } = await import('@codemirror/lang-javascript');

        let saveTimeout: number | undefined;
        const autoSaveExtension = EditorView.updateListener.of((update) => {
            if (update.docChanged) {
                if (saveTimeout !== undefined) clearTimeout(saveTimeout);
                saveTimeout = window.setTimeout(async () => {
                    const currentCode = update.state.doc.toString();
                    await storage.save(glyph.id, currentCode);
                    log.debug(SEG.GLYPH, `[TsGlyph] Auto-saved code for ${glyph.id}`);
                }, 500);
            }
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

        if (!savedCode) {
            await storage.save(glyph.id, code);
            log.debug(SEG.GLYPH, `[TsGlyph] Saved initial code for new glyph ${glyph.id}`);
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

function createAndDisplayResultGlyph(tsElement: HTMLElement, parentGlyph: Glyph, result: ExecutionResult): void {
    const tsRect = tsElement.getBoundingClientRect();
    const canvas = tsElement.closest('.canvas-workspace') as HTMLElement;
    if (!canvas) {
        log.error(SEG.GLYPH, '[TsGlyph] Cannot spawn result glyph: no canvas-workspace ancestor');
        return;
    }
    const canvasRect = canvas.getBoundingClientRect();

    const x = tsRect.left - canvasRect.left;
    const y = tsRect.bottom - canvasRect.top;

    const resultGlyphId = `result-${crypto.randomUUID()}`;
    const resultGlyph: Glyph = {
        id: resultGlyphId,
        title: 'TS Result',
        symbol: 'result',
        x,
        y,
        width: Math.round(tsRect.width),
        renderContent: () => document.createElement('div'),
    };

    const resultElement = createResultGlyph(resultGlyph, result);
    canvas.appendChild(resultElement);

    const resultRect = resultElement.getBoundingClientRect();
    uiState.addCanvasGlyph({
        id: resultGlyphId,
        symbol: 'result',
        x,
        y,
        width: Math.round(resultRect.width),
        height: Math.round(resultRect.height),
        result,
    });

    const tsGlyphId = tsElement.dataset.glyphId;
    if (tsGlyphId) {
        autoMeldResultBelow(tsElement, tsGlyphId, 'ts', 'TypeScript', resultElement, resultGlyphId, 'TsGlyph');
    }
}
