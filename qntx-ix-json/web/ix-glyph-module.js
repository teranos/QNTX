/**
 * ix-json Glyph Module — TypeScript-first plugin glyph using the PluginGlyphSDK.
 *
 * This replaces the server-rendered HTML pipeline (renderIXGlyphHTML in handlers.go).
 * The plugin's Go process still handles API logic (test-fetch, update-config, set-mode).
 * This module handles all rendering and user interaction.
 *
 * Served by the Go plugin at GET /ix-glyph-module.js
 * Dynamically imported by the QNTX frontend when module_url is set in GlyphDef.
 */

export async function render(glyph, sdk) {
    const { element, titleBar } = sdk.container({
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
    const status = sdk.statusLine();

    // ── Configuration section ────────────────────────────────────
    const configSection = section('Configuration');

    const apiUrlInput = sdk.input({ label: 'API URL', placeholder: 'https://api.example.com/data' });
    const authTokenInput = sdk.input({ label: 'Auth Token (optional)', placeholder: 'Bearer token', type: 'password' });
    const pollIntervalInput = sdk.input({ label: 'Poll Interval (seconds, 0 = manual only)', value: '0', type: 'number' });

    const btnRow = document.createElement('div');
    btnRow.style.display = 'flex';
    btnRow.style.gap = '4px';
    btnRow.style.marginTop = '2px';

    const saveBtn = sdk.button({
        label: 'Save Config',
        onClick: () => saveConfig(sdk, glyph.id, apiUrlInput, authTokenInput, pollIntervalInput, status),
    });

    const fetchBtn = sdk.button({
        label: 'Test Fetch',
        onClick: () => testFetch(sdk, glyph.id, apiUrlInput, authTokenInput, responsePreview, status),
    });

    btnRow.appendChild(saveBtn);
    btnRow.appendChild(fetchBtn);

    configSection.appendChild(apiUrlInput);
    configSection.appendChild(authTokenInput);
    configSection.appendChild(pollIntervalInput);
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
    sdk.preventDrag(responsePreview);
    responseSection.appendChild(responsePreview);

    // ── Mode controls ────────────────────────────────────────────
    const modeSection = section('Mode Controls');
    const modeRow = document.createElement('div');
    modeRow.style.display = 'flex';
    modeRow.style.gap = '4px';

    const pauseBtn = sdk.button({
        label: 'Pause',
        onClick: () => setMode(sdk, glyph.id, 'paused', apiUrlInput, authTokenInput, pollIntervalInput, status),
    });
    const activateBtn = sdk.button({
        label: 'Activate',
        primary: true,
        onClick: () => setMode(sdk, glyph.id, 'active-running', apiUrlInput, authTokenInput, pollIntervalInput, status),
    });
    modeRow.appendChild(pauseBtn);
    modeRow.appendChild(activateBtn);
    modeSection.appendChild(modeRow);

    // Assemble
    content.appendChild(configSection);
    content.appendChild(responseSection);
    content.appendChild(modeSection);
    element.appendChild(content);

    // Hydrate inputs from saved config
    const config = await sdk.loadConfig();
    if (config) {
        setInputValue(apiUrlInput, config.api_url || '');
        setInputValue(authTokenInput, config.auth_token || '');
        setInputValue(pollIntervalInput, String(config.poll_interval_seconds || 0));
    }

    return element;
}

// ── Helpers ──────────────────────────────────────────────────────

function section(title) {
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

function getInputValue(wrapper) {
    const input = wrapper.querySelector('input');
    return input ? input.value : '';
}

function setInputValue(wrapper, value) {
    const input = wrapper.querySelector('input');
    if (input) input.value = value;
}

async function saveConfig(sdk, glyphId, apiUrlEl, authTokenEl, pollIntervalEl, status) {
    try {
        const resp = await sdk.pluginFetch('/update-config', {
            method: 'POST',
            body: {
                glyph_id: glyphId,
                api_url: getInputValue(apiUrlEl),
                auth_token: getInputValue(authTokenEl),
                poll_interval_seconds: parseInt(getInputValue(pollIntervalEl)) || 0,
            },
        });
        if (resp.ok) {
            status.show('Configuration saved');
        } else {
            const body = await resp.json().catch(() => ({}));
            status.show(body.error || 'Save failed', true);
        }
    } catch (e) {
        status.show(e.message, true);
    }
}

async function testFetch(sdk, glyphId, apiUrlEl, authTokenEl, preview, status) {
    const apiUrl = getInputValue(apiUrlEl);
    if (!apiUrl) {
        status.show('API URL is required', true);
        return;
    }

    status.show('Fetching...');
    try {
        const resp = await sdk.pluginFetch('/test-fetch', {
            method: 'POST',
            body: {
                glyph_id: glyphId,
                api_url: apiUrl,
                auth_token: getInputValue(authTokenEl),
            },
        });
        if (resp.ok) {
            const body = await resp.json();
            preview.textContent = JSON.stringify(body.data, null, 2);
            status.show('Fetch successful');
        } else {
            const body = await resp.json().catch(() => ({}));
            status.show(body.error || 'Fetch failed', true);
        }
    } catch (e) {
        status.show(e.message, true);
    }
}

async function setMode(sdk, glyphId, mode, apiUrlEl, authTokenEl, pollIntervalEl, status) {
    try {
        const resp = await sdk.pluginFetch('/set-mode', {
            method: 'POST',
            body: {
                glyph_id: glyphId,
                mode: mode,
                api_url: getInputValue(apiUrlEl),
                auth_token: getInputValue(authTokenEl),
                poll_interval_seconds: parseInt(getInputValue(pollIntervalEl)) || 0,
            },
        });
        const body = await resp.json().catch(() => ({}));
        if (resp.ok) {
            status.show(body.status || 'Mode: ' + mode);
        } else {
            status.show(body.error || 'Failed to set mode', true);
        }
    } catch (e) {
        status.show(e.message, true);
    }
}

