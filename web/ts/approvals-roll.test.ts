// Approvals: what waits on a human, shown as a list to go through. Every press
// sends, and the other option sends again. What the host decides with them is
// the host's.
// Personas: Tim (options shown, a press sends, the fast classifier arrives, the roll's order),
// Spike (checks running or failed, a step that settles now, a press that sends
// nothing), Jenny (the PR story: merge, force, cancel, main's CI done).

import { describe, test, expect, beforeEach } from 'bun:test';
import { renderApprovals } from './approvals-roll';
import type { Approval, ApprovalHost, ApprovalOption, ApprovalRoll } from './approvals-roll';

const text = (s: string) => {
    const el = document.createElement('div');
    el.textContent = s;
    return el;
};

let pressed: string[];
let roll: ApprovalRoll;

const host: ApprovalHost = {
    pressed: (approval, option, step) => {
        pressed.push(`${approval.title}: ${option.label} → ${step.says}`);
        return Promise.resolve();
    },
};

/** An option whose one press says its label, with no percentage. */
const says = (label: string, means: ApprovalOption['means'], more: Partial<ApprovalOption> = {}): ApprovalOption => ({
    label, means, steps: [{ says: label }], confidence: 'none', ...more,
});

const yesNo = (title: string, arrivedMinutesAgo = 1, more: Partial<Approval> = {}): Approval => ({
    title,
    arrivedMinutesAgo,
    context: () => text(title),
    options: [says('Yes', 'yes'), says('No', 'no')],
    asides: [],
    openFor: { kind: 'until-decided' },
    checks: [],
    merges: { kind: 'none' },
    ...more,
});

const merge = (title: string, more: Partial<Approval> = {}): Approval => ({
    title,
    link: { label: 'GitHub', href: 'https://github.com/teranos/elements/pull/29' },
    arrivedMinutesAgo: 3,
    context: () => text(title),
    options: [
        says('Merge', 'yes', { steps: [{ says: 'merge when main CI passes', settles: 'when-ready' }, { says: 'force merge', settles: 'now' }] }),
        says('Don’t merge', 'no', { steps: [{ says: 'cancel the merge' }, { says: 'definitely no' }] }),
    ],
    asides: [],
    openFor: { kind: 'until-decided' },
    merges: { kind: 'until-host', label: 'main CI' },
    checks: [{ name: 'TypeScript', state: 'done' }, { name: 'Browser', state: 'done' }],
    ...more,
});

/** JSDOM says a colour back as rgb(); happy-dom as it was set. Either is the colour. */
const isColor = (got: string, hex: string): boolean => {
    const n = parseInt(hex.slice(1), 16);
    const rgb = hex.length === 4
        ? `rgb(${[0, 1, 2].map((i) => parseInt(hex[i + 1]! + hex[i + 1]!, 16)).join(', ')})`
        : `rgb(${(n >> 16) & 255}, ${(n >> 8) & 255}, ${n & 255})`;
    return got.includes(hex) || got.includes(rgb);
};

const buttons = (card: HTMLElement) => Array.from(card.querySelectorAll<HTMLButtonElement>('button'));
const button = (card: HTMLElement, label: string) => buttons(card).find((b) => b.querySelector('span')?.textContent === label)!;
const note = (btn: HTMLButtonElement) => btn.querySelector<HTMLElement>('[data-note]')!.textContent;
const titles = () => Array.from(roll.body.querySelectorAll('section')).map((s) => s.querySelector('[data-title]')!.textContent);

beforeEach(() => {
    document.body.innerHTML = '';
    pressed = [];
    roll = renderApprovals(host);
    document.body.appendChild(roll.body);
});

describe('Tim: every option visible, a press sends, the roll in order', () => {
    test('every option is a button; yes is green, no is red, neither is white', () => {
        const { card } = roll.add(yesNo('Raise the interval', 1, { options: [says('Yes', 'yes'), says('No', 'no'), says('Later', 'neither')] }));
        expect(buttons(card).map((b) => b.querySelector('span')!.textContent)).toEqual(['Yes', 'No', 'Later']);
        expect(isColor(button(card, 'Yes').style.border, '#16a34a')).toBe(true);
        expect(isColor(button(card, 'No').style.border, '#dc2626')).toBe(true);
        expect(isColor(button(card, 'Later').style.border, '#fff')).toBe(true);
    });

    test('a press sends, and fills the button with its colour', () => {
        const { card } = roll.add(yesNo('Raise the interval'));
        button(card, 'Yes').click();
        expect(pressed).toEqual(['Raise the interval: Yes → Yes']);
        expect(isColor(button(card, 'Yes').style.background, '#16a34a')).toBe(true);
        expect(isColor(button(card, 'No').style.background, '#000')).toBe(true);
    });

    test('the other option sends again; the same one says its next step', () => {
        const { card } = roll.add(merge('Merge PR #29'));
        button(card, 'Merge').click();
        button(card, 'Don’t merge').click();
        button(card, 'Don’t merge').click();
        expect(pressed).toEqual([
            'Merge PR #29: Merge → merge when main CI passes',
            'Merge PR #29: Don’t merge → cancel the merge',
            'Merge PR #29: Don’t merge → definitely no',
        ]);
    });

    test('the fast classifier answers after the card is there: the place is kept, then the number arrives', async () => {
        let answer!: (n: number) => void;
        const { card } = roll.add(yesNo('Raise the interval', 1, {
            options: [says('Yes', 'yes', { confidence: new Promise<number>((r) => { answer = r; }) }), says('No', 'no', { confidence: 18 })],
        }));
        expect(button(card, 'Yes').textContent).toContain('··%');
        expect(button(card, 'No').textContent).toContain('18%');
        answer(82);
        await Promise.resolve();
        expect(button(card, 'Yes').textContent).toContain('82%');
    });

    test('the most recent undecided one goes first, and the end of the roll is last', () => {
        roll.add(yesNo('Older', 40));
        roll.add(yesNo('Newest', 1));
        roll.add(yesNo('Middle', 12));
        roll.settle();
        expect(titles()).toEqual(['Newest', 'Middle', 'Older']);
        expect(roll.body.lastElementChild!.textContent).toBe('Nothing left to decide.');
    });
});

describe('Spike: not yet, not at all, and nothing more', () => {
    test('checks still running: nothing can be pressed, and the buttons say so', () => {
        const { card } = roll.add(merge('Merge PR #29', { checks: [{ name: 'TypeScript', state: 'done' }, { name: 'Browser', state: 'running' }] }));
        expect(button(card, 'Merge').disabled).toBe(true);
        expect(note(button(card, 'Merge'))).toBe('checks running');
        button(card, 'Merge').click();
        expect(pressed).toEqual([]);
    });

    test('a check failed: not ready for approval at all', () => {
        const { card } = roll.add(merge('Merge PR #31', { checks: [{ name: 'TypeScript', state: 'done' }, { name: 'Browser', state: 'failed' }] }));
        expect(button(card, 'Merge').disabled).toBe(true);
        expect(note(button(card, 'Merge'))).toBe('checks failed');
    });

    test('the host says the checks are done, and it can be decided', () => {
        const handle = roll.add(merge('Merge PR #29', { checks: [{ name: 'TypeScript', state: 'running' }, { name: 'Browser', state: 'waiting' }] }));
        expect(button(handle.card, 'Merge').disabled).toBe(true);
        handle.checks(['done', 'done']);
        expect(button(handle.card, 'Merge').disabled).toBe(false);
        expect(handle.card.querySelectorAll('[data-state="done"]').length).toBeGreaterThanOrEqual(2);
    });

    // "adn yet the approval dissapears as if its done"
    test('a press the node refuses leaves the card as it was, still to decide', async () => {
        document.body.innerHTML = '';
        const refusing = renderApprovals({ pressed: () => Promise.reject(new Error('GitHub refused the merge')) });
        document.body.appendChild(refusing.body);
        const { card } = refusing.add(merge('Merge PR #33'));
        const before = note(button(card, 'Merge'));
        button(card, 'Merge').click();
        await new Promise((done) => setTimeout(done, 0));
        expect(note(button(card, 'Merge'))).toBe(before);
        expect(button(card, 'Merge').disabled).toBe(false);
    });

    test('a step that settles now is final: pressing again sends nothing', () => {
        const { card } = roll.add(merge('Merge PR #29'));
        button(card, 'Merge').click();
        button(card, 'Merge').click();
        expect(pressed.at(-1)).toBe('Merge PR #29: Merge → force merge');
        button(card, 'Merge').click();
        button(card, 'Don’t merge').click();
        expect(pressed.length).toBe(2);
        expect(button(card, 'Don’t merge').disabled).toBe(true);
    });

    test('an option with no steps says its label, and pressing it again sends nothing more', () => {
        const { card } = roll.add(yesNo('Raise the interval'));
        button(card, 'Yes').click();
        button(card, 'Yes').click();
        expect(pressed).toEqual(['Raise the interval: Yes → Yes']);
    });

    test('one still waiting on checks goes after the ready ones; one that failed goes last', () => {
        roll.add(merge('Failed', { arrivedMinutesAgo: 1, checks: [{ name: 'Browser', state: 'failed' }] }));
        roll.add(merge('Waiting', { arrivedMinutesAgo: 2, checks: [{ name: 'Browser', state: 'running' }] }));
        roll.add(yesNo('Ready', 30));
        roll.settle();
        expect(titles()).toEqual(['Ready', 'Waiting', 'Failed']);
    });
});

describe('Jenny: the PR story', () => {
    test('Merge waits on main’s CI, in the button; Merge again forces it', () => {
        const { card } = roll.add(merge('Merge PR #29'));
        button(card, 'Merge').click();
        expect(note(button(card, 'Merge'))).toContain('merge when main CI passes');
        expect(note(button(card, 'Merge'))).toContain('main CI');
        expect(note(button(card, 'Merge'))).toContain('again: force merge');
        button(card, 'Merge').click();
        expect(note(button(card, 'Merge'))).toBe('force merge');
        expect(button(card, 'Merge').disabled).toBe(true);
    });

    test('Don’t merge while main’s CI is waited on cancels the merge; again is definitely no', () => {
        const { card } = roll.add(merge('Merge PR #29'));
        button(card, 'Merge').click();
        button(card, 'Don’t merge').click();
        // Nothing stands on Merge any more: it only says what pressing it would do.
        expect(note(button(card, 'Merge'))).toBe('press: merge when main CI passes');
        expect(isColor(button(card, 'Merge').style.background, '#000')).toBe(true);
        expect(isColor(button(card, 'Don’t merge').style.background, '#dc2626')).toBe(true);
        expect(note(button(card, 'Don’t merge'))).toContain('cancel the merge');
        button(card, 'Don’t merge').click();
        expect(pressed.at(-1)).toBe('Merge PR #29: Don’t merge → definitely no');
    });

    test('main’s CI passes: what stood takes effect, and nothing more can be sent', () => {
        const handle = roll.add(merge('Merge PR #29'));
        button(handle.card, 'Merge').click();
        handle.settled();
        expect(note(button(handle.card, 'Merge'))).toBe('merge when main CI passes');
        expect(button(handle.card, 'Merge').disabled).toBe(true);
        button(handle.card, 'Don’t merge').click();
        expect(pressed.length).toBe(1);
    });

    test('the film roll: a decided one stays above, and the undecided follow', () => {
        const a = roll.add(yesNo('A', 1));
        roll.add(yesNo('B', 2));
        roll.add(yesNo('C', 3));
        roll.settle();
        button(roll.body.querySelector('section')!, 'Yes').click();
        roll.settle();
        expect(titles()).toEqual(['A', 'B', 'C']);
        button(roll.body.querySelectorAll('section')[2]!, 'No').click();
        roll.settle();
        expect(titles()).toEqual(['A', 'C', 'B']);
        expect(a.card.isConnected).toBe(true);
    });
});
