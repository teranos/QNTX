/**
 * ix-json Glyph Module — pure TypeScript plugin glyph using the GlyphUI.
 *
 * No Go backend required. Config is persisted via ui.saveConfig/loadConfig,
 * API fetches happen directly from the browser (CORS-permitting).
 *
 * Dynamically imported by the QNTX frontend when registered in glyph-registry.
 */

import type { Glyph, GlyphUI, GlyphDef, RenderFn, MeldEvent } from '@qntx/glyphs';

export const glyphDef: GlyphDef = {
    symbol: '🔄',
    title: 'JSON API Ingestor',
    label: 'ix-json',
    defaultWidth: 600,
    defaultHeight: 700,
};

export const render: RenderFn = async (glyph: Glyph, ui: GlyphUI): Promise<HTMLElement> => {
    const { element, titleBar } = ui.container({
        defaults: {
            x: glyph.x ?? 200,
            y: glyph.y ?? 200,
            width: 600,
            height: 700,
        },
        titleBar: { label: 'JSON API Ingestor' },
        resizable: true,
    });

    // Content wrapper
    const content = document.createElement('div');
    content.style.flex = '1';
    content.style.overflow = 'auto';
    content.style.padding = '8px';
    content.style.display = 'flex';
    content.style.flexDirection = 'column';
    content.style.gap = '8px';
    content.style.fontFamily = 'monospace';
    content.style.fontSize = '12px';

    // ── Status line ──────────────────────────────────────────────
    const status = ui.statusLine();

    // ── Configuration section ────────────────────────────────────
    const configSection = section('Configuration');

    const apiUrlInput = ui.input({ label: 'API URL', placeholder: 'https://api.example.com/data' });
    const authTokenInput = ui.input({ label: 'Auth Token (optional)', placeholder: 'Bearer token', type: 'password' });

    const btnRow = document.createElement('div');
    btnRow.style.display = 'flex';
    btnRow.style.gap = '4px';
    btnRow.style.marginTop = '2px';

    const saveBtn = ui.button({
        label: 'Save Config',
        onClick: () => saveConfig(ui, apiUrlInput, authTokenInput, status),
    });

    const fetchBtn = ui.button({
        label: 'Test Fetch',
        onClick: () => testFetch(apiUrlInput, authTokenInput, responsePreview, status),
    });

    btnRow.appendChild(saveBtn);
    btnRow.appendChild(fetchBtn);

    configSection.appendChild(apiUrlInput);
    configSection.appendChild(authTokenInput);
    configSection.appendChild(btnRow);
    configSection.appendChild(status.element);

    // ── Response preview ─────────────────────────────────────────
    const responseSection = section('API Response Preview');
    const responsePreview = document.createElement('pre');
    responsePreview.style.background = 'var(--card-bg, #f9f9f9)';
    responsePreview.style.border = '1px solid var(--border-color, #e0e0e0)';
    responsePreview.style.borderRadius = '3px';
    responsePreview.style.padding = '6px';
    responsePreview.style.fontSize = '11px';
    responsePreview.style.fontFamily = 'monospace';
    responsePreview.style.overflowX = 'auto';
    responsePreview.style.maxHeight = '200px';
    responsePreview.style.overflowY = 'auto';
    responsePreview.style.whiteSpace = 'pre-wrap';
    responsePreview.style.wordBreak = 'break-word';
    responsePreview.style.overflowWrap = 'break-word';
    responsePreview.textContent = '(no data yet — click Test Fetch)';
    ui.preventDrag(responsePreview);
    responseSection.appendChild(responsePreview);

    // Assemble
    content.appendChild(configSection);
    content.appendChild(responseSection);
    element.appendChild(content);

    // Hydrate inputs from saved config
    const config = await ui.loadConfig();
    if (config) {
        setInputValue(apiUrlInput, (config.api_url as string) || '');
        setInputValue(authTokenInput, (config.auth_token as string) || '');
    }

    // React to melds — when a note/URL glyph melds from above, extract URL and use as API URL
    ui.onMeld((event: MeldEvent) => {
        const url = extractUrl(event.content);
        if (url) {
            setInputValue(apiUrlInput, url);
            saveConfig(ui, apiUrlInput, authTokenInput, status).then(() => {
                status.show(`URL received from melded glyph`);
            });
        }
    });

    return element;
};

// ── Helpers ──────────────────────────────────────────────────────

interface StatusLine {
    element: HTMLElement;
    show(msg: string, isError?: boolean): void;
    clear(): void;
}

function section(title: string): HTMLDivElement {
    const el = document.createElement('div');
    el.style.display = 'flex';
    el.style.flexDirection = 'column';
    el.style.gap = '4px';

    const h = document.createElement('h3');
    h.textContent = title;
    h.style.fontSize = '11px';
    h.style.fontWeight = '600';
    h.style.margin = '0';
    h.style.color = 'var(--muted-foreground, #666)';
    h.style.textTransform = 'uppercase';
    h.style.letterSpacing = '0.5px';
    el.appendChild(h);

    return el;
}

function getInputValue(wrapper: HTMLElement): string {
    const input = wrapper.querySelector('input');
    return input ? input.value : '';
}

/** Extract the first http/https URL from text that may contain markdown or other content. */
function extractUrl(text: string): string | null {
    const lines = text.split('\n');
    for (const line of lines) {
        // Split on whitespace without regex
        const tokens = line.trim().split(' ').flatMap(t => t.split('\t'));
        for (const token of tokens) {
            if (!token) continue;
            // Find http:// or https:// anywhere in the token
            let idx = token.indexOf('https://');
            if (idx === -1) idx = token.indexOf('http://');
            if (idx === -1) continue;

            // Take from the protocol start, strip trailing punctuation
            let url = token.slice(idx);
            const trailingChars = ')>].,;!';
            while (url.length > 0 && trailingChars.includes(url[url.length - 1])) {
                url = url.slice(0, -1);
            }
            return url;
        }
    }
    return null;
}

function setInputValue(wrapper: HTMLElement, value: string): void {
    const input = wrapper.querySelector('input');
    if (input) input.value = value;
}

async function saveConfig(
    ui: GlyphUI,
    apiUrlEl: HTMLElement,
    authTokenEl: HTMLElement,
    status: StatusLine,
): Promise<void> {
    try {
        await ui.saveConfig({
            api_url: getInputValue(apiUrlEl),
            auth_token: getInputValue(authTokenEl),
        });
        status.show('Configuration saved');
    } catch (e) {
        status.show((e as Error).message, true);
    }
}

async function testFetch(
    apiUrlEl: HTMLElement,
    authTokenEl: HTMLElement,
    preview: HTMLPreElement,
    status: StatusLine,
): Promise<void> {
    const apiUrl = getInputValue(apiUrlEl);
    if (!apiUrl) {
        status.show('API URL is required', true);
        return;
    }

    status.show('Fetching...');
    try {
        const headers: Record<string, string> = {};
        const authToken = getInputValue(authTokenEl);
        if (authToken) {
            headers['Authorization'] = authToken;
        }

        const resp = await fetch(apiUrl, { headers });
        if (resp.ok) {
            const data = await resp.json();
            preview.textContent = JSON.stringify(data, null, 2);
            status.show('Fetch successful');
        } else {
            status.show(`HTTP ${resp.status}: ${resp.statusText}`, true);
        }
    } catch (e) {
        status.show((e as Error).message, true);
    }
}
