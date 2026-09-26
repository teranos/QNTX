/**
 * Who is looking, top-left: a round frame with their picture, their name
 * beside it, and under it the namespace they stand in.
 *
 * "username and round photoframe topleft"
 * "yeah, instead of what is now the qntx square logo actually."
 *
 * The frame is always there and always round. A person the node has no
 * picture of yet gets an empty frame, never the logo.
 */

import { apiFetch } from './client';
import { log, SEG } from './logger';
import type { Person } from './self-person';

function empty(picture: HTMLImageElement): void {
    picture.removeAttribute('src');
    picture.classList.add('empty');
}

/**
 * Draws the person into the header. The picture is asked of the node, not
 * the provider: the page's CSP lets images come from the node and from
 * blob: URLs, and nothing else.
 */
export async function drawWho(who: Person | null): Promise<void> {
    const picture = document.getElementById('who-picture') as HTMLImageElement | null;
    const name = document.getElementById('who-name');
    const namespace = document.getElementById('who-namespace');
    if (!picture || !name || !namespace) return;

    if (!who) {
        empty(picture);
        picture.alt = '';
        name.textContent = '';
        namespace.textContent = '';
        return;
    }

    name.textContent = who.name || who.user;
    namespace.textContent = who.standing;
    picture.alt = who.name || who.user;

    if (!who.picture) {
        empty(picture);
        return;
    }
    try {
        const response = await apiFetch('/i/picture');
        if (!response.ok) {
            log.warn(SEG.UI, `[Who] The node did not answer the picture: HTTP ${response.status}`);
            empty(picture);
            return;
        }
        picture.src = URL.createObjectURL(await response.blob());
        picture.classList.remove('empty');
    } catch (err: unknown) {
        log.warn(SEG.UI, '[Who] The picture was not fetched:', err);
        empty(picture);
    }
}
