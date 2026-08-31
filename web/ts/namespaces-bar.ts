import { apiFetch, connectivity } from './client';
import { openDoorDraftGlyph } from './door-draft-glyph';
import { jsonBody } from './http-utils';
import { escapeHtml } from './html-utils';
import { log, SEG } from './logger.ts';
import { tilesHtml, type Namespace } from './namespaces-view';
import { standAtTheDoor } from './signin';

let bar: HTMLElement | null = null;
let namespaces: Namespace[] = [];
let selected = '';
let adding = false;
let failure = '';

// 501 when the node keeps one universe, 403 below SUPER, 401 when nobody is
// signed in yet. None is an error to show — they mean there is no bar.
async function load(): Promise<boolean> {
    const response = await apiFetch('/api/namespaces');
    if (response.status === 501 || response.status === 403 || response.status === 401) return false;
    if (!response.ok) {
        failure = `could not read namespaces: HTTP ${response.status} ${await response.text()}`;
        return true;
    }
    const data = await response.json() as { namespaces: Namespace[] };
    namespaces = data.namespaces || [];
    failure = '';
    return true;
}

function render(): void {
    if (!bar) return;

    const said = failure === '' ? '' : `<div class="namespaces-failure" title="press to copy">${escapeHtml(failure)}</div>`;
    bar.innerHTML = tilesHtml(namespaces, selected, adding) + said;

    if (adding) bar.querySelector<HTMLInputElement>('#namespace-new')?.focus();
}

async function create(name: string): Promise<void> {
    const response = await apiFetch('/api/namespaces', jsonBody('POST', { name }));
    adding = false;

    if (!response.ok) {
        const said = await response.text();
        log.error(SEG.ERROR, '[Namespaces] Failed to create:', name, response.status, said);
        failure = `could not create ${name}: HTTP ${response.status} ${said}`;
        render();
        return;
    }

    await load();
    render();

    // A namespace nobody can arrive at is not finished. Arriving needs a door,
    // and a door is the one thing here the node cannot write for itself — so
    // the moment it is created is the moment to say what that door would be.
    openDoorDraftGlyph(name);
}

function attach(el: HTMLElement): void {
    el.addEventListener('click', (e: Event) => {
        const target = e.target as HTMLElement;
        if (target.closest('.door-latch')) {
            standAtTheDoor();
            return;
        }
        if (target.closest('.namespace-add')) {
            adding = true;
            render();
            return;
        }
        // Beside the tiles, not swallowed into a hover: a press copies the
        // failure reason, the same acknowledgement as tokens-glyph.ts didCell().
        const said = target.closest<HTMLElement>('.namespaces-failure');
        if (said) {
            const message = failure;
            void navigator.clipboard.writeText(message).then(
                () => { said.textContent = 'copied'; setTimeout(() => { said.textContent = message; }, 1200); },
                () => { said.textContent = 'refused'; setTimeout(() => { said.textContent = message; }, 1200); },
            );
            return;
        }

        const chosen = target.closest<HTMLElement>('.namespace-tile[data-name]');
        if (!chosen) return;
        const name = chosen.dataset.name || '';
        selected = selected === name ? '' : name;
        render();
    });

    el.addEventListener('keydown', (e: KeyboardEvent) => {
        const input = e.target as HTMLInputElement;
        if (!input.classList?.contains('namespace-new')) return;

        // Space opens this drawer, so a name with a space in it must not reach
        // the global shortcut and collapse what is being typed into.
        e.stopPropagation();

        if (e.key === 'Enter') {
            e.preventDefault();
            const name = input.value.trim();
            if (name === '') return;
            create(name);
        }
        if (e.key === 'Escape') {
            e.preventDefault();
            adding = false;
            render();
        }
    });
}

// Signing in happens after the page loads, so the bar has to be able to arrive
// later. Asking once at startup is how it reported 401 at somebody who was
// signed in — it had asked before they were.
export function initNamespacesBar(): void {
    const header = document.getElementById('system-drawer-header');
    if (!header) return;

    connectivity.subscribeAuth(authenticated => {
        if (!authenticated) {
            teardown();
            return;
        }
        void appear(header);
    });
}

async function appear(header: HTMLElement): Promise<void> {
    let keeps = false;
    try {
        keeps = await load();
    } catch (error: unknown) {
        log.error(SEG.ERROR, '[Namespaces] Failed to reach /api/namespaces:', error);
        return;
    }
    if (!keeps) {
        teardown();
        return;
    }

    if (!bar) {
        bar = document.createElement('div');
        bar.className = 'namespaces-bar';
        header.insertAdjacentElement('afterend', bar);
        attach(bar);
    }
    render();
}

// Losing the session takes the bar with it, rather than leaving a list of
// namespaces nobody is entitled to see any more.
function teardown(): void {
    bar?.remove();
    bar = null;
    selected = '';
    adding = false;
    failure = '';
}
