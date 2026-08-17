import { apiFetch } from './client';
import { jsonBody } from './http-utils';
import { escapeHtml } from './html-utils';
import { log, SEG } from './logger.ts';
import { createButton } from './components/button';
import { tilesHtml, type Namespace } from './namespaces-view';

let bar: HTMLElement | null = null;
let namespaces: Namespace[] = [];
let selected = '';
let adding = false;
let failure = '';

// The node decides who sees this: 501 when it keeps one universe, 403 when the
// caller is not SUPER. Either answer means no bar at all, not an empty one.
async function load(): Promise<boolean> {
    const response = await apiFetch('/api/namespaces');
    if (response.status === 501 || response.status === 403) return false;
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

    const said = failure === '' ? '' : `<div class="namespaces-failure">${escapeHtml(failure)}</div>`;
    bar.innerHTML = tilesHtml(namespaces, selected, adding) + said;

    mountRemove();
    if (adding) bar.querySelector<HTMLInputElement>('#namespace-new')?.focus();
}

// Deleting a namespace takes everything inside it (ADR-027), so the button asks
// twice and the second press names what is about to go.
function mountRemove(): void {
    const slot = bar?.querySelector<HTMLElement>('.namespace-remove');
    if (!slot) return;

    const name = slot.dataset.name || '';
    const button = createButton({
        label: '−',
        variant: 'danger',
        confirmation: { label: `delete ${name}` },
        onClick: () => remove(name),
    });
    slot.appendChild(button.element);
}

async function remove(name: string): Promise<void> {
    const response = await apiFetch(`/api/namespaces/${encodeURIComponent(name)}`, { method: 'DELETE' });
    if (!response.ok) {
        throw new Error(`could not delete ${name}: HTTP ${response.status} ${await response.text()}`);
    }
    selected = '';
    await load();
    render();
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
}

function attach(el: HTMLElement): void {
    el.addEventListener('click', (e: Event) => {
        const target = e.target as HTMLElement;
        if (target.closest('.namespace-remove')) return;

        if (target.closest('.namespace-add')) {
            adding = true;
            render();
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

// The bar sits under the system bar and above the log, so pressing space opens
// the drawer onto it. A node with no namespaces to manage grows no bar.
export async function initNamespacesBar(): Promise<void> {
    const header = document.getElementById('system-drawer-header');
    if (!header) return;

    let keeps = false;
    try {
        keeps = await load();
    } catch (error: unknown) {
        log.error(SEG.ERROR, '[Namespaces] Failed to reach /api/namespaces:', error);
        return;
    }
    if (!keeps) return;

    bar = document.createElement('div');
    bar.className = 'namespaces-bar';
    header.insertAdjacentElement('afterend', bar);
    attach(bar);
    render();
}
