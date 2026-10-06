/**
 * Obsidian Element — a vault's folders, each bindable to a folder of a
 * repository, and what each bound folder is now (ADR-049).
 */

// "you would think there would be a Obsidian Element to make it a bit easier"
// "and i guess i want to do the docs tracking in that Element as well"

import type { Element } from '@teranos/elements';
import { tray } from '@teranos/elements';
import { apiFetch, apiJson } from './client';
import { jsonBody } from './http-utils';
import { Button } from './components/button';
import { refusal } from './self-person';
import { log, SEG } from './logger';

/** One vault, as /api/vault gives it. */
export interface Vault {
    name: string;
    path: string;
    /** Each folder it holds, as owner/repo@branch:path=place, place being where in the vault it is. */
    folders: string[];
}

/** What one bound folder is now, as /api/vault/states gives it. */
export interface FolderState {
    folder: string;
    place: string;
    state: 'active' | 'disabled' | 'invalid';
    why: string;
}

/** A user or organization the App is installed on, as /api/vault/owners gives it. */
export interface Owner {
    login: string;
    type: string;
    installation: number;
}

const ELEMENT_ID = 'obsidian-element';

// "the real story should not have to depend on having claude already setup in the system"

/** 1.0.0 BLOCKER (#1091): what the element says while a vault is set up by hand. */
export const SETUP_NOT_BUILT = 'Setting up a vault here is not built yet: signing in to Obsidian Sync, choosing a vault and keeping it syncing (#1091, 1.0.0 blocker).';

function div(className: string, text = ''): HTMLDivElement {
    const d = document.createElement('div');
    d.className = className;
    d.textContent = text;
    return d;
}

function pick(text: string, onClick: () => void, className = 'obsidian-pick'): HTMLButtonElement {
    const b = document.createElement('button');
    b.type = 'button';
    b.className = className;
    b.textContent = text;
    b.addEventListener('click', onClick);
    return b;
}

const query = (path: string, params: Record<string, string>) => `${path}?${new URLSearchParams(params).toString()}`;

/** A folder of a repository a vault's folder is being bound to, as far as it has been chosen. */
interface Binding {
    owner?: Owner;
    repo?: string;
    /** The repository folder whose folders are listed, '' being its top. */
    at: string;
    /** The repository folder chosen. */
    path?: string;
}

/** Everything the element shows of one vault. */
export interface View {
    vault: Vault;
    dirs: string[];
    states: FolderState[];
}

// "normal view, just the directory listing"
// "left click a dir: - Bind to repository md folder - gh user or orgs, click
// one - see repos, click one - see subdirs, click one"

/** Exported for tests: the vault's folders as a tree, each bound one marked with its state. */
export function renderVault(container: HTMLElement, view: View, reload: () => Promise<void>): void {
    container.innerHTML = '';
    const header = div('obsidian-vault');
    header.append(div('obsidian-vault-name', view.vault.name), div('obsidian-vault-path', view.vault.path));
    container.appendChild(header);

    const byPlace = new Map(view.states.map(s => [s.place, s]));
    const expanded = new Set<string>();
    let open: string | null = null;
    let binding: Binding | null = null;

    const tree = div('obsidian-tree');
    const record = div('obsidian-record');
    container.append(tree, record);

    const childrenOf = (place: string) => view.dirs.filter(d => {
        if (place === '') return !d.includes('/');
        return d.startsWith(place + '/') && !d.slice(place.length + 1).includes('/');
    });

    // The node's own refusal (server/vault.go): no place is another's or inside it.
    const clash = (place: string): string | null => {
        for (const other of byPlace.keys()) {
            if (other !== place && (place.startsWith(other + '/') || other.startsWith(place + '/'))) {
                return `${other} and ${place} are one place in the vault, or one holds the other`;
            }
        }
        return null;
    };

    const draw = () => {
        tree.innerHTML = '';
        tree.appendChild(level(''));
        record.innerHTML = '';
        record.appendChild(div('obsidian-record-title', 'What the node keeps'));
        if (view.vault.folders.length === 0) record.appendChild(div('obsidian-folder', 'No folder is bound yet.'));
        for (const folder of view.vault.folders) record.appendChild(div('obsidian-folder', folder));
    };

    const level = (parent: string): HTMLUListElement => {
        const ul = document.createElement('ul');
        for (const place of childrenOf(parent)) {
            const li = document.createElement('li');
            const kids = childrenOf(place).length > 0;
            const row = div('obsidian-row' + (open === place ? ' open' : ''));
            row.dataset.place = place;
            const twist = pick(kids ? (expanded.has(place) ? '▾' : '▸') : '', () => {
                if (expanded.has(place)) expanded.delete(place); else expanded.add(place);
                draw();
            }, 'obsidian-twist');
            twist.disabled = !kids;
            const name = pick(place.slice(place.lastIndexOf('/') + 1), () => {
                open = open === place ? null : place;
                binding = null;
                draw();
            }, 'obsidian-name');
            row.append(twist, name);
            const state = byPlace.get(place);
            if (state) {
                // The row is its state's colour, and its dot says nothing until hovered.
                const says = state.state === 'invalid' ? `Invalid: ${state.why}` : state.state;
                row.classList.add(`obsidian-row-${state.state}`);
                row.title = says;
                const dot = div(`obsidian-state obsidian-state-${state.state}`);
                dot.setAttribute('role', 'img');
                dot.setAttribute('aria-label', says);
                const ends = state.folder.slice(0, state.folder.lastIndexOf('='));
                row.append(div('obsidian-bound', `⇄ ${ends}`), dot);
            }
            li.appendChild(row);
            if (open === place) li.appendChild(panel(place));
            if (kids && expanded.has(place)) li.appendChild(level(place));
            ul.appendChild(li);
        }
        return ul;
    };

    const panel = (place: string): HTMLDivElement => {
        const p = div('obsidian-panel');
        const state = byPlace.get(place);
        if (state) {
            p.append(div('obsidian-folder', state.folder));
            // "and if expanded, there should be a two stage button to allow me to unbind as well."
            const unbind = new Button({
                label: 'Unbind',
                variant: 'danger',
                confirmation: { label: 'Confirm again to unbind' },
                onClick: async () => {
                    const response = await apiFetch('/api/vault/unbind', jsonBody('POST', { name: view.vault.name, place }));
                    if (!response.ok) throw new Error(await refusal(response));
                    await reload();
                },
            });
            // "and another button to simply disable it, but the bind is still there, it just doesnt do anything"
            const disabled = state.state === 'disabled';
            const toggle = new Button({
                label: disabled ? 'Enable' : 'Disable',
                variant: 'secondary',
                onClick: async () => {
                    const response = await apiFetch(`/api/vault/${disabled ? 'enable' : 'disable'}`, jsonBody('POST', { name: view.vault.name, place }));
                    if (!response.ok) throw new Error(await refusal(response));
                    await reload();
                },
            });
            const actions = div('obsidian-actions');
            actions.append(toggle.element, unbind.element);
            p.appendChild(actions);
            return p;
        }
        const refused = clash(place);
        if (refused) {
            p.append(div('obsidian-refused', refused));
            return p;
        }
        if (!binding) {
            p.append(pick('Bind to repository md folder', () => { binding = { at: '' }; draw(); }, 'obsidian-action'));
            return p;
        }
        const b = binding;
        const crumbs = div('obsidian-crumbs');
        crumbs.append(`${place} ⇄ `, b.owner ? pick(b.owner.login, () => { binding = { owner: b.owner, at: '' }; draw(); }, 'obsidian-crumb') : '…');
        if (b.repo) crumbs.append(' / ', pick(b.repo.slice(b.repo.indexOf('/') + 1), () => { binding = { owner: b.owner, repo: b.repo, at: '' }; draw(); }, 'obsidian-crumb'));
        if (b.at) crumbs.append(` : ${b.at}`);
        p.appendChild(crumbs);

        const list = div('obsidian-list');
        p.appendChild(list);
        if (b.path !== undefined) {
            p.append(div('obsidian-folder', `${b.repo}@main:${b.path}=${place}`));
            const confirm = new Button({
                label: 'Confirm',
                variant: 'primary',
                confirmation: { label: 'Confirm again to bind' },
                onClick: async () => {
                    const response = await apiFetch('/api/vault/bind', jsonBody('POST', { name: view.vault.name, place, repo: b.repo, path: b.path }));
                    if (!response.ok) throw new Error(await refusal(response));
                    await reload();
                },
            });
            p.appendChild(confirm.element);
        } else {
            void fill(list, b, place);
        }
        p.appendChild(pick('Cancel', () => { open = null; binding = null; draw(); }, 'obsidian-cancel'));
        return p;
    };

    // Each step asks the node, as the App, for what can be clicked next.
    const fill = async (list: HTMLElement, b: Binding, place: string) => {
        list.appendChild(div('element-loading', 'Asking GitHub…'));
        try {
            if (!b.owner) {
                const { owners } = await apiJson<{ owners: Owner[] }>('/api/vault/owners');
                list.innerHTML = '';
                for (const owner of owners) {
                    const kind = owner.type === 'Organization' ? 'org' : 'user';
                    list.appendChild(pick(`${owner.login}  ${kind}`, () => { binding = { owner, at: '' }; draw(); }));
                }
            } else if (!b.repo) {
                const { repos } = await apiJson<{ repos: string[] }>(query('/api/vault/repos', { installation: String(b.owner.installation) }));
                list.innerHTML = '';
                for (const repo of repos) {
                    list.appendChild(pick(repo.slice(repo.indexOf('/') + 1), () => { binding = { owner: b.owner, repo, at: '' }; draw(); }));
                }
            } else {
                const { dirs } = await apiJson<{ dirs: string[] }>(query('/api/vault/subdirs', { repo: b.repo, path: b.at }));
                list.innerHTML = '';
                if (b.at) list.appendChild(pick(`${b.at}  this folder`, () => { binding = { ...b, path: b.at }; draw(); }, 'obsidian-pick obsidian-here'));
                for (const dir of dirs) {
                    const row = div('obsidian-subdir');
                    row.append(
                        pick(dir.slice(dir.lastIndexOf('/') + 1), () => { binding = { ...b, path: dir }; draw(); }),
                        pick('▸', () => { binding = { ...b, at: dir }; draw(); }, 'obsidian-into'),
                    );
                    list.appendChild(row);
                }
                if (dirs.length === 0 && !b.at) list.appendChild(div('obsidian-folder', `${b.repo} has no folder on main.`));
            }
        } catch (err: unknown) {
            const message = err instanceof Error ? err.message : String(err);
            log.error(SEG.UI, `[ObsidianElement] binding ${place}: ${message}`, err);
            list.innerHTML = '';
            list.appendChild(div('obsidian-refused', message));
        }
    };

    draw();
}

/** Exported for tests: ask the node once and draw each vault from its answers. */
export async function load(container: HTMLElement): Promise<void> {
    const reload = () => load(container);
    try {
        const { vaults } = await apiJson<{ vaults: Vault[] }>('/api/vault');
        container.innerHTML = '';
        if (vaults.length === 0) {
            container.appendChild(div('element-loading', 'The node keeps no vault yet.'));
            return;
        }
        for (const vault of vaults) {
            const [{ dirs }, { folders }] = await Promise.all([
                apiJson<{ dirs: string[] }>(query('/api/vault/dirs', { name: vault.name })),
                apiJson<{ folders: FolderState[] }>(query('/api/vault/states', { name: vault.name })),
            ]);
            const section = div('obsidian-vault-section');
            container.appendChild(section);
            renderVault(section, { vault, dirs, states: folders }, reload);
        }
    } catch (err: unknown) {
        const message = `the node did not say its vaults: ${err instanceof Error ? err.message : String(err)}`;
        log.error(SEG.UI, `[ObsidianElement] ${message}`, err);
        container.innerHTML = '';
        container.appendChild(div('element-error', message));
    }
}

export function createObsidianElement(): Element {
    return {
        id: ELEMENT_ID,
        title: 'Obsidian',
        symbol: '◆',
        renderContent: () => {
            const content = div('obsidian-element-content');
            const vaults = div('obsidian-vaults');
            vaults.appendChild(div('element-loading', 'Loading the node’s vaults…'));
            content.append(div('obsidian-setup-not-built', SETUP_NOT_BUILT), vaults);
            void load(vaults);
            return content;
        },
    };
}

/** Opens the Obsidian element. */
export function openObsidianElement(): void {
    tray.open(ELEMENT_ID);
}
