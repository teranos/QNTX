// The Design Element's specimens: each runs @teranos/elements' own code, the proximity engine,
// a morph transaction, title bar controls, stash and restore. None is an element: nothing here
// carries a data-element-id, so the page holds no element the tray does not.

import {
    Proximity,
    applyRestingDotGeometry,
    beginMorphToBox,
    beginMorphToDot,
    getOpenDuration,
    getRestDuration,
    addWindowControls,
    removeWindowControls,
    stashContent,
    restoreContent,
    hasStash,
    createInput,
    pressFrom,
    sizeText,
    WINDOW_BORDER_RADIUS,
    WINDOW_BOX_SHADOW,
} from '@teranos/elements';
import type { Element } from '@teranos/elements';
import { row, sectionTitle, specimen, statusText, titleBar } from './design-gallery.ts';

function dashedStage(height: string): HTMLDivElement {
    const stage = document.createElement('div');
    stage.style.position = 'relative';
    stage.style.height = height;
    stage.style.border = '1px dashed var(--border-on-dark)';
    stage.style.borderRadius = 'var(--border-radius)';
    return stage;
}

// Dots that grow as the pointer nears them, by the tray's own engine.
function proximitySpecimen(): HTMLElement {
    return specimen('Proximity', 'Pointer-distance morphing: move the pointer toward the dots', (body) => {
        const stage = dashedStage('160px');

        const column = document.createElement('div');
        column.style.position = 'absolute';
        column.style.right = '4px';
        column.style.top = '50%';
        column.style.transform = 'translateY(-50%)';
        column.style.display = 'flex';
        column.style.flexDirection = 'column';
        column.style.alignItems = 'flex-end';
        column.style.gap = '2px';
        for (let i = 1; i <= 5; i++) {
            const dot = document.createElement('div');
            dot.className = 'dot';
            applyRestingDotGeometry(dot);
            column.appendChild(dot);
        }
        stage.appendChild(column);

        const hint = document.createElement('div');
        hint.textContent = 'move the pointer toward the dots';
        hint.style.position = 'absolute';
        hint.style.left = '12px';
        hint.style.right = '40px';
        hint.style.top = '50%';
        hint.style.transform = 'translateY(-50%)';
        hint.style.color = 'var(--text-on-dark-tertiary)';
        sizeText(hint, 'xs');
        stage.appendChild(hint);

        const proximity = new Proximity();
        const none = new Map<string, Element>();
        let pointing = 0;
        const follow = () => {
            proximity.updateProximity(column, none, false);
            pointing = requestAnimationFrame(follow);
        };
        stage.addEventListener('pointerenter', () => { pointing = requestAnimationFrame(follow); });
        stage.addEventListener('pointermove', (e) => { proximity.setPointerPosition(e.clientX, e.clientY); });
        stage.addEventListener('pointerleave', () => {
            cancelAnimationFrame(pointing);
            proximity.setPointerPosition(-1000, -1000);
            proximity.updateProximity(column, none, false);
        });

        body.appendChild(stage);
    });
}

// One DOM node, a dot and then a window and then a dot, by the package's morph transactions.
function morphSpecimen(): HTMLElement {
    return specimen('Morph transactions', 'Web Animations API, taken or abandoned: dot to window and back, the same node', (body) => {
        const status = statusText();
        const stage = dashedStage('200px');

        const node = document.createElement('div');
        const atRest = () => {
            node.replaceChildren();
            node.removeAttribute('style');
            node.className = 'dot';
            node.style.position = 'absolute';
            node.style.right = '10px';
            node.style.bottom = '10px';
            node.style.width = '8px';
            node.style.height = '8px';
        };
        atRest();
        stage.appendChild(node);

        let open = false;
        const within = (r: DOMRect, at: DOMRect) => new DOMRect(r.left - at.left, r.top - at.top, r.width, r.height);

        const maximize = pressFrom({ id: 'design-morph-open', title: 'Maximize', does: async () => {
            if (open) { status.set('already a window'); return; }
            const at = stage.getBoundingClientRect();
            const from = within(node.getBoundingClientRect(), at);
            const width = Math.min(300, at.width - 20);
            const height = 140;
            const to = { x: (at.width - width) / 2, y: (at.height - height) / 2, width, height };
            node.style.left = `${from.left}px`;
            node.style.top = `${from.top}px`;
            node.style.right = '';
            node.style.bottom = '';
            await beginMorphToBox(node, from, to, getOpenDuration());
            node.className = '';
            Object.assign(node.style, {
                left: `${to.x}px`, top: `${to.y}px`, width: `${to.width}px`, height: `${to.height}px`,
                borderRadius: WINDOW_BORDER_RADIUS, boxShadow: WINDOW_BOX_SHADOW,
                backgroundColor: 'var(--bg-almost-black)', border: '1px solid var(--border-on-dark)',
                display: 'flex', flexDirection: 'column',
            });
            const words = document.createElement('div');
            words.textContent = 'This was the dot. Same DOM node: nothing cloned, nothing made again.';
            words.style.padding = '8px';
            words.style.color = 'var(--text-on-dark)';
            node.append(titleBar('Morphed window', 'title-bar'), words);
            open = true;
            status.set('committed: a window');
        } });

        const minimize = pressFrom({ id: 'design-morph-rest', title: 'Minimize', does: async () => {
            if (!open) { status.set('already a dot'); return; }
            const at = stage.getBoundingClientRect();
            const from = within(node.getBoundingClientRect(), at);
            node.replaceChildren();
            await beginMorphToDot(node, from, { x: at.width - 18, y: at.height - 18 }, getRestDuration());
            atRest();
            open = false;
            status.set('committed: the dot, the same node');
        } });

        body.append(stage, row(maximize.element, minimize.element), status.element);
    });
}

// The window's controls, put on a title bar and taken off it.
function controlsSpecimen(): HTMLElement {
    return specimen('Title bar controls', 'addWindowControls / removeWindowControls', (body) => {
        const status = statusText();
        const bar = titleBar('Sample element', 'title-bar');
        const add = pressFrom({ id: 'design-controls-add', title: 'Add controls', does: () => {
            addWindowControls(bar, { onMinimize: () => { status.set('minimize pressed'); }, onClose: () => { status.set('close pressed'); } });
            status.set('controls on the bar');
        } });
        const remove = pressFrom({ id: 'design-controls-remove', title: 'Remove controls', does: () => {
            removeWindowControls(bar);
            status.set('controls off the bar');
        } });
        body.append(bar, row(add.element, remove.element), status.element);
    });
}

// What a window holds, taken off the DOM and put back: the same nodes, what was typed in them kept.
function stashSpecimen(): HTMLElement {
    return specimen('Stash / restore', 'DOM content kept across morphs, in a WeakMap', (body) => {
        const status = statusText();

        const win = document.createElement('div');
        win.className = 'design-stash-window';
        win.style.border = '1px solid var(--border-on-dark)';
        win.style.borderRadius = 'var(--border-radius)';
        win.style.minHeight = '8px';

        const bar = titleBar('Stash demo', 'title-bar');
        addWindowControls(bar, { onMinimize: () => { status.set('minimize pressed'); }, onClose: () => { status.set('close pressed'); } });
        const inside = document.createElement('div');
        inside.style.padding = '8px';
        inside.appendChild(createInput({ placeholder: 'type, then stash' }));
        const note = document.createElement('div');
        note.textContent = 'What is typed and the nodes themselves outlive the stash. The window controls are stripped on stash and are not restored.';
        note.style.color = 'var(--text-on-dark-tertiary)';
        sizeText(note, 'xs');
        inside.appendChild(note);
        win.append(bar, inside);

        const stash = pressFrom({ id: 'design-stash', title: 'Stash', does: () => {
            if (hasStash(win)) { status.set('already stashed'); return; }
            stashContent(win);
            status.set(`stashed: ${win.childNodes.length} children, hasStash ${String(hasStash(win))}`);
        } });
        const restore = pressFrom({ id: 'design-restore', title: 'Restore', does: () => {
            if (!restoreContent(win)) { status.set('nothing stashed'); return; }
            status.set(`restored: ${win.childNodes.length} children`);
        } });

        body.append(win, row(stash.element, restore.element), status.element);
    });
}

export function renderElementSpecimens(root: HTMLElement): void {
    const section = document.createElement('section');
    section.className = 'element-section design-specimens';
    section.appendChild(sectionTitle('@teranos/elements'));
    section.append(proximitySpecimen(), morphSpecimen(), controlsSpecimen(), stashSpecimen());
    root.appendChild(section);
}
