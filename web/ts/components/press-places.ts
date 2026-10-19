// Presses in markup: a template says where each one stands, and it is put there once the markup is in.
// The press itself is @teranos/elements' (pressFrom); this only finds its place.

import { pressFrom, type Press, type PressHandle } from '@teranos/elements';

function attribute(text: string): string {
    return text.split('&').join('&amp;').split('"').join('&quot;').split('<').join('&lt;').split('>').join('&gt;');
}

/** Where a press stands in a template, by its id, with the page's classes for what lays it out. */
export function pressPlace(id: string, ...classes: string[]): string {
    return `<span data-press-at="${attribute(id)}" data-press-class="${attribute(classes.join(' '))}"></span>`;
}

/** Each press put where its place stands, and only those are made. A place with no press throws. */
export function placePresses(container: HTMLElement, presses: Record<string, Press>): Record<string, PressHandle> {
    const placed: Record<string, PressHandle> = {};
    for (const place of container.querySelectorAll<HTMLElement>('[data-press-at]')) {
        const id = place.getAttribute('data-press-at');
        const classes = place.getAttribute('data-press-class');
        if (typeof id !== 'string' || typeof classes !== 'string') throw new Error('a place is written by pressPlace, which says its press and its classes');
        const press = presses[id];
        if (!press) throw new Error(`a place stands for press ${id}, and there is no such press`);
        const handle = pressFrom(press);
        handle.element.classList.add(...classes.split(' ').filter(Boolean));
        place.replaceWith(handle.element);
        placed[id] = handle;
    }
    return placed;
}
