/**
 * Element Module Loader — dynamic import + ElementUI injection for TS-authored plugin elements.
 *
 * When a plugin declares a module_url in its ElementDef, the frontend uses this
 * loader instead of the legacy HTML pipeline. The module is dynamically imported,
 * its render function is called with a ElementUI, and the returned element
 * is mounted on the canvas.
 *
 * This eliminates: innerHTML, script re-execution, duplicated escapeHTML,
 * global window.* pollution, and gives plugins type-safe access to QNTX primitives.
 */

import type { Element } from '@teranos/elements';
import type { PluginElementDef } from './plugin-provided-elements';
import type { ElementModule } from './element-ui';
import { createElementUI } from './element-ui';
import { loadPluginCSS } from './plugin-provided-elements';
import { log, SEG } from '../../logger';
import { canvasPlaced } from '@teranos/elements';
import { preventDrag } from '@teranos/elements';
import { wireExpandToWindow } from '@teranos/elements';

// Cache imported modules — one import per module_url
const moduleCache = new Map<string, Promise<ElementModule>>();

/** Create a plugin element by dynamically importing its TypeScript module. */
export async function createPluginElementFromModule(
    item: Element,
    def: PluginElementDef
): Promise<HTMLElement> {
    // Load CSS if provided (cached globally)
    if (def.css_url) {
        loadPluginCSS(def.css_url);
    }

    const moduleUrl = def.module_url!;

    try {
        // Import module (cached per URL) — relative path resolves against
        // window.location.origin (dev server in dev, backend in production)
        const mod = await loadModule(moduleUrl);

        // Create ElementUI scoped to this element
        const ui = createElementUI(item, def.plugin);

        // The module's render() may call ui.element() for a canvasPlaced
        // wrapper, or return a raw element. If the returned element lacks
        // data-element-id, wrap it so selection/deletion work on the canvas.
        const rendered = await mod.render(item, ui);

        if (!rendered.dataset.elementId) {
            return wrapInCanvasPlaced(item, rendered, {
                plugin: def.plugin,
                title: def.title,
                symbol: def.symbol,
                defaultWidth: def.default_width,
                defaultHeight: def.default_height,
            });
        }

        log.debug(SEG.ELEMENT, `[ElementModule] Rendered ${def.plugin} element ${item.id} from module`);
        return rendered;
    } catch (err) {
        log.error(SEG.ELEMENT, `[ElementModule] Failed to load module for ${def.plugin}: ${moduleUrl}`, err);
        return createModuleErrorElement(item, def, err);
    }
}

interface WrapOpts {
    plugin: string;
    title?: string;
    symbol?: string;
    defaultWidth?: number;
    defaultHeight?: number;
}

/** Wrap a raw plugin element in canvasPlaced with title bar for selection/drag/resize. */
export function wrapInCanvasPlaced(item: Element, rendered: HTMLElement, opts: WrapOpts): HTMLElement {
    // Expand button for morph to window
    const expandBtn = document.createElement('button');
    expandBtn.textContent = '\u2B06'; // ⬆
    expandBtn.title = 'Expand to window';
    expandBtn.setAttribute('aria-label', 'Expand to window');
    preventDrag(expandBtn);

    const title = opts.title || opts.plugin;
    const { element } = canvasPlaced({
        item: item,
        className: `canvas-plugin-element plugin-${opts.plugin}`,
        defaults: {
            x: item.x ?? 200,
            y: item.y ?? 200,
            width: opts.defaultWidth ?? 400,
            height: opts.defaultHeight ?? 300,
        },
        titleBar: { label: title, actions: [expandBtn] },
        resizable: true,
        logLabel: `Plugin:${opts.plugin}`,
    });
    element.appendChild(rendered);

    wireExpandToWindow({
        element,
        expandBtn,
        elementId: item.id,
        title,
        symbol: opts.symbol || item.symbol || '',
        renderContent: () => rendered,
        logLabel: `Plugin:${opts.plugin}`,
        adoptExtras: { opensAs: 'window' as const },
    });

    log.debug(SEG.ELEMENT, `[ElementModule] Wrapped ${opts.plugin} element ${item.id} in canvasPlaced`);
    return element;
}

/** Load and cache a plugin module. Only caches successful imports. */
function loadModule(url: string): Promise<ElementModule> {
    const cached = moduleCache.get(url);
    if (cached) return cached;

    const pending = import(/* @vite-ignore */ url).then((mod) => {
        // Support both default export and named export
        if (typeof mod.render === 'function') return mod as ElementModule;
        if (mod.default && typeof mod.default.render === 'function') return mod.default as ElementModule;
        throw new Error(`Module does not export a render function: ${url}`);
    });

    // Cache the promise immediately for dedup, but evict on failure
    // so the next attempt can retry (e.g., after plugin restart)
    moduleCache.set(url, pending);
    pending.catch((err: unknown) => {
        // The rejection still reaches the caller through the returned
        // promise; this only clears the cache so the next attempt retries.
        log.debug(SEG.ELEMENT, `Module load failed, evicting ${url} from cache:`, err);
        moduleCache.delete(url);
    });

    return pending;
}

/** Create an error placeholder when module loading fails. */
function createModuleErrorElement(item: Element, def: PluginElementDef, err: unknown): HTMLElement {
    const { element } = canvasPlaced({
        item: item,
        className: `canvas-plugin-element plugin-${def.plugin}`,
        defaults: {
            x: item.x ?? 200,
            y: item.y ?? 200,
            width: def.default_width ?? 400,
            height: def.default_height ?? 300,
        },
        resizable: false,
        logLabel: 'ElementModuleError',
    });

    const content = document.createElement('div');
    content.style.padding = '16px';
    content.style.color = 'var(--color-error, #ef4444)';
    content.style.fontFamily = 'var(--font-mono)';
    content.style.fontSize = '12px';

    const title = document.createElement('div');
    title.style.fontWeight = 'bold';
    title.style.marginBottom = '8px';
    title.textContent = `Failed to load ${def.plugin} module`;

    const detail = document.createElement('div');
    detail.style.opacity = '0.8';
    detail.textContent = err instanceof Error ? err.message : String(err);

    content.appendChild(title);
    content.appendChild(detail);
    element.appendChild(content);

    return element;
}
