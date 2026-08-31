/**
 * Door Draft Glyph — what the door onto a namespace would be (ADR-032).
 *
 * A namespace can be born at runtime. A door cannot: it comes from am.toml and
 * nothing in the node writes that file. So this does not open a door. It says
 * the block, exactly, and hands it over.
 *
 * "the node knows what the console asks for, hands it over, you paste back.
 *  yes, in this age you paste it back to the agent who will deal with it"
 *
 * Which is why every part of this is a thing you can take away whole.
 */

import { glyphRun } from '@qntx/glyphs';
import { apiFetch } from './client';
import { jsonBody } from './http-utils';
import { escapeHtml } from './html-utils';
import { createPrimaryButton } from './components/button';
import { log, SEG } from './logger';

/** What the node says the door would be. Mirrors auth.DoorDraft. */
interface DoorDraft {
    namespace: string;
    rp_id: string;
    origins: string[];
    redirect_uri: string;
    toml: string;
    client_toml: string;
}

function glyphIdFor(namespace: string): string {
    return `door-draft-${namespace}`;
}

/**
 * Opens the door draft for a namespace. Called when one is created, and
 * reachable again afterwards — a namespace whose door was never pasted in is
 * still a namespace waiting for one.
 */
export function openDoorDraftGlyph(namespace: string): void {
    const glyphId = glyphIdFor(namespace);
    if (glyphRun.has(glyphId)) {
        glyphRun.openGlyph(glyphId);
        return;
    }

    glyphRun.add({
        id: glyphId,
        title: `${namespace} door`,
        symbol: '⌸',
        onClose: () => { glyphRun.remove(glyphId); },
        renderContent: () => content(namespace),
    });
    glyphRun.openGlyph(glyphId);
}

function content(namespace: string): HTMLElement {
    const root = document.createElement('div');
    root.className = 'door-draft';

    root.appendChild(preamble(namespace));

    const form = document.createElement('div');
    form.className = 'door-draft-form';
    root.appendChild(form);

    // Where the door is reached. The one thing the node cannot know: a domain
    // somebody owns, and nothing about this deployment says which.
    const origins = field(form, 'Origins', 'https://portal.example.com',
        'Where a browser reaches this door — where the page is, never where the API answers. Several, separated by spaces.');
    // The domain those origins share. Left empty it takes the first origin's
    // host, which is the answer whenever a door is one hostname.
    const rpID = field(form, 'rp_id', 'left empty: the first origin\'s host',
        'The relying party the passkeys made at this door belong to. Every browser requires it to be a registrable domain suffix of every origin above.');

    const said = document.createElement('div');
    said.className = 'door-draft-said';
    root.appendChild(said);

    const drafted = document.createElement('div');
    drafted.className = 'door-draft-block';
    root.appendChild(drafted);

    const draw = createPrimaryButton('Say the block', async () => {
        said.textContent = '';
        drafted.innerHTML = '';

        const asked = {
            namespace,
            origins: origins.value.split(' ').map(o => o.trim()).filter(o => o !== ''),
            rp_id: rpID.value.trim(),
        };
        const response = await apiFetch('/api/doors/draft', jsonBody('POST', asked));
        if (!response.ok) {
            const why = await response.text();
            log.error(SEG.ERROR, '[DoorDraft] refused:', namespace, response.status, why);
            // The node's own words. It refuses on the rule a browser would
            // refuse on later, so this is the answer rather than a summary.
            said.textContent = why.trim() || `HTTP ${response.status}`;
            return;
        }

        drafted.appendChild(rendered(await response.json() as DoorDraft));
    });
    form.appendChild(draw.element);

    return root;
}

function preamble(namespace: string): HTMLElement {
    const said = document.createElement('div');
    said.className = 'door-draft-preamble';
    said.innerHTML =
        `<p>${escapeHtml(namespace)} exists. Nobody can arrive at it yet — arriving needs a door, ` +
        `and a door comes from <code>am.toml</code>, which this node reads and never writes.</p>` +
        `<p>So it says the block instead. Take it where it goes.</p>`;
    return said;
}

function field(into: HTMLElement, label: string, placeholder: string, why: string): HTMLInputElement {
    const row = document.createElement('label');
    row.className = 'door-draft-field';

    const name = document.createElement('span');
    name.className = 'door-draft-label';
    name.textContent = label;
    row.appendChild(name);

    const input = document.createElement('input');
    input.type = 'text';
    input.placeholder = placeholder;
    row.appendChild(input);

    const note = document.createElement('span');
    note.className = 'door-draft-why';
    note.textContent = why;
    row.appendChild(note);

    into.appendChild(row);
    return input;
}

function rendered(draft: DoorDraft): HTMLElement {
    const out = document.createElement('div');

    out.appendChild(block(
        'Paste this under [auth] in am.toml',
        draft.toml,
        'This is the door. Once it is in the file the config watcher opens it — no restart.',
    ));

    out.appendChild(block(
        'The provider\'s console asks for this redirect URI',
        draft.redirect_uri,
        'Where this node answers, which is not where the door is. A console told a different one fails the ceremony at the very end.',
    ));

    out.appendChild(block(
        'And when that console has issued a client',
        draft.client_toml,
        'Until this is in the file the door consents under the node\'s client, so the screen says the node\'s name instead of this door\'s.',
    ));

    return out;
}

/** One thing you can take away whole. Pressing it copies it. */
function block(title: string, body: string, why: string): HTMLElement {
    const wrap = document.createElement('div');
    wrap.className = 'door-draft-out';

    const head = document.createElement('div');
    head.className = 'door-draft-out-title';
    head.textContent = title;
    wrap.appendChild(head);

    const pre = document.createElement('pre');
    pre.className = 'door-draft-pre';
    pre.title = 'press to copy';
    pre.textContent = body;
    pre.addEventListener('click', () => {
        void navigator.clipboard.writeText(body).then(
            () => { flash(head, title, 'copied'); },
            () => { flash(head, title, 'the browser refused the clipboard'); },
        );
    });
    wrap.appendChild(pre);

    const note = document.createElement('div');
    note.className = 'door-draft-why';
    note.textContent = why;
    wrap.appendChild(note);

    return wrap;
}

function flash(el: HTMLElement, was: string, now: string): void {
    el.textContent = now;
    setTimeout(() => { el.textContent = was; }, 1200);
}
