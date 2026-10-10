/**
 * Connectivity Element — surfaces raw fetch/WebSocket failures directly to the user.
 *
 * Auto-opens on the first failure. Subscribes to `connectivity.subscribeFailures`
 * and updates the failure list live. No user interaction required — load the
 * page in a broken state, the URL and reason are on screen.
 */

import { connectivity, type Failure } from '../../client';
import { copyable } from '../../copyable';
import { log, SEG } from '../../logger';
import { DEFAULT_COLOR, getForm, tray } from '@teranos/elements';
import type { Element } from '@teranos/elements';

const CONNECTIVITY_ELEMENT_ID = 'connectivity';

function formatFailure(f: Failure): string {
    const ts = new Date(f.at).toISOString().slice(11, 19);
    return `${ts}  ${f.source}  ${f.url}\n  ${f.reason}`;
}

interface WebBuildStamp { commit: string; build_time: string; qntx?: string; }

// Two shas answering two questions: qntx is the code that was built, commit is
// the pipeline that built it. They differ because the deploy lives in its own
// repo, and showing only the second made a correct build look like a wrong one.
function webBuildLine(): string {
    const stamp = (window as unknown as { __QNTX_WEB_BUILD__?: WebBuildStamp }).__QNTX_WEB_BUILD__;
    if (!stamp || !stamp.commit) return 'web build: unknown';
    const qntx = stamp.qntx ? `qntx ${stamp.qntx.slice(0, 7)}  ·  ` : '';
    return `web build: ${qntx}via ${stamp.commit.slice(0, 7)}  ·  ${stamp.build_time}`;
}

function renderConnectivityContent(): HTMLElement {
    const container = document.createElement('div');
    container.className = 'element-content';
    container.style.display = 'flex';
    container.style.flexDirection = 'column';
    container.style.gap = '8px';
    container.style.padding = '10px 12px';
    container.style.fontFamily = 'var(--font-mono)';
    container.style.fontSize = '11px';
    container.style.color = 'var(--text-on-dark)';

    const header = document.createElement('div');
    header.textContent = 'Connectivity failures';
    header.style.fontSize = '11px';
    header.style.opacity = '0.7';
    header.style.textTransform = 'uppercase';
    header.style.letterSpacing = '0.5px';

    const buildLine = document.createElement('div');
    buildLine.textContent = webBuildLine();
    buildLine.style.fontSize = '10px';
    buildLine.style.opacity = '0.55';
    copyable(buildLine);

    const list = document.createElement('pre');
    list.style.margin = '0';
    // Let the widest line drive the window's intrinsic width via the
    // ResizeObserver in @teranos/elements window/window.ts. maxWidth
    // (viewport * MAX_VIEWPORT_WIDTH_RATIO) still caps very long URLs.
    list.style.whiteSpace = 'pre';
    list.style.color = '#e06060';
    copyable(list);

    function render(): void {
        const failures = connectivity.failures;
        if (failures.length === 0) {
            list.textContent = '(none)';
            list.style.color = 'var(--text-secondary)';
            return;
        }
        list.style.color = '#e06060';
        list.textContent = failures.slice().reverse().map(formatFailure).join('\n\n');
    }

    render();
    connectivity.subscribeFailures(() => render());

    container.append(header, buildLine, list);
    return container;
}

// The dot's color while a failure has arrived that nobody has opened it to see.
const UNSEEN_FAILURE_COLOR = '#e06060';

// One item for the element's whole life, so a color set on it is the one every
// form reads back.
const item: Element = {
    id: CONNECTIVITY_ELEMENT_ID,
    title: 'Connectivity',
    renderContent: renderConnectivityContent,
    onClose: () => {
        log.debug(SEG.ELEMENT, '[ConnectivityElement] Closed');
    },
};

function isOpen(el: HTMLElement): boolean {
    const form = getForm(el);
    return form === 'window' || form === 'panel';
}

// The DOM element the observer below is on. A close removes it and the next
// add makes another, which is watched in turn.
let watched: HTMLElement | null = null;

/**
 * Opened is seen: the color comes off. Content renders once and is restored
 * from a stash after, so the form changing is the one sign of every opening.
 */
function clearColorWhenOpened(el: HTMLElement): void {
    if (watched === el) return;
    watched = el;
    new MutationObserver(() => {
        if (!isOpen(el) || item.color !== UNSEEN_FAILURE_COLOR) return;
        delete item.color;
        el.style.backgroundColor = DEFAULT_COLOR;
    }).observe(el, { attributes: true, attributeFilter: ['data-form'] });
}

/**
 * Add the connectivity element to the tray and open it. No-op if already present.
 * Called on the first failure event via subscribeFailures.
 */
export function spawnConnectivityElement(): void {
    if (!tray.has(CONNECTIVITY_ELEMENT_ID)) tray.add(item);
    tray.open(CONNECTIVITY_ELEMENT_ID);
}

/**
 * Put the connectivity element in the tray without opening it, and change its
 * dot's color. Nothing is opened over what is being done.
 *
 * "While it's in the tray, it may change dot color on activity"
 */
export function signalConnectivityInTray(): void {
    if (!tray.has(CONNECTIVITY_ELEMENT_ID)) tray.add(item);
    item.color = UNSEEN_FAILURE_COLOR;
    const el = document.querySelector<HTMLElement>(`[data-element-id="${CONNECTIVITY_ELEMENT_ID}"]`);
    if (!el) {
        log.warn(SEG.ELEMENT, `[ConnectivityElement] Added to the tray, but no element with data-element-id="${CONNECTIVITY_ELEMENT_ID}" is in the DOM`);
        return;
    }
    clearColorWhenOpened(el);
    // Open, it is being looked at: nothing to signal.
    if (isOpen(el)) {
        delete item.color;
        return;
    }
    el.style.backgroundColor = UNSEEN_FAILURE_COLOR;
}
