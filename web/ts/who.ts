/**
 * Who is looking, top-left: their picture, round, and their name. Under it,
 * the namespace they stand in.
 *
 * "username and round photoframe topleft" — in place of the QNTX mark, which
 * stays for a page nobody is signed in to.
 */

import { apiFetch } from './client';
import { log, SEG } from './logger';
import type { Person } from './self-person';

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
        picture.src = '/qntx.jpg';
        picture.alt = 'QNTX';
        picture.classList.remove('round');
        name.textContent = '';
        namespace.textContent = '';
        return;
    }

    name.textContent = who.name || who.user;
    namespace.textContent = who.standing;
    picture.alt = who.name || who.user;

    if (!who.picture) {
        picture.src = '/qntx.jpg';
        picture.classList.remove('round');
        return;
    }
    try {
        const response = await apiFetch('/i/picture');
        if (!response.ok) {
            log.warn(SEG.UI, `[Who] The node did not answer the picture: HTTP ${response.status}`);
            picture.src = '/qntx.jpg';
            picture.classList.remove('round');
            return;
        }
        picture.src = URL.createObjectURL(await response.blob());
        picture.classList.add('round');
    } catch (err: unknown) {
        log.warn(SEG.UI, '[Who] The picture was not fetched:', err);
        picture.src = '/qntx.jpg';
        picture.classList.remove('round');
    }
}
