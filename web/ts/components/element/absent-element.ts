/**
 * What is drawn where an element should be, when the canvas has no way to draw it.
 *
 * The canvas used to put the plugin placeholder here whatever the element was, so
 * a published element — an attestation, with no plugin and no am.toml line — was
 * told to enable a plugin that does not exist. A placeholder that names the
 * wrong cause is worse than one that names none: it sends whoever reads it to
 * edit a file that has nothing to do with it.
 *
 * So this one asks. /g/ knows what is published and /api/plugins knows what is
 * loaded, and whichever recognises the name is what the element says about itself.
 * Nobody should have to open a console to find out why an element is missing.
 */

import type { Element } from '@teranos/elements';
import { canvasPlaced } from '@teranos/elements';
import { createSymbolSpan, settleSymbolSpan } from '@teranos/elements';
import { apiFetch } from '../../client';
import { log, SEG } from '../../logger';
import { el } from '../../html-utils';
import { elementAbsenceReason } from './plugin-provided-elements';

/** One element the node serves, as /g/ reports it. */
interface PublishedElement {
    name: string;
    as: string;
    url: string;
}

const MUTED = '#999';
const FAULT = '#ef4444';

/**
 * The frame and the first thing it says. What it says is replaced as soon as
 * the node answers — the element is on the canvas before either call returns.
 */
export function createAbsentElement(item: Element, name: string): HTMLElement {
    item.border ??= '1px solid var(--border)';

    const { element } = canvasPlaced({
        item: item,
        className: `canvas-element-absent absent-${name}`,
        defaults: {
            x: item.x ?? 200,
            y: item.y ?? 200,
            width: item.width ?? 400,
            height: item.height ?? 300,
        },
        resizable: false,
        logLabel: 'AbsentElement',
    });
    element.style.pointerEvents = 'auto';

    const symbol = item.symbolElement
        ? settleSymbolSpan(item.symbolElement)
        : createSymbolSpan(item.symbol ?? '?');
    Object.assign(symbol.style, { fontWeight: 'bold', color: '#666', opacity: '0.5' });

    const titleText = el('span', {
        text: `${name} (absent)`,
        style: { fontSize: '12px', fontFamily: 'monospace', lineHeight: '1.4', color: MUTED },
    });
    element.appendChild(el('div', { class: 'title-bar title-bar--auto' }, [symbol, titleText]));

    const content = el('div', {
        class: 'element-absent-content',
        style: {
            flex: '1', padding: '16px', color: 'var(--text-secondary)',
            fontSize: '12px', fontFamily: 'monospace', textAlign: 'center',
            lineHeight: '1.6', display: 'flex', flexDirection: 'column',
            justifyContent: 'center', alignItems: 'center', gap: '10px',
        },
    });
    element.appendChild(content);

    // Remembered so the frame can be asked again: what it says first is true
    // when it is drawn, and the usual first answer stops being true a moment
    // later, once discovery has run.
    asking.set(element, { name, content });

    say(content, 'this element is not drawn', 'asking the node why');
    askWhy(name, content);

    return element;
}

/** Put one answer in the element's place. */
function say(content: HTMLElement, headline: string, detail: string, tone = MUTED): void {
    content.textContent = '';
    content.append(
        el('div', { text: headline }),
        el('div', {
            text: detail,
            style: {
                fontSize: '11px', color: tone, maxWidth: '360px',
                wordBreak: 'break-word', overflowWrap: 'break-word',
            },
        }),
    );
}

/**
 * Ask what this name is, and say what comes back.
 *
 * /g/ first: a published element is what this node now serves UI as, and a name
 * it knows is answered without ever mentioning plugins.
 */
function askWhy(name: string, content: HTMLElement): void {
    served()
        .then(items => {
            const found = items.find(g => g.name === name);
            if (found) {
                const why = elementAbsenceReason(name);
                if (why) {
                    say(content, `${name} is published and did not load`, why, FAULT);
                } else {
                    // Published, no failure recorded: the page has not reached
                    // it yet. It appears on its own when discovery lands, and
                    // askAgain redraws this if discovery lands on a failure.
                    say(content, `${name} is published as ${found.as}`,
                        `the page has not loaded ${found.url} yet`);
                }
                return;
            }
            return notPublished(name, content, items);
        })
        .catch((err: unknown) => {
            say(content, `${name} is not drawn`,
                `and the node could not say why: ${err instanceof Error ? err.message : String(err)}`, FAULT);
            log.error(SEG.ELEMENT, `[AbsentElement] Could not ask the node about ${name}:`, err);
        });
}

/**
 * Ask again and redraw.
 *
 * What this frame first says is true at the moment it is drawn, and the most
 * common case — published, discovery has not run yet — stops being true a
 * moment later. Whoever ran discovery and still found nothing registered calls
 * this, so the frame says why it failed instead of that it had not tried.
 */
export function askAbsentAgain(element: HTMLElement): void {
    const asked = asking.get(element);
    if (asked) askWhy(asked.name, asked.content);
}

/** What each absent frame is about, so it can be asked again. */
const asking = new WeakMap<HTMLElement, { name: string; content: HTMLElement }>();

/** Everything /g/ serves. */
async function served(): Promise<PublishedElement[]> {
    const resp = await apiFetch('/g/');
    if (!resp.ok) {
        throw new Error(`/g/ answered ${resp.status}`);
    }
    const data: { elements?: PublishedElement[] } = await resp.json();
    return data.elements ?? [];
}

/**
 * Nothing is published under this name, so the other thing it could be is a
 * plugin. This is the only branch that may speak about plugins at all.
 */
async function notPublished(name: string, content: HTMLElement, items: PublishedElement[]): Promise<void> {
    const resp = await apiFetch('/api/plugins');
    if (!resp.ok) {
        say(content, `nothing is published as ${name}`,
            `and this node did not say what plugins it runs (/api/plugins answered ${resp.status})`);
        return;
    }

    const data: { plugins?: Array<{ name: string; state: string; message?: string }> } = await resp.json();
    const plugin = (data.plugins ?? []).find(p => p.name === name);

    if (!plugin) {
        // A canvas record keeps a symbol, and an element is published under a
        // name — so what a record is called here may be neither. Saying what
        // this node does serve beats saying only that it does not serve this.
        const publishes = items.length > 0
            ? `this node publishes ${items.map(g => g.name).join(', ')}`
            : 'this node publishes no elements at all';
        say(content, `nothing on this node is called ${name}`, publishes);
        return;
    }
    if (plugin.state === 'loading') {
        say(content, `the ${name} plugin is starting`, 'the element appears when it is ready');
        return;
    }
    if (plugin.state === 'failed') {
        say(content, `the ${name} plugin failed to load`, plugin.message ?? 'it said no more than that', FAULT);
        return;
    }
    say(content, `the ${name} plugin is ${plugin.state}`, 'and it published no element of this kind');
}
