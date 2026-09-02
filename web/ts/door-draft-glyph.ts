/**
 * Door Glyph — the door onto a namespace (ADR-032).
 *
 * A namespace can be born at runtime. A door cannot: it comes from am.toml and
 * nothing in the node writes that file. So this does not open a door. It walks
 * you to one, and every part of it is a thing you can take away whole.
 *
 * "the node knows what the console asks for, hands it over, you paste back.
 *  yes, in this age you paste it back to the agent who will deal with it"
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
    arrival: string;
    console_url: string;
}

/** The door a namespace has now. Mirrors auth.DoorStanding. */
interface DoorStanding {
    namespace: string;
    open: boolean;
    rp_id: string;
    origins: string[];
    arrival: string;
    redirect_uri: string;
    own_clients: string[];
    console_url: string;
}

function glyphIdFor(namespace: string): string {
    return `door-draft-${namespace}`;
}

/**
 * Opens the door for a namespace: when one is created, and any time after. A
 * door set up weeks ago is a thing you come back to and read rather than
 * remember.
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

    // Asked on open, so the glyph says what is rather than only what would be.
    void standing(namespace).then(door => {
        root.innerHTML = '';
        if (door?.open) {
            root.appendChild(itIsOpen(door));
            return;
        }
        root.appendChild(theWalk(namespace, door));
    });

    return root;
}

async function standing(namespace: string): Promise<DoorStanding | null> {
    const response = await apiFetch('/api/doors/standing?namespace=' + encodeURIComponent(namespace));
    if (!response.ok) {
        log.warn(SEG.UI, '[Door] could not read the standing door:', namespace, response.status);
        return null;
    }
    return await response.json() as DoorStanding;
}

// A door that is open. The address to send people to comes first, because it
// is the only line here anybody outside this screen ever needs.
function itIsOpen(door: DoorStanding): HTMLElement {
    const out = document.createElement('div');

    out.appendChild(done(door.arrival));

    out.appendChild(block('Where a browser reaches it', door.origins.join('\n'),
        `Relying party ${door.rp_id}. Passkeys made here belong to it.`));

    out.appendChild(block('The redirect URI its console was told', door.redirect_uri,
        'Where this node answers, which is not where the door is.'));

    if (door.own_clients.length > 0) {
        out.appendChild(note(`Consents under its own client for: ${door.own_clients.join(', ')}.`));
        return out;
    }

    out.appendChild(note(
        'It has no OAuth client of its own, so it consents under the node\'s and the '
        + 'screen says the node\'s name. Its own client lives in its own Google project — '
        + 'branding follows the project, not the client.'));
    out.appendChild(consoleLink(door.console_url));
    return out;
}

// A door that is not open yet: one, two, three, done.
function theWalk(namespace: string, door: DoorStanding | null): HTMLElement {
    const out = document.createElement('div');

    const said = document.createElement('div');
    said.className = 'door-draft-preamble';
    said.innerHTML = `<p>${escapeHtml(namespace)} exists. Nobody can arrive at it yet.</p>`;
    out.appendChild(said);

    const form = document.createElement('div');
    form.className = 'door-draft-form';
    out.appendChild(form);

    const origins = field(form, 'Origins', 'https://portal.example.com',
        'Where a browser reaches this door — where the page is, never where the API answers. Several, separated by spaces.');
    const rpID = field(form, 'rp_id', 'left empty: the first origin\'s host',
        'The relying party the passkeys made at this door belong to. Every browser requires it to be a registrable domain suffix of every origin above.');
    const clientID = field(form, 'client_id', 'paste it back here after step 1',
        'What the console issued. The secret never comes here — am.toml ships world-readable, so it goes to SSM and the block only names it.');

    const refused = document.createElement('div');
    refused.className = 'door-draft-said';
    out.appendChild(refused);

    const steps = document.createElement('div');
    steps.className = 'door-draft-block';
    out.appendChild(steps);

    const draw = createPrimaryButton('Say the steps', async () => {
        refused.textContent = '';
        steps.innerHTML = '';

        const asked = {
            namespace,
            origins: origins.value.split(' ').map(o => o.trim()).filter(o => o !== ''),
            rp_id: rpID.value.trim(),
        };
        const response = await apiFetch('/api/doors/draft', jsonBody('POST', asked));
        if (!response.ok) {
            const why = await response.text();
            log.error(SEG.ERROR, '[Door] refused:', namespace, response.status, why);
            refused.textContent = why.trim() || `HTTP ${response.status}`;
            return;
        }

        steps.appendChild(walked(await response.json() as DoorDraft, clientID.value.trim()));
    });

    const act = document.createElement('div');
    act.className = 'door-draft-act';
    act.appendChild(draw.element);
    form.appendChild(act);

    if (door && !door.open) {
        out.appendChild(note('Nothing in am.toml opens onto this namespace yet.'));
    }
    return out;
}

// The steps, numbered, ending in the address to hand out.
function walked(draft: DoorDraft, clientID: string): HTMLElement {
    const out = document.createElement('div');

    out.appendChild(step(1, 'Make the OAuth client'));
    out.appendChild(consoleLink(draft.console_url));
    out.appendChild(block('Authorized JavaScript origins', draft.origins.join('\n'),
        'The console asks for this first. It is where the page is.'));
    out.appendChild(block('Authorized redirect URIs', draft.redirect_uri,
        'This node, not the door. A console told a different one fails the ceremony at the very end.'));

    out.appendChild(step(2, 'Put the secret where it belongs'));
    out.appendChild(block('The secret goes to SSM, never into am.toml',
        secretPath(draft.namespace),
        'am.toml ships as a world-readable parameter, so a literal there is already disclosed.'));

    out.appendChild(step(3, 'Paste this under [auth] in am.toml'));
    out.appendChild(block('The door, and the client it consents under',
        draft.toml + '\n' + withClient(draft.client_toml, clientID),
        'The config watcher opens it — no restart.'));

    out.appendChild(done(draft.arrival));
    return out;
}

// The client_id typed above, put where the console's value goes. Left empty
// the placeholder stands, so the block is still a whole thing to take away.
function withClient(clientTOML: string, clientID: string): string {
    if (clientID === '') return clientTOML;
    const placeholder = '"<what the console issues>"';
    const at = clientTOML.indexOf(placeholder);
    if (at === -1) return clientTOML;
    return clientTOML.slice(0, at) + '"' + clientID + '"' + clientTOML.slice(at + placeholder.length);
}

function secretPath(namespace: string): string {
    return 'ssm:///q/' + namespace + '/google/client-secret';
}

function step(n: number, title: string): HTMLElement {
    const head = document.createElement('div');
    head.className = 'door-draft-step';
    head.textContent = `${n}. ${title}`;
    return head;
}

// The end of it. Everything above exists so that this line can be handed to
// somebody, so it says so rather than leaving it to be worked out.
function done(arrival: string): HTMLElement {
    const out = document.createElement('div');
    out.className = 'door-draft-done';

    const head = document.createElement('div');
    head.className = 'door-draft-out-title';
    head.textContent = 'Done. Send people here';
    out.appendChild(head);

    const where = document.createElement('a');
    where.className = 'door-draft-arrival';
    where.href = arrival;
    where.target = '_blank';
    where.rel = 'noopener noreferrer';
    where.textContent = arrival;
    out.appendChild(where);

    return out;
}

function consoleLink(url: string): HTMLElement {
    const wrap = document.createElement('div');
    wrap.className = 'door-draft-console';

    const link = document.createElement('a');
    link.href = url;
    link.target = '_blank';
    link.rel = 'noopener noreferrer';
    link.textContent = 'Open the console';
    wrap.appendChild(link);

    return wrap;
}

function note(said: string): HTMLElement {
    const out = document.createElement('div');
    out.className = 'door-draft-why';
    out.textContent = said;
    return out;
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

    const said = document.createElement('span');
    said.className = 'door-draft-why';
    said.textContent = why;
    row.appendChild(said);

    into.appendChild(row);
    return input;
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

    const said = document.createElement('div');
    said.className = 'door-draft-why';
    said.textContent = why;
    wrap.appendChild(said);

    return wrap;
}

function flash(head: HTMLElement, title: string, said: string): void {
    head.textContent = said;
    setTimeout(() => { head.textContent = title; }, 1200);
}
