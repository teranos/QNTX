// The Design Element's components: what an element is built from, live, in @teranos/elements' own primitives.

import {
    addWindowControls,
    createInput,
    createButton,
    createStatusLine,
    createTable,
    pressFrom,
    sizeText,
    tableCell,
    TEXT_SIZES,
    type TextSize,
} from '@teranos/elements';
import { record } from './table-cell.ts';

/** A heading, the way an element's sections are headed. */
export function sectionTitle(text: string): HTMLHeadingElement {
    const h = document.createElement('h3');
    h.className = 'element-section-title';
    h.textContent = text;
    sizeText(h, 'lg');
    return h;
}

/** One specimen: its title bar, what it shows under that, and the specimen itself. */
export function specimen(title: string, description: string, build: (body: HTMLElement) => void): HTMLElement {
    const container = document.createElement('div');
    container.className = 'design-specimen';
    container.dataset.specimen = title;
    container.style.border = '1px solid var(--border-on-dark)';
    container.style.borderRadius = 'var(--border-radius)';
    container.style.marginBottom = '10px';

    const bar = titleBar(title, 'title-bar title-bar--auto');
    container.appendChild(bar);

    const desc = document.createElement('div');
    desc.textContent = description;
    desc.style.color = 'var(--text-on-dark-tertiary)';
    desc.style.padding = '4px 8px 0';
    sizeText(desc, 'xs');
    container.appendChild(desc);

    const body = document.createElement('div');
    body.className = 'design-specimen-body';
    body.style.display = 'flex';
    body.style.flexDirection = 'column';
    body.style.gap = '8px';
    body.style.padding = '8px';
    build(body);
    container.appendChild(body);
    return container;
}

/** A title bar as the elements wear one: its classes, its title. */
export function titleBar(title: string, classes: string): HTMLDivElement {
    const bar = document.createElement('div');
    bar.className = classes;
    const text = document.createElement('span');
    text.textContent = title;
    bar.appendChild(text);
    return bar;
}

/** A line under a specimen saying what it did. */
export function statusText(): { element: HTMLElement; set: (msg: string) => void } {
    const node = document.createElement('div');
    node.className = 'design-status';
    node.style.color = 'var(--text-on-dark-tertiary)';
    sizeText(node, 'xs');
    return { element: node, set(msg: string) { node.textContent = msg; } };
}

/** A row of presses that wraps to the width it has. */
export function row(...nodes: HTMLElement[]): HTMLDivElement {
    const line = document.createElement('div');
    line.style.display = 'flex';
    line.style.flexWrap = 'wrap';
    line.style.alignItems = 'center';
    line.style.gap = '6px';
    line.append(...nodes);
    return line;
}

// What a press is, each kind: one press does it; a second stage undoable, irrevocable, or creating.
function pressSpecimen(): HTMLElement {
    return specimen('Press', 'pressFrom: the same size at every stage; white at rest, red armed when irrevocable, green and shimmering armed when it creates', (body) => {
        const status = statusText();
        const one = pressFrom({ id: 'design-press-one', title: 'One press', does: () => { status.set('One press: done'); } });
        const undoable = pressFrom({ id: 'design-press-undoable', title: 'Undoable', again: 'do it', does: () => { status.set('Undoable: done'); } });
        const irrevocable = pressFrom({ id: 'design-press-irrevocable', title: 'Irrevocable', again: 'cannot be undone', irrevocable: true, does: () => { status.set('Irrevocable: done'); } });
        const creates = pressFrom({ id: 'design-press-creates', title: 'Creates', again: 'create it', creates: true, does: () => { status.set('Creates: created'); } });
        const throws = pressFrom({ id: 'design-press-throws', title: 'Throws', does: () => { throw new Error('a throw from does is said beside the press'); } });
        body.appendChild(row(one.element, undoable.element, irrevocable.element, creates.element, throws.element));
        body.appendChild(status.element);
    });
}

// ElementUI's primitives, the ones a plugin's element gets through ui.*: a field, a press, a status line.
function primitivesSpecimen(): HTMLElement {
    return specimen('ElementUI primitives', 'ui.input(), ui.button(), ui.statusLine(): createInput, createButton, createStatusLine', (body) => {
        const field = createInput({ label: 'Say', placeholder: 'something to say' });
        const status = createStatusLine();
        const say = createButton({
            label: 'Say',
            onClick: () => {
                const typed = field.querySelector('input');
                if (!typed || !typed.value) {
                    status.show('nothing typed', true);
                    return;
                }
                status.show(`said: ${typed.value}`);
            },
        });
        body.append(field, say, status.element);
    });
}

function code(text: string): HTMLPreElement {
    const pre = document.createElement('pre');
    pre.textContent = text;
    pre.style.fontFamily = 'var(--font-mono)';
    pre.style.color = 'var(--text-on-dark)';
    pre.style.whiteSpace = 'pre-wrap';
    pre.style.margin = '0';
    sizeText(pre, 'sm');
    return pre;
}

// The title bars elements wear: py and ts their own colours (py-element.ts, ts-element.ts), auto-height, a panel's.
function titleBarSpecimen(): HTMLElement {
    return specimen('title-bar', 'One title bar for every manifestation', (body) => {
        const status = statusText();
        const standard = titleBar('ix-prompt', 'title-bar');
        addWindowControls(standard, { onMinimize: () => { status.set('minimize pressed'); }, onClose: () => { status.set('close pressed'); } });

        const py = titleBar('py-element', 'title-bar');
        py.style.backgroundColor = '#2a5578';
        (py.firstChild as HTMLElement).style.color = '#FFD43B';

        const ts = titleBar('ts-element', 'title-bar');
        ts.style.backgroundColor = '#5c3d1a';
        (ts.firstChild as HTMLElement).style.color = '#f0c878';

        const auto = titleBar('attestation with a longer title that wraps to show auto-height (title-bar--auto)', 'title-bar title-bar--auto');

        const panel = document.createElement('div');
        panel.className = 'panel';
        panel.appendChild(titleBar('plugin-config (a panel: no drag cursor)', 'title-bar title-bar--auto'));

        body.append(standard, status.element, py, code("import secrets\nprint(secrets.choice(['teach', 'meld']))"), ts, code('const id = generateASUID()\nconsole.log(id)'), auto, panel);
    });
}

// A table is the package's, four columns at most; a record is a label beside its value (table-cell.ts).
function tableSpecimen(): HTMLElement {
    return specimen('Table', 'createTable(name, heads, rows): one to four heads, every row as wide; record() for a label beside its value', (body) => {
        body.appendChild(createTable('design-sample', ['Name', 'Size', 'px'], (Object.keys(TEXT_SIZES) as TextSize[]).map((size) => ({
            cells: [tableCell(size), tableCell(`sizeText(node, '${size}')`), tableCell(String(TEXT_SIZES[size]))] as const,
        }))));
        body.appendChild(record('design-record', ['Label', 'Value'], [
            ['Columns', 'one to four'],
            ['A cell', 'its text, or the host\'s own td'],
        ]));
    });
}

// Text inside an element is at one of the package's sizes, and no other.
function textSpecimen(): HTMLElement {
    return specimen('Text size', "sizeText(node, 'xs' | 'sm' | 'md' | 'lg')", (body) => {
        for (const size of Object.keys(TEXT_SIZES) as TextSize[]) {
            const line = document.createElement('div');
            line.textContent = `${size} · ${TEXT_SIZES[size]}px · The quick brown fox jumps over the lazy dog`;
            line.style.color = 'var(--text-on-dark)';
            sizeText(line, size);
            body.appendChild(line);
        }
    });
}

export function renderComponentGallery(root: HTMLElement): void {
    const section = document.createElement('section');
    section.className = 'element-section design-components';
    section.appendChild(sectionTitle('Components'));
    section.append(pressSpecimen(), primitivesSpecimen(), tableSpecimen(), textSpecimen(), titleBarSpecimen());
    root.appendChild(section);
}
