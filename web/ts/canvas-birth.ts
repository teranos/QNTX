/**
 * A namespace with no canvas.
 *
 * "and for every other namespace the canvas needs to be explicitly created and
 * named." — nothing is drawn but ⌗ and a name field. Enter creates it; the
 * page reloads and the canvas is there.
 */

import { Subcanvas } from './sym';

export function askForACanvas(host: HTMLElement, create: (name: string) => Promise<void>): HTMLElement {
    const birth = document.createElement('div');
    birth.className = 'canvas-birth';

    const symbol = document.createElement('span');
    symbol.className = 'canvas-birth-symbol';
    symbol.textContent = Subcanvas;

    const name = document.createElement('input');
    name.className = 'canvas-birth-name';
    name.type = 'text';
    name.placeholder = 'name this canvas';
    name.autocomplete = 'off';
    name.spellcheck = false;

    const said = document.createElement('div');
    said.className = 'canvas-birth-said';

    name.addEventListener('keydown', (e: KeyboardEvent) => {
        // Space opens the system drawer, so a name with a space in it must
        // not reach the global shortcut.
        e.stopPropagation();
        if (e.key !== 'Enter') return;
        e.preventDefault();
        const value = name.value.trim();
        if (value === '') return;
        name.disabled = true;
        said.textContent = '';
        create(value).catch((err: unknown) => {
            name.disabled = false;
            said.textContent = err instanceof Error ? err.message : String(err);
        });
    });

    birth.append(symbol, name, said);
    host.append(birth);
    name.focus();
    return birth;
}
