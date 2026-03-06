/**
 * PTY Glyph Module — terminal on canvas via GlyphUI.
 *
 * Replaces legacy terminal.html. xterm.js is bundled by bun build
 * instead of loaded from CDN. Backend URL comes from GlyphUI
 * instead of being hardcoded.
 */

import type { Glyph, GlyphUI, RenderFn } from '@qntx/glyphs';
import { Terminal } from '@xterm/xterm';
import { FitAddon } from '@xterm/addon-fit';

export const render: RenderFn = async (glyph: Glyph, ui: GlyphUI): Promise<HTMLElement> => {
    const { element } = ui.container({
        defaults: {
            x: glyph.x ?? 200,
            y: glyph.y ?? 200,
            width: glyph.width ?? 800,
            height: glyph.height ?? 600,
        },
        titleBar: { label: 'Terminal' },
        resizable: { minWidth: 300, minHeight: 200 },
        className: 'canvas-pty-glyph',
    });

    const termDiv = document.createElement('div');
    termDiv.style.flex = '1';
    termDiv.style.overflow = 'hidden';
    element.appendChild(termDiv);

    // Tells the canvas click handler to skip focus theft and glyph selection
    ui.preventDrag(termDiv);

    const term = new Terminal({
        cursorBlink: true,
        fontSize: 13,
        fontFamily: 'Menlo, Monaco, "Courier New", monospace',
        theme: {
            background: '#1e1e23',
            foreground: '#d4d4d4',
            cursor: '#d4d4d4',
        },
    });

    const fitAddon = new FitAddon();
    term.loadAddon(fitAddon);

    // xterm.js needs the element in the document for keyboard event setup.
    // The caller mounts our element after we return, so defer open().
    let cancelled = false;
    ui.onCleanup(() => { cancelled = true; });

    requestAnimationFrame(() => {
        if (cancelled) return;

        term.open(termDiv);
        fitAddon.fit();

        termDiv.addEventListener('mousedown', () => term.focus());

        const resizeObserver = new ResizeObserver(() => fitAddon.fit());
        resizeObserver.observe(termDiv);

        // ws is populated by the async flow below; handlers registered
        // synchronously here close over this variable.
        let ws: WebSocket | null = null;

        // Keyboard input → WebSocket. Registered synchronously so xterm
        // binds the handler during the same tick as term.open().
        term.onData((data) => {
            if (ws?.readyState === WebSocket.OPEN) {
                ws.send(JSON.stringify({
                    type: 1,
                    data: btoa(data),
                    headers: {},
                    timestamp: 0,
                }));
            } else {
                // Self-diagnostic: shows in terminal so user sees what's wrong
                const state = ws ? `readyState=${ws.readyState}` : 'null';
                term.write(`\x1b[33m[ws: ${state}]\x1b[0m`);
            }
        });

        term.onResize(({ cols, rows }) => {
            if (ws?.readyState === WebSocket.OPEN) {
                ws.send(JSON.stringify({
                    type: 3,
                    data: '',
                    headers: { cols: String(cols), rows: String(rows) },
                    timestamp: 0,
                }));
            }
        });

        // Async flow: create PTY session, connect WebSocket
        (async () => {
            try {
                const resp = await ui.pluginFetch('/create', {
                    method: 'POST',
                    body: { glyph_id: glyph.id },
                });

                if (!resp.ok) {
                    const text = await resp.text();
                    term.write(`\x1b[31mFailed to create PTY session: ${resp.status} ${text}\x1b[0m\r\n`);
                    return;
                }

                const { pty_id } = await resp.json();
                ui.log.info(`PTY session created: ${pty_id}`);

                ws = ui.pluginWebSocket({ session_id: pty_id });

                ws.onopen = () => {
                    ui.log.debug('WebSocket connected');
                    ws!.send(JSON.stringify({
                        type: 3,
                        data: '',
                        headers: { cols: String(term.cols), rows: String(term.rows) },
                        timestamp: 0,
                    }));
                };

                ws.onmessage = (event) => {
                    try {
                        const msg = JSON.parse(event.data);
                        if (msg.type === 1 && msg.data) {
                            term.write(atob(msg.data));
                        }
                    } catch (e) {
                        ui.log.error('Failed to parse message', e);
                    }
                };

                ws.onerror = () => {
                    term.write('\r\n\x1b[31mWebSocket connection error\x1b[0m\r\n');
                };

                ws.onclose = () => {
                    term.write('\r\n\x1b[33mConnection closed\x1b[0m\r\n');
                };
            } catch (err) {
                const msg = err instanceof Error ? err.message : String(err);
                term.write(`\x1b[31mError: ${msg}\x1b[0m\r\n`);
                ui.log.error('PTY initialization failed', err);
            }
        })();

        ui.onCleanup(() => {
            resizeObserver.disconnect();
            if (ws && ws.readyState !== WebSocket.CLOSED) ws.close();
            term.dispose();
        });
    });

    return element;
};
