/**
 * The A2A card the node would give a caller (am node), resting in ≡ as an italic A2A button.
 */

import type { Element } from '@teranos/elements';
import { buttonFrom } from '@teranos/elements';
import { apiFetch } from './client';
import { log, SEG } from './logger.ts';

export const A2A_CARD_ID = 'a2a-card';

/** What am node answers: the card, and what it leaves empty that the spec requires. */
export interface AmCard {
    card: {
        name?: string;
        description?: string;
        version?: string;
        supportedInterfaces?: { url?: string; protocolBinding?: string; protocolVersion?: string }[];
        capabilities?: { extensions?: { params?: { mcp?: { url?: string; protocolVersion?: string } } }[] };
        skills?: { name?: string }[];
    };
    missing: string[];
}

function row(label: string, value: HTMLElement | string): HTMLElement {
    const r = document.createElement('div');
    r.className = 'element-row';
    const l = document.createElement('span');
    l.className = 'label';
    l.textContent = label;
    const v = document.createElement('span');
    v.className = 'element-value';
    if (typeof value === 'string') v.textContent = value;
    else v.appendChild(value);
    r.append(l, v);
    return r;
}

/** Nothing is filled in that the node did not say. */
function said(value: string | undefined): HTMLElement | string {
    if (value) return value;
    const none = document.createElement('span');
    none.className = 'status-unwell';
    none.textContent = 'not said';
    return none;
}

function lines(items: string[], className: string): HTMLElement {
    const block = document.createElement('span');
    for (const item of items) {
        const line = document.createElement('div');
        line.className = className;
        line.textContent = item;
        block.appendChild(line);
    }
    return block;
}

/** Exported for tests: the card, drawn into container. */
export function renderA2ACard(container: HTMLElement, am: AmCard): void {
    const card = am.card;
    const interfaces = (card.supportedInterfaces ?? [])
        .map((i) => `${i.url ?? ''} ${i.protocolBinding ?? ''} ${i.protocolVersion ?? ''}`);
    // The node's MCP is not an interface: the node extension says where it answers.
    const mcp = (card.capabilities?.extensions ?? []).map((e) => e.params?.mcp).find((m) => m?.url);
    const skills = (card.skills ?? []).map((s) => s.name ?? '').join(', ');
    container.append(
        row('Name:', said(card.name)),
        row('Description:', said(card.description)),
        row('Version:', said(card.version)),
        row('Interface:', interfaces.length > 0 ? lines(interfaces, '') : said(undefined)),
        row('MCP:', said(mcp ? `${mcp.url} ${mcp.protocolVersion ?? ''}`.trim() : undefined)),
        row('Skills:', said(skills)),
        row('Missing:', am.missing.length > 0 ? lines(am.missing, 'status-unwell') : 'nothing'),
    );
}

// Asked when the button is drawn, so the window it opens into is measured full.
let asked: Promise<void> | null = null;
let answered: AmCard | null = null;
let refused: string | null = null;

/** Settles once the node answered or refused; which is in answered or refused. */
function ask(): Promise<void> {
    asked ??= apiFetch('/am/node')
        .then(async (response) => {
            if (!response.ok) throw new Error(`/am/node answered ${response.status} ${response.statusText}`);
            const body = await response.json() as Partial<AmCard>;
            if (typeof body.card !== 'object' || body.card === null || !Array.isArray(body.missing)) {
                throw new Error(`/am/node answered something that is not a card: ${JSON.stringify(body)}`);
            }
            answered = body as AmCard;
        })
        .catch((err: unknown) => {
            refused = err instanceof Error ? err.message : String(err);
            log.warn(SEG.SELF, `[a2a] ${refused}`);
        });
    return asked;
}

function drawInto(content: HTMLElement): void {
    if (answered) {
        renderA2ACard(content, answered);
        return;
    }
    if (refused) {
        content.append(row('Card:', said(undefined)), row('Why:', refused));
        return;
    }
    void ask().then(() => drawInto(content));
}

/** The card's element. */
function a2aCard(): Element {
    return {
        id: A2A_CARD_ID,
        title: 'A2A',
        renderContent: () => {
            const content = document.createElement('div');
            content.className = 'element-content';
            drawInto(content);
            return content;
        },
    };
}

// ≡ redraws itself whole, and the card is one element for its lifetime:
// the row holding it, or the hole it leaves, is made once and handed back.
let held: HTMLElement | null = null;

/** The row in ≡ that holds the card at rest, the same one every time. */
export function a2aCardRow(): HTMLElement {
    if (!held || held.ownerDocument !== document) {
        held = document.createElement('div');
        held.className = 'element-section';
        held.appendChild(buttonFrom(a2aCard(), { className: 'qntx-btn qntx-btn-ghost qntx-btn-small a2a-card-button' }));
        void ask();
    }
    return held;
}
