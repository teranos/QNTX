/**
 * Canvas-Placed Manifestation — shared wrapper for glyphs on the canvas workspace.
 *
 * Every canvas-placed glyph needs:  container, position, drag, and (usually)
 * a title bar and resize handle.  This module provides all of that so each
 * glyph factory only has to supply its content.
 *
 * Usage:
 *   const { element, titleBar } = canvasPlaced({ glyph, className, ... });
 *   element.appendChild(myContent);        // factory adds its content
 *   titleBar?.appendChild(extraButton);    // optional: extend the title bar
 */

import type { Glyph } from '../glyph';
import {
    applyCanvasGlyphLayout,
    makeDraggable,
    makeResizable,
    preventDrag,
    storeCleanup,
    type MakeDraggableOptions,
} from '../glyph-interaction';

// ── Config ──────────────────────────────────────────────────────────

export interface CanvasPlacedConfig {
    glyph: Glyph;
    /** Type-specific CSS class (e.g. 'canvas-py-glyph'). Joined with 'canvas-glyph'. */
    className: string;
    /** Default position and size when glyph has no saved layout. */
    defaults: { x: number; y: number; width: number; height: number };
    /** Title bar with label and optional action buttons. Omit for no title bar. */
    titleBar?: { label: string; actions?: HTMLElement[] };
    /** Custom drag handle. Falls back to title bar, then element. */
    dragHandle?: HTMLElement;
    /** Extra options forwarded to makeDraggable. */
    draggableOptions?: Partial<MakeDraggableOptions>;
    /** Enable resize handle. Pass object for custom min dimensions. */
    resizable?: boolean | { minWidth?: number; minHeight?: number };
    /** Extra CSS class(es) for the resize handle (e.g. 'glyph-resize-handle--small'). */
    resizeHandleClass?: string;
    /** Label for drag/resize log messages (e.g. 'PyGlyph'). */
    logLabel: string;
    /** Use minHeight instead of height (for auto-sizing glyphs). */
    useMinHeight?: boolean;
    /** Existing element to populate (for conversion support). Creates new div if omitted. */
    element?: HTMLElement;
}

export interface CanvasPlacedResult {
    element: HTMLElement;
    titleBar: HTMLElement | null;
}

// ── Factory ─────────────────────────────────────────────────────────

export function canvasPlaced(config: CanvasPlacedConfig): CanvasPlacedResult {
    const { glyph, className, defaults, logLabel } = config;

    // Container
    const element = config.element ?? document.createElement('div');
    element.className = `${className} canvas-glyph`;
    element.dataset.glyphId = glyph.id;
    if (glyph.symbol) element.dataset.glyphSymbol = glyph.symbol;

    // Layout
    applyCanvasGlyphLayout(element, {
        x: glyph.x ?? defaults.x,
        y: glyph.y ?? defaults.y,
        width: glyph.width ?? defaults.width,
        height: glyph.height ?? defaults.height,
        useMinHeight: config.useMinHeight,
    });

    // Title bar
    let titleBar: HTMLElement | null = null;
    if (config.titleBar) {
        titleBar = document.createElement('div');
        titleBar.className = 'canvas-glyph-title-bar';

        const label = document.createElement('span');
        label.textContent = config.titleBar.label;
        titleBar.appendChild(label);

        if (config.titleBar.actions) {
            for (const action of config.titleBar.actions) {
                preventDrag(action);
                titleBar.appendChild(action);
            }
        }

        element.appendChild(titleBar);
    }

    // Drag
    const dragHandle = config.dragHandle ?? titleBar ?? element;
    const cleanupDrag = makeDraggable(element, dragHandle, glyph, {
        logLabel,
        ...config.draggableOptions,
    });
    storeCleanup(element, cleanupDrag);

    // Resize
    if (config.resizable) {
        const handle = document.createElement('div');
        handle.className = config.resizeHandleClass
            ? `glyph-resize-handle ${config.resizeHandleClass}`
            : 'glyph-resize-handle';
        element.appendChild(handle);

        const resizeOpts = typeof config.resizable === 'object' ? config.resizable : {};
        const cleanupResize = makeResizable(element, handle, glyph, {
            logLabel,
            ...resizeOpts,
        });
        storeCleanup(element, cleanupResize);
    }

    return { element, titleBar };
}
