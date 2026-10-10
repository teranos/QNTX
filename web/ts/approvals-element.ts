/**
 * Approvals Element — what waits on the human (ADR-052).
 */

// "approvals are attestations obviously"
// "Approvals is human only"
// "And only through it's element"
// "approvals is ROOT only for now"

// A panel, reached from ⍟ by ROOT. The element is @teranos/elements' (Rubidium:
// every option visible, the checks as segments in the GitHub button, the
// countdown in the buttons, the film roll); this is the host playing it with
// what the node says. Asked on open, after each press, and when the node tells
// the page an approval moved; nothing polls.

import type { Element, Approval as Card, ApprovalHandle, ApprovalOption, ApprovalStep, SegmentState } from '@teranos/elements';
import { tray, renderApprovals } from '@teranos/elements';
import { apiFetch, apiJson } from './client';
import { jsonBody } from './http-utils';
import { refusal } from './self-person';
import { log, SEG } from './logger';
import type { Approvals, Approval, ApprovalAnswered } from './generated/proto/plugin/grpc/protocol/approval';

export const ELEMENT_ID = 'approvals-element';

const APPROVALS_PATH = '/api/approvals';
const ANSWER_PATH = '/api/approvals/answer';

// "Click 1 means merge after CI passes, press again to force merge. If we still
// wait for CI, the NO will cancel the yes. Press NO again and it's definitely NO."
// The node takes these words and no others (server/approvals.go).
const MERGE_OPTIONS: ApprovalOption[] = [
    {
        label: 'Merge', means: 'yes', steps: [
            { says: 'merge when main CI passes', settles: 'when-ready' },
            { says: 'force merge', settles: 'now' },
        ],
    },
    {
        label: 'Don’t merge', means: 'no', steps: [
            { says: 'cancel the merge' },
            { says: 'definitely no' },
        ],
    },
];

/** Whole minutes since an RFC3339 moment; a moment that does not read is now. */
export function minutesAgo(at: string, now = Date.now()): number {
    const then = Date.parse(at);
    if (Number.isNaN(then)) return 0;
    return Math.max(0, Math.floor((now - then) / 60000));
}

/** The segment a check is drawn as, from the node's word for it. */
function stateOf(state: string): SegmentState {
    if (state === 'running' || state === 'done' || state === 'failed') return state;
    return 'waiting';
}

function lines(texts: string[]): HTMLElement {
    const el = document.createElement('div');
    el.style.whiteSpace = 'pre-wrap';
    el.style.overflowWrap = 'anywhere';
    el.style.lineHeight = '1.45';
    el.style.fontFamily = 'var(--font-mono)';
    el.textContent = texts.join('\n');
    return el;
}

/** Exported for tests: the card the element draws for what the node said. */
export function cardOf(approval: Approval, said: HTMLElement, now = Date.now()): Card {
    return {
        title: approval.title || approval.subject,
        link: { label: 'GitHub', href: approval.link },
        arrivedMinutesAgo: minutesAgo(approval.asked_at, now),
        context: () => {
            const context = document.createElement('div');
            context.style.display = 'flex';
            context.style.flexDirection = 'column';
            context.style.gap = '6px';
            const about = [`${approval.subject} → ${approval.base}`, `at ${approval.sha.slice(0, 7)}`];
            if (approval.said) about.push(`standing: ${approval.said}`);
            context.appendChild(lines(about));
            context.appendChild(said);
            return context;
        },
        options: MERGE_OPTIONS,
        checks: approval.checks.map((c) => ({ name: c.name, state: stateOf(c.state) })),
        merges: { label: 'main CI' },
    };
}

interface Shown {
    approval: Approval;
    handle: ApprovalHandle;
    /** What the node said of the last press, under the card's context. */
    said: HTMLElement;
}

/** One press, sent to the node. What it answers is what the card does next. */
async function sendPress(approval: Approval, option: ApprovalOption, step: ApprovalStep): Promise<ApprovalAnswered> {
    const response = await apiFetch(ANSWER_PATH, jsonBody('POST', {
        subject: approval.subject, sha: approval.sha, option: option.label, said: step.says,
    }));
    if (!response.ok) throw new Error(await refusal(response));
    return await response.json() as ApprovalAnswered;
}

/** Exported for tests: the roll, filled from the node and kept up to date by it. */
export function approvalsRoll(): { body: HTMLElement; load: () => Promise<void> } {
    const shown = new Map<string, Shown>();
    const byCard = new Map<Card, Shown>();

    /** What the node said of a press, under the card; a refusal in its words. */
    const press = (one: Shown, option: ApprovalOption, step: ApprovalStep): Promise<void> => {
        one.said.textContent = '';
        one.said.className = '';
        return sendPress(one.approval, option, step).then((answered) => {
            if (answered.did === 'merged') {
                one.said.textContent = `merged as ${answered.merge_sha.slice(0, 7)}`;
                one.handle.settled();
            } else if (answered.did === 'waiting') {
                one.said.textContent = 'merges when main CI passes';
            }
        }, (err: unknown) => {
            const why = err instanceof Error ? err.message : String(err);
            log.error(SEG.UI, 'The press was refused:', why);
            one.said.textContent = why;
            one.said.className = 'element-error';
        });
    };
    const roll = renderApprovals({
        pressed: (card, option, step) => {
            const one = byCard.get(card);
            if (!one) throw new Error(`a press came from a card the roll never drew: ${card.title}`);
            return press(one, option, step);
        },
    });

    const load = async () => {
        const answered = await apiJson<Approvals>(APPROVALS_PATH);
        if (!Array.isArray(answered.approvals)) throw new Error('the node answered the approvals with no list');
        const seen = new Set<string>();
        for (const approval of answered.approvals) {
            const key = `${approval.subject}@${approval.sha}`;
            seen.add(key);
            const states = approval.checks.map((c) => stateOf(c.state));
            const one = shown.get(key);
            if (one) {
                one.approval = approval;
                one.handle.checks(states);
                continue;
            }
            const said = document.createElement('div');
            said.style.fontFamily = 'var(--font-mono)';
            said.style.fontSize = '12px';
            const card = cardOf(approval, said);
            const handle = roll.add(card);
            const made = { approval, handle, said };
            shown.set(key, made);
            byCard.set(card, made);
        }
        // Gone from the list — merged, closed, or pushed to a new head — what
        // stood on it took effect, or nothing more is asked.
        for (const [key, one] of shown) {
            if (!seen.has(key)) one.handle.settled();
        }
    };
    return { body: roll.body, load };
}

let reloadOpen: (() => Promise<void>) | null = null;

/** The node told the page an approval moved: the open element reads the lines again. */
export function reloadApprovals(): Promise<void> {
    if (!reloadOpen) return Promise.resolve();
    return reloadOpen().catch((err: unknown) => {
        log.error(SEG.UI, 'The approvals were not read again:', err instanceof Error ? err.message : String(err));
    });
}

export function createApprovalsElement(): Element {
    return {
        id: ELEMENT_ID,
        title: 'Approvals',
        symbol: '✓',
        opensAs: 'panel',
        color: '#000',
        renderContent: () => {
            const { body, load } = approvalsRoll();
            reloadOpen = load;
            load().catch((err: unknown) => {
                const why = err instanceof Error ? err.message : String(err);
                log.error(SEG.UI, 'The approvals were not read:', why);
                const said = document.createElement('div');
                said.className = 'element-error';
                said.textContent = why;
                body.prepend(said);
            });
            return body;
        },
    };
}

/** Opens the Approvals element. Called from ⍟, for ROOT. */
export function openApprovalsElement(): void {
    tray.open(ELEMENT_ID);
}
