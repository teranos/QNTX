// Claude Element — the ROOT agent: said to, and read as the session it continues.
// "in terms of ui, this will be the Claude Element"
// What it is and why is ADR-048's; this draws what the claude signum answers.

import type { Element } from '@teranos/elements';
import { preventDrag, tray } from '@teranos/elements';
import { apiJson } from './client';
import { log, SEG } from './logger.ts';
import { Claude } from './sym';
import { apiError, sacredEntry } from './components/sacred';
import { renderTranscript, type TranscriptRead } from './components/element/transcript-element';

const ELEMENT_ID = 'claude-element';

// Who the ROOT agent is and how the node runs it (claude am).
export interface ClaudeAm {
    did: string;
    model: string;
    effort: string;
    permission_mode: string;
    permission_modes: string[];
    allow: string[];
    session: string;
    answering: boolean;
    claude_code: string;
    not_ready: string;
}

// What it answered (claude say).
export interface ClaudeSaid {
    answer: string;
    is_error: boolean;
    subtype: string;
    session: string;
    model: string;
    claude_code: string;
    permission_mode: string;
    denied: string[];
    cost_usd: number;
    took_ms: number;
    unwritten: string;
}

// The node, as this element asks it.
export interface RootAgent {
    am(): Promise<ClaudeAm>;
    say(says: string, mode: string): Promise<ClaudeSaid>;
    read(): Promise<TranscriptRead | undefined>;
}

const theNode: RootAgent = {
    am: () => apiJson<ClaudeAm>('/api/claude'),
    say: (says, mode) => apiJson<ClaudeSaid>('/api/claude/say', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(mode ? { says, permission_mode: mode } : { says }),
    }),
    // Its one session, read whole wherever whoever reads it stands.
    read: () => apiJson<{ transcript: TranscriptRead }>('/api/claude/session')
        .then(answer => answer.transcript),
};

// How often a turn that is going is read again.
const FOLLOW_MS = 2000;

function part<K extends keyof HTMLElementTagNameMap>(tag: K, className: string): HTMLElementTagNameMap[K] {
    const el = document.createElement(tag);
    el.className = className;
    return el;
}

function choice(value: string, label: string): HTMLOptionElement {
    const option = document.createElement('option');
    option.value = value;
    option.textContent = label;
    return option;
}

function wordsOf(err: unknown): string {
    return err instanceof Error ? err.message : String(err);
}

/**
 * The ROOT agent drawn into body: who it is, its session, and where something
 * is said to it. What it gives back stops reading a turn that is going.
 */
export function drawClaude(body: HTMLElement, node: RootAgent = theNode, every: number = FOLLOW_MS): () => void {
    const who = part('div', 'claude-who');
    who.textContent = 'Asking the node who answers…';
    const session = part('div', 'claude-session tr');
    const status = part('div', 'claude-status');
    const denied = part('div', 'claude-denied');
    const failed = part('div', 'claude-failed');

    const mode = part('select', 'claude-mode');
    mode.title = 'The permission mode Claude Code runs this turn in';
    const says = part('textarea', 'claude-says');
    says.rows = 2;
    says.placeholder = 'Say something to the ROOT agent';
    const send = part('button', 'qntx-btn qntx-btn-small claude-send');
    send.textContent = 'Say';
    const form = part('div', 'claude-say');
    form.append(mode, says, send);
    // What is typed and pressed here is not a drag of the element.
    preventDrag(form);
    preventDrag(session);

    body.replaceChildren(who, session, status, denied, failed, form);

    let reading = '';
    let sending = false;
    let following: ReturnType<typeof setInterval> | null = null;

    const fails = (what: string, err: unknown) => {
        const said = wordsOf(err);
        log.error(SEG.ELEMENT, `[Claude] ${what}: ${said}`);
        failed.replaceChildren(sacredEntry(apiError(`claude-${Date.now()}`, new Date().toISOString(), said, { surface: 'claude', region: what })));
    };

    const read = (): Promise<void> => node.read()
        .then(answer => {
            if (!answer) return;
            renderTranscript(session, answer, (text) => {
                navigator.clipboard.writeText(text).catch((err: unknown) => fails('copy', err));
            });
            // What was said last is what is looked at.
            const column = session.querySelector<HTMLElement>('.tr-col');
            if (column) column.scrollTop = column.scrollHeight;
        })
        .catch((err: unknown) => fails('read', err));

    const hold = (answering: boolean) => {
        send.disabled = answering;
        status.textContent = answering ? 'answering…' : '';
    };

    const settle = () => {
        if (following) { clearInterval(following); following = null; }
        hold(false);
    };

    // A turn that is going is read again and again until the node says it ended.
    const follow = () => {
        if (following) return;
        following = setInterval(() => {
            node.am()
                .then(is => {
                    if (is.session) { reading = is.session; void read(); }
                    if (!is.answering && !sending) settle();
                })
                .catch((err: unknown) => { fails('follow', err); if (!sending) settle(); });
        }, every);
    };

    const sendIt = () => {
        const words = says.value.trim();
        if (!words || sending) return;
        sending = true;
        hold(true);
        failed.replaceChildren();
        denied.textContent = '';
        follow();
        node.say(words, mode.value)
            .then(answer => {
                says.value = '';
                reading = answer.session;
                if (answer.denied.length > 0) denied.textContent = `It reached for ${answer.denied.join(', ')} and was not allowed.`;
                if (answer.unwritten) fails('session', new Error(`a row of the session was not written down: ${answer.unwritten}`));
                return read();
            })
            // What was typed stays in the box: a refusal does not cost the words.
            .catch((err: unknown) => fails('say', err))
            .finally(() => { sending = false; settle(); });
    };
    send.addEventListener('click', sendIt);
    says.addEventListener('keydown', (e) => {
        if (e.key === 'Enter' && !e.shiftKey) { e.preventDefault(); sendIt(); }
    });

    node.am()
        .then(is => {
            who.textContent = [is.did, is.model, is.effort, is.not_ready].filter(Boolean).join('  ');
            who.title = is.allow.length > 0 ? `Allowed without being asked: ${is.allow.join(', ')}` : 'Nothing is allowed without being asked';
            // am.toml that gives no mode leaves the choice here, and none is made for the speaker.
            if (!is.permission_mode) mode.appendChild(choice('', 'permission mode'));
            for (const name of is.permission_modes) mode.appendChild(choice(name, name));
            mode.value = is.permission_mode;
            reading = is.session;
            if (reading) void read();
            else session.textContent = 'Nothing has been said to it yet.';
            if (is.answering) { hold(true); follow(); }
        })
        .catch((err: unknown) => {
            who.textContent = wordsOf(err);
            send.disabled = true;
            log.warn(SEG.ELEMENT, `[Claude] the node did not say who answers: ${wordsOf(err)}`);
        });

    return settle;
}

export function createClaudeElement(): Element {
    return {
        id: ELEMENT_ID,
        title: 'Claude',
        symbol: Claude,
        renderContent: () => {
            const body = part('div', 'content-area claude');
            drawClaude(body);
            return body;
        },
    };
}

/** Opens the Claude element. */
export function openClaudeElement(): void {
    tray.open(ELEMENT_ID);
}
