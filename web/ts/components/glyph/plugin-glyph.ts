/**
 * Plugin Glyph Renderer — generic renderer for all plugin-provided glyphs.
 *
 * Fetches HTML content from plugin endpoints and mounts in canvas-placed wrapper.
 * Handles retry logic for 503 (plugin unavailable) and error states.
 */

import type { Glyph } from '@qntx/glyphs';
import type { PluginGlyphDef } from './plugin-provided-glyphs';
import { canvasPlaced } from '@qntx/glyphs';
import { loadPluginCSS } from './plugin-provided-glyphs';
import { apiFetch } from '../../api';
import { log, SEG } from '../../logger';
import { connectivityManager } from '../../connectivity';
import { el } from '../../html-utils';

/** Create a plugin glyph element */
export async function createPluginGlyph(
    glyph: Glyph,
    def: PluginGlyphDef
): Promise<HTMLElement> {
    // Load CSS if provided (cached globally)
    if (def.css_url) {
        loadPluginCSS(def.css_url);
    }

    // Create canvas-placed wrapper without title bar
    const { element } = canvasPlaced({
        glyph,
        className: `canvas-plugin-glyph plugin-${def.plugin}`,
        defaults: {
            x: glyph.x ?? 200,
            y: glyph.y ?? 200,
            width: def.default_width ?? 400,
            height: def.default_height ?? 300,
        },
        resizable: true,
        logLabel: 'PluginGlyph',
    });

    // Custom title bar
    const symbol = el('span', {
        text: def.symbol,
        style: { fontWeight: 'bold', flexShrink: '0', color: '#adbcc1' },
    });
    const titleText = el('span', {
        text: def.title,
        style: { fontSize: '12px', fontFamily: 'monospace', lineHeight: '1.4', color: '#d7dee3' },
    });
    const titleBar = el('div', { class: 'glyph-title-bar glyph-title-bar--auto' }, [symbol, titleText]);
    element.appendChild(titleBar);

    // Content container
    const content = el('div', { class: 'plugin-glyph-content glyph-content-area' });
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

        // Execute scripts — innerHTML doesn't execute them for security.
        // External scripts load via document.head (detached trees don't trigger loads).
        // Inline scripts run after externals complete (they may depend on globals like Terminal).
        const scripts = container.querySelectorAll('script');
        const externalLoads: Promise<void>[] = [];
        const inlineScripts: HTMLScriptElement[] = [];

        scripts.forEach((oldScript) => {
            if (oldScript.src) {
                const newScript = el('script');
                const loaded = new Promise<void>((resolve, reject) => {
                    newScript.onload = () => resolve();
                    newScript.onerror = () => reject(new Error(`Failed to load script: ${oldScript.src}`));
                });
                newScript.src = oldScript.src;
                document.head.appendChild(newScript);
                oldScript.remove();
                externalLoads.push(loaded);
            } else {
                inlineScripts.push(oldScript);
            }
        });

        if (externalLoads.length > 0) {
            await Promise.all(externalLoads);
        }
        for (const oldScript of inlineScripts) {
            const newScript = el('script');
            newScript.textContent = oldScript.textContent;
            oldScript.parentNode?.replaceChild(newScript, oldScript);
        }

        // Prevent drag on interactive elements so they receive mouse events
        // without triggering glyph drag. Runs AFTER scripts execute so
        // dynamically created elements (e.g. xterm.js textarea) are included.
        const interactiveElements = container.querySelectorAll(
            'input, textarea, button, select, canvas, [contenteditable="true"], .xterm'
        );
        interactiveElements.forEach((el) => {
            el.addEventListener('mousedown', (e) => {
                e.stopPropagation();
            });
        });

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

/**
 * Create a placeholder glyph for unavailable plugin
 *
 * Similar to semantic search degraded state - shows plugin name and instructions.
 * Not alarming (no red error styling), just a muted placeholder.
 */
export function createPluginPlaceholderGlyph(
    glyph: Glyph,
    pluginName: string
): HTMLElement {
    const isOffline = connectivityManager.state === 'offline';

    // Create canvas-placed wrapper without title bar
    const { element } = canvasPlaced({
        glyph,
        className: `canvas-plugin-placeholder plugin-${pluginName}`,
        defaults: {
            x: glyph.x ?? 200,
            y: glyph.y ?? 200,
            width: glyph.width ?? 400,
            height: glyph.height ?? 300,
        },
        resizable: false,
        logLabel: 'PluginPlaceholder',
    });

    element.style.border = '1px solid var(--border)';
    element.style.pointerEvents = 'auto'; // Allow dragging

    // Custom title bar (same style as working plugin glyph)
    const symbol = el('span', {
        text: glyph.symbol ?? '?',
        style: { fontWeight: 'bold', flexShrink: '0', color: '#666', opacity: '0.5' },
    });
    const titleText = el('span', {
        text: `${pluginName} (unavailable)`,
        style: { fontSize: '12px', fontFamily: 'monospace', lineHeight: '1.4', color: '#999' },
    });
    const titleBar = el('div', { class: 'glyph-title-bar glyph-title-bar--auto' }, [symbol, titleText]);
    element.appendChild(titleBar);

    // Content area with instructions (like SE glyph degraded state)
    const content = el('div', {
        class: 'plugin-placeholder-content',
        style: {
            flex: '1', padding: '16px', color: 'var(--text-secondary)',
            fontSize: '12px', fontFamily: 'monospace', textAlign: 'center',
            lineHeight: '1.6', display: 'flex', flexDirection: 'column',
            justifyContent: 'center', alignItems: 'center', gap: '12px',
        },
    });

    if (isOffline) {
        content.innerHTML = `
            <div>Plugin requires server connection</div>
            <div style="font-size: 11px; color: #666;">
                Plugin glyphs are only available when online
            </div>
        `;
    } else {
        const displayName = pluginName === 'unknown' ? 'This plugin' : `Plugin "${pluginName}"`;
        // Default message — overwritten if we can fetch actual state
        content.innerHTML = `
            <div>${displayName} is not enabled</div>
            <div style="font-size: 11px; color: #999; max-width: 320px;">
                Enable in <code style="color: #d4f0d4;">am.toml</code>:
            </div>
            ${pluginName !== 'unknown' ? `
                <code style="color: #d4f0d4; text-align: left; line-height: 1.5;">
                    [[plugins]]<br>
                    name = "${pluginName}"<br>
                    path = "path/to/plugin"
                </code>
            ` : ''}
        `;

        // Fetch actual plugin state to show real error
        fetchPluginState(pluginName, content, displayName);
    }

    element.appendChild(content);

    log.debug(SEG.GLYPH, `[PluginPlaceholder] Created placeholder for ${pluginName} plugin`);

    return element;
}

/** Fetch plugin state from API and update placeholder content with actual error */
function fetchPluginState(pluginName: string, content: HTMLElement, displayName: string): void {
    apiFetch('/api/plugins')
        .then(resp => resp.ok ? resp.json() : null)
        .then((data: { plugins?: Array<{ name: string; state: string; message?: string }> } | null) => {
            if (!data?.plugins) return;
            const info = data.plugins.find(p => p.name === pluginName);
            if (!info) return; // Not in registry at all — "not enabled" is correct

            if (info.state === 'loading') {
                content.textContent = '';
                content.append(
                    el('div', { text: `${displayName} is loading...` }),
                    el('div', {
                        text: 'Plugin is starting up, glyph will appear when ready',
                        style: { fontSize: '11px', color: '#999' },
                    }),
                );
            } else if (info.state === 'failed' && info.message) {
                content.textContent = '';
                content.append(
                    el('div', { text: `${displayName} failed to load` }),
                    el('div', {
                        text: info.message,
                        style: {
                            fontSize: '11px', color: '#ef4444', maxWidth: '360px',
                            wordBreak: 'break-word', overflowWrap: 'break-word',
                        },
                    }),
                );
            }
        })
        .catch(() => {
            // API unreachable — keep default "not enabled" message
        });
}
