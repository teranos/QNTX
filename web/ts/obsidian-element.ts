/**
 * Obsidian Element — the vaults the node keeps a copy of, and the folders of
 * repositories each holds (ADR-049).
 */

// "you would think there would be a Obsidian Element to make it a bit easier"
// "and i guess i want to do the docs tracking in that Element as well"

// Plain window in the tray, for ROOT. One section per vault, and below them a
// vault said whole. Asked on open and after each press; nothing polls.

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

const ELEMENT_ID = 'obsidian-element';

// "the real story should not have to depend on having claude already setup in the system"

/** 1.0.0 BLOCKER (#1091): what the element says while a vault is set up by hand. */
export const SETUP_NOT_BUILT = 'Setting up a vault here is not built yet: signing in to Obsidian Sync, choosing a vault and keeping it syncing (#1091, 1.0.0 blocker).';

function section(title: string): HTMLDivElement {
    const div = document.createElement('div');
    div.className = 'element-section';
    const h = document.createElement('h3');
    h.className = 'element-section-title';
    h.textContent = title;
    div.appendChild(h);
    return div;
}

function row(label: string, value: HTMLElement): HTMLDivElement {
    const div = document.createElement('div');
    div.className = 'element-row';
    const l = document.createElement('span');
    l.className = 'label';
    l.textContent = label;
    const v = document.createElement('span');
    v.className = 'element-value';
    v.appendChild(value);
    div.append(l, v);
    return div;
}

function said(text: string, className = 'element-loading'): HTMLDivElement {
    const div = document.createElement('div');
    div.className = className;
    div.textContent = text;
    return div;
}

function input(className: string, value: string, placeholder: string): HTMLInputElement {
    const box = document.createElement('input');
    box.type = 'text';
    box.className = `input ${className}`;
    box.value = value;
    box.placeholder = placeholder;
    box.autocomplete = 'off';
    box.spellcheck = false;
    return box;
}

/** A vault's folders, one per line, as the node reads them: owner/repo@branch:path=place. */
function foldersBox(folders: string[]): HTMLTextAreaElement {
    const box = document.createElement('textarea');
    box.className = 'input obsidian-folders';
    box.rows = Math.max(3, folders.length + 1);
    box.value = folders.join('\n');
    box.placeholder = 'owner/repo@branch:path=place in the vault, one per line';
    box.spellcheck = false;
    return box;
}

/** Say one vault whole. A refusal is thrown in the node's words, for the button to show. */
async function setVault(name: string, path: string, folders: string): Promise<void> {
    const response = await apiFetch('/api/vault', jsonBody('POST', {
        name: name.trim(),
        path: path.trim(),
        // The node reads folders apart by whitespace (strings.Fields), so a line is a folder.
        folders: folders.split('\n').join(' '),
    }));
    if (!response.ok) throw new Error(await refusal(response));
}

/** Exported for tests: one section per vault, its path and the folders it holds, each changeable. */
export function renderVaults(container: HTMLElement, vaults: Vault[], reload: () => Promise<void>): void {
    container.innerHTML = '';
    if (vaults.length === 0) {
        container.appendChild(said('The node keeps no vault yet.'));
    }
    for (const vault of vaults) {
        const s = section(vault.name);
        s.classList.add('obsidian-vault');
        const path = input('obsidian-path', vault.path, '/var/lib/obsidian/<vault>');
        const folders = foldersBox(vault.folders);
        s.append(row('Path:', path), row('Folders:', folders));
        if (vault.folders.length === 0) s.appendChild(said('It holds no folder of any repository yet.'));
        const actions = document.createElement('div');
        actions.className = 'element-actions';
        const save = new Button({
            label: 'Save',
            variant: 'primary',
            onClick: async () => {
                await setVault(vault.name, path.value, folders.value);
                await reload();
            },
        });
        actions.appendChild(save.element);
        s.appendChild(actions);
        container.appendChild(s);
    }
}

/** Exported for tests: a vault said whole, by name. */
export function renderNewVault(container: HTMLElement, reload: () => Promise<void>): void {
    container.innerHTML = '';
    const s = section('Another vault');
    const name = input('obsidian-name', '', 'its name in Obsidian Sync');
    const path = input('obsidian-path', '', '/var/lib/obsidian/<vault>');
    const folders = foldersBox([]);
    s.append(row('Name:', name), row('Path:', path), row('Folders:', folders));
    const actions = document.createElement('div');
    actions.className = 'element-actions';
    const add = new Button({
        label: 'Keep this vault',
        variant: 'ghost',
        onClick: async () => {
            await setVault(name.value, path.value, folders.value);
            await reload();
        },
    });
    actions.appendChild(add.element);
    s.appendChild(actions);
    container.appendChild(s);
}

/** Exported for tests: ask the node once and draw the vaults from its answer. */
export async function load(vaults: HTMLElement, more: HTMLElement): Promise<void> {
    const reload = () => load(vaults, more);
    try {
        const answer = await apiJson<{ vaults: Vault[] }>('/api/vault');
        renderVaults(vaults, answer.vaults, reload);
        renderNewVault(more, reload);
    } catch (err: unknown) {
        const message = `the node did not say its vaults: ${err instanceof Error ? err.message : String(err)}`;
        log.error(SEG.UI, `[ObsidianElement] ${message}`, err);
        vaults.innerHTML = '';
        vaults.appendChild(said(message, 'element-error'));
        more.innerHTML = '';
    }
}

export function createObsidianElement(): Element {
    return {
        id: ELEMENT_ID,
        title: 'Obsidian',
        symbol: '◆',
        renderContent: () => {
            const content = document.createElement('div');
            content.className = 'obsidian-element-content';
            content.style.display = 'flex';
            content.style.flexDirection = 'column';
            content.style.gap = '8px';
            content.style.padding = '12px';

            const vaults = document.createElement('div');
            const more = document.createElement('div');
            vaults.appendChild(said('Loading the node’s vaults…'));
            const blocker = said(SETUP_NOT_BUILT, 'obsidian-setup-not-built');
            content.append(blocker, vaults, more);

            void load(vaults, more);
            return content;
        },
    };
}

/** Opens the Obsidian element. */
export function openObsidianElement(): void {
    tray.open(ELEMENT_ID);
}
