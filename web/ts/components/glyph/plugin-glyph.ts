/**
 * Plugin Glyph Renderer — generic renderer for all plugin-provided glyphs.
 *
 * Fetches HTML content from plugin endpoints and mounts in canvas-placed wrapper.
 * Handles retry logic for 503 (plugin unavailable) and error states.
 */

import type { Glyph } from './glyph';
import type { PluginGlyphDef } from './plugin-glyphs';
import { canvasPlaced } from './manifestations/canvas-placed';
import { loadPluginCSS } from './plugin-glyphs';
import { apiFetch } from '../../api';
import { log, SEG } from '../../logger';

/** Create a plugin glyph element */
export async function createPluginGlyph(
    glyph: Glyph,
    def: PluginGlyphDef
): Promise<HTMLElement> {
    // Load CSS if provided (cached globally)
    if (def.css_url) {
        loadPluginCSS(def.css_url);
    }

    // Create canvas-placed wrapper
    const { element } = canvasPlaced({
        glyph,
        className: `canvas-plugin-glyph plugin-${def.plugin}`,
        defaults: {
            x: glyph.x ?? 200,
            y: glyph.y ?? 200,
            width: def.default_width ?? 400,
            height: def.default_height ?? 300,
        },
        titleBar: {
            label: def.title,
            actions: [],
        },
        resizable: true,
        logLabel: 'PluginGlyph',
    });

    // Content container
    const content = document.createElement('div');
    content.className = 'plugin-glyph-content';
    content.style.flex = '1';
    content.style.overflow = 'auto';
    content.style.padding = '8px';
    element.appendChild(content);

    // Fetch and render plugin content
    await fetchPluginContent(content, def.content_url, glyph.id, glyph.content ?? '');

    return element;
}

/** Fetch HTML from plugin endpoint and mount in container */
async function fetchPluginContent(
    container: HTMLElement,
    baseURL: string,
    glyphId: string,
    content: string,
    retryCount = 0
): Promise<void> {
    const maxRetries = 3;

    // Show loading state
    container.innerHTML = '<div class="plugin-loading">Loading...</div>';

    // Build URL with query params
    const params = new URLSearchParams({
        glyph_id: glyphId,
        content: content,
    });
    const url = `${baseURL}?${params}`;

    try {
        const resp = await apiFetch(url);

        // Handle plugin unavailable (503)
        if (resp.status === 503) {
            const retryAfter = parseInt(resp.headers.get('Retry-After') ?? '5', 10);
            const delay = Math.min(retryAfter * 1000, 10000);

            container.innerHTML = `
                <div class="plugin-error">
                    <p>Plugin temporarily unavailable</p>
                    <p>Retrying in ${retryAfter}s... (${retryCount + 1}/${maxRetries})</p>
                </div>
            `;

            if (retryCount < maxRetries) {
                setTimeout(() => {
                    void fetchPluginContent(container, baseURL, glyphId, content, retryCount + 1);
                }, delay);
            } else {
                container.innerHTML = `
                    <div class="plugin-error">
                        <p>Plugin unavailable after ${maxRetries} retries</p>
                        <button onclick="location.reload()">Reload Page</button>
                    </div>
                `;
            }
            return;
        }

        if (!resp.ok) {
            throw new Error(`HTTP ${resp.status}: ${resp.statusText}`);
        }

        // Render HTML
        const html = await resp.text();
        container.innerHTML = html;

    } catch (err) {
        const message = err instanceof Error ? err.message : String(err);
        log.error(SEG.GLYPH, '[PluginGlyph] Fetch error:', err);
        container.innerHTML = `
            <div class="plugin-error">
                <p>Failed to load plugin content</p>
                <p>${message}</p>
                <button onclick="this.parentElement.parentElement.remove()">Remove Glyph</button>
            </div>
        `;
    }
}
