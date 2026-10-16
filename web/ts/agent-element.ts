// Agent Element — the agent of the namespace this person stands in (ADR-048):
// signed in on their own account, by Claude Code's own flow, and said to.

// "Agent setup is its own thing, skippable as part of namespace creation, but still someything you probably want to setup"

// "Agent setup is the claude login flow"

import type { Element } from '@teranos/elements';
import { preventDrag, tray } from '@teranos/elements';
import { apiJson } from './client';
import { log, SEG } from './logger.ts';
import { Agent } from './sym';
import { standingNamespace } from './standing.ts';
import { drawClaude, type ClaudeAm, type ClaudeSaid, type RootAgent } from './claude-element';
import type { TranscriptRead } from './components/element/transcript-element';
import { apiError, sacredEntry } from './components/sacred';

const ELEMENT_ID = 'agent-element';

// The namespace's agent, as agents am answers it: who opted the namespace
// into it, and who it is in Claude Code.
export interface AgentAm {
    namespace: string;
    set_by: string;
    agent: ClaudeAm;
}

// What agents login answers: where to sign in while a sign-in is begun and
// not finished, and what Claude Code says.
export interface SignedIn {
    url: string;
    signed_in: boolean;
    auth_method: string;
    api_provider: string;
}

// The node, as this element asks it about one namespace's agent.
export interface NamespaceAgent extends RootAgent {
    begin(): Promise<SignedIn>;
    finish(code: string): Promise<SignedIn>;
}

const JSON_HEADERS = { 'Content-Type': 'application/json' };

/** The agent of namespace, at the agents signum's path. */
export function agentOf(namespace: string): NamespaceAgent {
    const at = `/api/agents/${encodeURIComponent(namespace)}`;
    return {
        am: () => apiJson<AgentAm>(at).then(is => is.agent),
        say: (says, mode) => apiJson<ClaudeSaid>(`${at}/say`, {
            method: 'POST',
            headers: JSON_HEADERS,
            body: JSON.stringify(mode ? { says, permission_mode: mode } : { says }),
        }),
        // Its one session, shared by everyone who has reach on the namespace.
        read: () => apiJson<{ transcript: TranscriptRead }>(`${at}/session`)
            .then(answer => answer.transcript),
        // Without a code a sign-in is begun and the URL given; with the code shown there it is finished.
        begin: () => apiJson<SignedIn>(`${at}/login`, { method: 'POST', headers: JSON_HEADERS, body: '{}' }),
        finish: (code) => apiJson<SignedIn>(`${at}/login`, {
            method: 'POST',
            headers: JSON_HEADERS,
            body: JSON.stringify({ code }),
        }),
    };
}

function part<K extends keyof HTMLElementTagNameMap>(tag: K, className: string): HTMLElementTagNameMap[K] {
    const el = document.createElement(tag);
    el.className = className;
    return el;
}

function wordsOf(err: unknown): string {
    return err instanceof Error ? err.message : String(err);
}

/**
 * The agent of namespace drawn into body: its sign-in while it is not signed in,
 * the conversation once it is. What it gives back stops reading a turn that is going.
 */
export function drawAgent(body: HTMLElement, namespace: string, node: NamespaceAgent = agentOf(namespace)): () => void {
    const head = part('div', 'agent-head');
    head.textContent = `the agent of ${namespace}`;
    const signin = part('div', 'agent-signin');
    const failed = part('div', 'agent-failed');
    const talk = part('div', 'agent-talk');
    body.replaceChildren(head, signin, failed, talk);

    let settle = () => {};

    const fails = (what: string, err: unknown) => {
        const said = wordsOf(err);
        log.error(SEG.ELEMENT, `[Agent] ${what}: ${said}`);
        failed.replaceChildren(sacredEntry(apiError(`agent-${Date.now()}`, new Date().toISOString(), said, { surface: 'agent', region: what })));
    };

    // Signed in: the conversation, the Claude element's own drawing of it.
    const converse = () => {
        signin.replaceChildren();
        settle = drawClaude(talk, node);
        const says = talk.querySelector<HTMLTextAreaElement>('.claude-says');
        if (says) says.placeholder = `Say something to the agent of ${namespace}`;
    };

    // Not signed in: Claude Code's own flow, carried by the node. The URL it
    // prints is a link out, and the code shown there comes back here.
    const signIn = () => {
        const said = part('div', 'agent-said');
        said.textContent = `The agent of ${namespace} signs in with your own Claude account. The node holds no credential of it: what the sign-in leaves is the agent's own.`;
        const begin = part('button', 'qntx-btn qntx-btn-small agent-begin');
        begin.textContent = 'Sign in';
        const at = part('a', 'agent-url');
        at.target = '_blank';
        at.rel = 'noopener noreferrer';
        at.hidden = true;
        const code = part('input', 'agent-code');
        code.placeholder = 'the code shown there';
        code.hidden = true;
        const finish = part('button', 'qntx-btn qntx-btn-small agent-finish');
        finish.textContent = 'Finish';
        finish.hidden = true;
        signin.replaceChildren(said, begin, at, code, finish);
        preventDrag(signin);

        begin.addEventListener('click', () => {
            begin.disabled = true;
            failed.replaceChildren();
            node.begin()
                .then(begun => {
                    if (begun.signed_in) { converse(); return; }
                    at.href = begun.url;
                    at.textContent = begun.url;
                    at.hidden = false;
                    code.hidden = false;
                    finish.hidden = false;
                    code.focus();
                })
                .catch((err: unknown) => { begin.disabled = false; fails('sign in', err); });
        });

        const finishIt = () => {
            const shown = code.value.trim();
            if (!shown) return;
            finish.disabled = true;
            failed.replaceChildren();
            node.finish(shown)
                .then(done => {
                    if (done.signed_in) { converse(); return; }
                    finish.disabled = false;
                    fails('sign in', new Error(`the code was handed back, and Claude Code says the agent of ${namespace} is not signed in`));
                })
                .catch((err: unknown) => { finish.disabled = false; fails('sign in', err); });
        };
        finish.addEventListener('click', finishIt);
        code.addEventListener('keydown', (e) => {
            if (e.key === 'Enter') { e.preventDefault(); finishIt(); }
        });
    };

    node.am()
        .then(is => {
            if (is.signed_in) converse();
            else signIn();
        })
        .catch((err: unknown) => {
            // The node says why: no agent set, or none that this person acts in.
            head.textContent = wordsOf(err);
            log.warn(SEG.ELEMENT, `[Agent] the node did not say who the agent of ${namespace} is: ${wordsOf(err)}`);
        });

    return () => settle();
}

// Who this is for: whoever reaches these (audience.ts).
export const needs = ['/api/agents/{namespace}'] as const;

export function createAgentElement(): Element {
    return {
        id: ELEMENT_ID,
        title: 'Agent',
        symbol: Agent,
        renderContent: () => {
            const body = part('div', 'content-area claude agent');
            drawAgent(body, standingNamespace());
            return body;
        },
    };
}

/** Opens the Agent element. */
export function openAgentElement(): void {
    tray.open(ELEMENT_ID);
}
