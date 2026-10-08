// Keys Element: a namespace's keys (ADR-051), opened from ⍟ for where the
// person stands. A value is sent once and never drawn: the node answers names.

import type { Element } from '@teranos/elements';
import { tray } from '@teranos/elements';
import { apiFetch } from './client';
import { Button } from './components/button';
import { log, SEG } from './logger';
import { refusal } from './self-person';

export interface KeyInfo {
    name: string;
    set_by: string;
    set_at: string;
}

const ELEMENT_ID = 'keys-element';

/** The node's answer, or its refusal as the error, in its own words. */
async function asked(path: string, init?: RequestInit): Promise<KeyInfo[]> {
    const response = await apiFetch(path, { ...init, headers: { Accept: 'application/json', 'Content-Type': 'application/json' } });
    if (!response.ok) {
        throw new Error(await refusal(response));
    }
    const body = await response.json() as { keys?: KeyInfo[] };
    return body.keys ?? [];
}

export function listKeys(): Promise<KeyInfo[]> {
    return asked('/api/keys');
}

export function setKey(name: string, value: string): Promise<KeyInfo[]> {
    return asked('/api/keys', { method: 'POST', body: JSON.stringify({ name, value }) });
}

export function dropKey(name: string): Promise<KeyInfo[]> {
    return asked('/api/keys/drop', { method: 'POST', body: JSON.stringify({ name }) });
}

function fmt(dt: string): string {
    const d = new Date(dt);
    return isNaN(d.getTime()) ? dt : d.toISOString().slice(0, 19).replace('T', ' ');
}

/** Exported for tests. Each row is a name, who set it, when, and its drop. */
export function renderKeys(container: HTMLElement, keys: KeyInfo[], onDrop: (name: string) => Promise<void>): void {
    container.innerHTML = '';
    if (keys.length === 0) {
        const empty = document.createElement('div');
        empty.className = 'element-loading';
        empty.textContent = 'No keys.';
        container.appendChild(empty);
        return;
    }

    const table = document.createElement('table');
    table.className = 'element-table keys-table';
    const thead = document.createElement('thead');
    const headings = document.createElement('tr');
    for (const name of ['Name', 'Set by', 'Set at', '']) {
        const th = document.createElement('th');
        th.textContent = name;
        headings.appendChild(th);
    }
    thead.appendChild(headings);
    table.appendChild(thead);

    const tbody = document.createElement('tbody');
    for (const k of keys) {
        const tr = document.createElement('tr');
        for (const text of [k.name, k.set_by, fmt(k.set_at)]) {
            const td = document.createElement('td');
            td.textContent = text;
            tr.appendChild(td);
        }
        const action = document.createElement('td');
        const drop = new Button({
            label: 'Drop',
            variant: 'danger',
            size: 'small',
            confirmation: { label: `Drop ${k.name}` },
            onClick: async () => { await onDrop(k.name); },
        });
        action.appendChild(drop.element);
        tr.appendChild(action);
        tbody.appendChild(tr);
    }
    table.appendChild(tbody);
    container.appendChild(table);
}

/** Exported for tests. The value field is emptied once the node has it. */
export function renderForm(container: HTMLElement, onSave: (name: string, value: string) => Promise<void>): void {
    container.innerHTML = '';
    container.className = 'keys-form';
    container.style.display = 'flex';
    container.style.flexWrap = 'wrap';
    container.style.gap = '8px';

    const name = document.createElement('input');
    name.type = 'text';
    name.className = 'keys-name';
    name.placeholder = 'name';
    name.setAttribute('aria-label', 'Key name');

    const value = document.createElement('input');
    value.type = 'password';
    value.className = 'keys-value';
    value.placeholder = 'value';
    value.autocomplete = 'off';
    value.setAttribute('aria-label', 'Key value');

    const save = new Button({
        label: 'Save',
        variant: 'primary',
        onClick: async () => {
            await onSave(name.value.trim(), value.value);
            value.value = '';
            name.value = '';
        },
    });

    container.appendChild(name);
    container.appendChild(value);
    container.appendChild(save.element);
}

function showRefusal(container: HTMLElement, message: string): void {
    container.innerHTML = '';
    const errBox = document.createElement('div');
    errBox.className = 'element-error';
    errBox.textContent = message;
    container.appendChild(errBox);
}

/** Exported for tests: the element's content for one namespace. */
export function renderKeysContent(content: HTMLElement, namespace: string): void {
    content.innerHTML = '';
    const heading = document.createElement('h3');
    heading.className = 'element-section-title';
    heading.textContent = `Keys of ${namespace}`;

    const listContainer = document.createElement('div');
    listContainer.className = 'keys-list';
    listContainer.innerHTML = '<div class="element-loading">Loading keys…</div>';

    const formContainer = document.createElement('div');

    const draw = (keys: KeyInfo[]) => {
        renderKeys(listContainer, keys, async (name) => { draw(await dropKey(name)); });
    };
    renderForm(formContainer, async (name, value) => { draw(await setKey(name, value)); });

    content.appendChild(heading);
    content.appendChild(formContainer);
    content.appendChild(listContainer);

    listKeys().then(draw).catch((err: unknown) => {
        log.error(SEG.UI, '[KeysElement] the node did not list the keys', err);
        showRefusal(listContainer, err instanceof Error ? err.message : String(err));
    });
}

/** The namespace the element is for, handed in by ⍟ as it opens it. */
let standing = '';

export function createKeysElement(): Element {
    return {
        id: ELEMENT_ID,
        title: 'Keys',
        symbol: '⚷',
        renderContent: () => {
            const content = document.createElement('div');
            content.className = 'keys-element-content';
            content.style.display = 'flex';
            content.style.flexDirection = 'column';
            content.style.gap = '8px';
            content.style.padding = '12px';
            renderKeysContent(content, standing);
            return content;
        },
    };
}

/** Opens the keys element for the namespace the person stands in. Called from ⍟. */
export function openKeysElement(namespace: string): void {
    standing = namespace;
    tray.open(ELEMENT_ID);
}
