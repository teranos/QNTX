/**
 * ix-net Glyph Module — Network Inspector status dashboard.
 *
 * Shows proxy status, capture counts, image counts, and token usage.
 * Refreshes every 5 seconds. Uses GlyphUI SDK for container, status line,
 * and plugin fetch.
 */

import type { Glyph, GlyphUI, GlyphDef, RenderFn } from '@qntx/glyphs';

export const glyphDef: GlyphDef = {
    symbol: '\u{1F50D}',
    title: 'Network Inspector',
    label: 'ix-net',
    defaultWidth: 320,
    defaultHeight: 280,
};

export const render: RenderFn = async (glyph: Glyph, ui: GlyphUI): Promise<HTMLElement> => {
    const { element } = ui.container({
        defaults: {
            x: glyph.x ?? 100,
            y: glyph.y ?? 100,
            width: 320,
            height: 280,
        },
        titleBar: { label: 'ix-net' },
        resizable: true,
    });

    const body = document.createElement('div');
    body.style.flex = '1';
    body.style.overflow = 'auto';
    body.style.padding = '12px';
    body.style.fontFamily = 'monospace';
    body.style.fontSize = '13px';
    element.appendChild(body);

    const status = ui.statusLine();
    element.appendChild(status.element);

    function row(parent: HTMLElement, label: string, value: string): void {
        const el = document.createElement('div');
        el.style.display = 'flex';
        el.style.justifyContent = 'space-between';
        el.style.padding = '2px 0';
        const lbl = document.createElement('span');
        lbl.style.color = 'var(--muted-foreground, #888)';
        lbl.textContent = label;
        const val = document.createElement('span');
        val.textContent = value;
        el.appendChild(lbl);
        el.appendChild(val);
        parent.appendChild(el);
    }

    async function refresh(): Promise<void> {
        try {
            const resp = await ui.pluginFetch('/captures');
            const data = await resp.json();
            const caps: Array<{
                has_images: boolean;
                image_count: number;
                input_tokens: number;
                output_tokens: number;
                model: string;
                status_code: number;
            }> = data.captures || [];
            const total: number = data.total || 0;
            const withImages = caps.filter(c => c.has_images).length;
            const totalImages = caps.reduce((n, c) => n + c.image_count, 0);
            const totalIn = caps.reduce((n, c) => n + c.input_tokens, 0);
            const totalOut = caps.reduce((n, c) => n + c.output_tokens, 0);

            body.innerHTML = '';
            row(body, 'Proxy', 'listening');
            row(body, 'Captures', String(total));
            row(body, 'With images', String(withImages));
            row(body, 'Total images', String(totalImages));
            row(body, 'Input tokens', totalIn.toLocaleString());
            row(body, 'Output tokens', totalOut.toLocaleString());

            if (caps.length > 0) {
                const last = caps[caps.length - 1];
                row(body, 'Last model', last.model);
                row(body, 'Last status', String(last.status_code));
            }

            status.clear();
        } catch {
            body.innerHTML = '';
            row(body, 'Proxy', 'not reachable');
            status.show('fetch failed', true);
        }
    }

    await refresh();
    const interval = setInterval(refresh, 5000);
    ui.onCleanup(() => clearInterval(interval));

    return element;
};
