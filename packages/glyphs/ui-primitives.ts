/**
 * GlyphUI DOM primitives — the pure half of the GlyphUI factory.
 *
 * These build DOM and nothing else. The I/O half of GlyphUI (pluginFetch,
 * pluginWebSocket, onMeld, config persistence, logging) is host-coupled by
 * nature and stays in the host factory, which delegates the DOM building
 * blocks to these.
 */

import { preventDrag } from './canvas-drag';

/** Create a text input with drag protection already applied. */
export function createInput(opts?: { label?: string; placeholder?: string; value?: string; type?: string }): HTMLElement {
    const wrapper = document.createElement('div');
    wrapper.className = 'glyph-form-group';

    if (opts?.label) {
        const label = document.createElement('label');
        label.className = 'glyph-label';
        label.textContent = opts.label;
        wrapper.appendChild(label);
    }

    const input = document.createElement('input');
    input.className = 'glyph-input';
    input.type = opts?.type ?? 'text';
    if (opts?.placeholder) input.placeholder = opts.placeholder;
    if (opts?.value) input.value = opts.value;

    preventDrag(input);
    wrapper.appendChild(input);
    return wrapper;
}

/** Create a button with drag protection already applied. */
export function createButton(opts: { label: string; onClick: () => void; primary?: boolean }): HTMLButtonElement {
    const btn = document.createElement('button');
    btn.className = opts.primary ? 'glyph-btn glyph-btn--primary' : 'glyph-btn';
    btn.textContent = opts.label;
    btn.addEventListener('click', opts.onClick);
    preventDrag(btn);
    return btn;
}

/**
 * Create a status line for showing feedback messages.
 * TODO: Weak design element — useful concept (contextual feedback next to the
 * action that caused it) but visually underwhelming. Rethink the presentation.
 */
export function createStatusLine(): { element: HTMLElement; show(msg: string, isError?: boolean): void; clear(): void } {
    const el = document.createElement('div');
    el.className = 'glyph-status';
    el.style.fontFamily = 'monospace';
    el.style.fontSize = 'var(--font-size-xs, 10px)';
    el.style.minHeight = '16px';
    el.style.lineHeight = '16px';
    let timer: ReturnType<typeof setTimeout> | null = null;

    return {
        element: el,
        show(msg: string, isError = false) {
            el.textContent = msg;
            el.style.color = isError ? 'var(--color-error, #ef4444)' : 'var(--color-success, #22c55e)';
            if (timer) clearTimeout(timer);
            if (!isError) {
                timer = setTimeout(() => { el.textContent = ''; }, 4000);
            }
        },
        clear() {
            if (timer) clearTimeout(timer);
            el.textContent = '';
        },
    };
}
