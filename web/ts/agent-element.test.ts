/**
 * Agent element — the agent of a namespace, signed in on the person's own
 * account and said to.
 *
 * Personas: Tim (happy path), Spike (edge cases).
 */

import { describe, test, expect } from 'bun:test';
import { drawAgent, type NamespaceAgent, type SignedIn } from './agent-element';
import type { ClaudeAm, ClaudeSaid } from './claude-element';

const am = (signedIn: boolean): ClaudeAm => ({
    did: 'did:key:z6MkAgentOfPond', model: 'claude-opus-5-5', effort: 'low',
    permission_mode: 'auto', permission_modes: ['auto', 'plan'], allow: [],
    session: '', answering: false, claude_code: '/nix/store/claude', not_ready: '',
    signed_in: signedIn, auth_method: signedIn ? 'claude.ai' : '',
});

const said: ClaudeSaid = {
    answer: 'hello', is_error: false, subtype: 'success', session: 's1', model: 'claude-opus-5-5',
    claude_code: '2.1.289', permission_mode: 'auto', denied: [], cost_usd: 0, took_ms: 1, unwritten: '',
};

// A node whose agent of pond is in the state given, and whose sign-in takes the code shown.
function node(signedIn: boolean, log: string[] = []): NamespaceAgent {
    let signed = signedIn;
    return {
        am: () => { log.push('am'); return Promise.resolve(am(signed)); },
        say: () => Promise.resolve(said),
        read: () => Promise.resolve(undefined),
        begin: () => {
            log.push('begin');
            const begun: SignedIn = { url: 'https://claude.ai/oauth/authorize?code=true', signed_in: false, auth_method: '', api_provider: '' };
            return Promise.resolve(begun);
        },
        finish: (code) => {
            log.push(`finish ${code}`);
            signed = true;
            const done: SignedIn = { url: '', signed_in: true, auth_method: 'claude.ai', api_provider: 'firstParty' };
            return Promise.resolve(done);
        },
    };
}

const settled = () => new Promise(resolve => setTimeout(resolve, 0));

describe('Agent - Tim', () => {
    // "Agent setup is the claude login flow"
    test('an agent not signed in is signed in by the URL given and the code brought back', async () => {
        const body = document.createElement('div');
        const log: string[] = [];
        drawAgent(body, 'pond', node(false, log));
        await settled();
        expect(body.querySelector('.agent-head')?.textContent).toBe('the agent of pond');
        const begin = body.querySelector<HTMLButtonElement>('.agent-begin')!;
        expect(begin).not.toBeNull();
        expect(body.querySelector('.claude-say')).toBeNull();

        begin.click();
        await settled();
        const at = body.querySelector<HTMLAnchorElement>('.agent-url')!;
        expect(at.hidden).toBe(false);
        expect(at.href).toBe('https://claude.ai/oauth/authorize?code=true');
        expect(at.target).toBe('_blank');

        const code = body.querySelector<HTMLInputElement>('.agent-code')!;
        code.value = 'abc#123';
        body.querySelector<HTMLButtonElement>('.agent-finish')!.click();
        await settled();
        await settled();
        expect(log).toEqual(['am', 'begin', 'finish abc#123', 'am']);
        // Signed in, the conversation is drawn and the sign-in is gone.
        expect(body.querySelector('.agent-begin')).toBeNull();
        expect(body.querySelector('.claude-say')).not.toBeNull();
        expect(body.querySelector<HTMLTextAreaElement>('.claude-says')?.placeholder).toBe('Say something to the agent of pond');
    });

    // An agent already signed in is spoken to at once.
    test('an agent signed in is the conversation', async () => {
        const body = document.createElement('div');
        drawAgent(body, 'pond', node(true));
        await settled();
        await settled();
        expect(body.querySelector('.agent-begin')).toBeNull();
        expect(body.querySelector('.claude-say')).not.toBeNull();
        // "No need to show did key to user": who answers is said by its model.
        expect(body.querySelector('.claude-who')?.textContent).toContain('claude-opus-5-5');
        expect(body.querySelector('.claude-who')?.textContent).not.toContain('did:key');
    });
});

describe('Agent - Spike', () => {
    // The node says why there is no agent to draw, and nothing is offered in its place.
    test('a namespace that opted into no agent is said, and no sign-in is drawn', async () => {
        const body = document.createElement('div');
        const none: NamespaceAgent = {
            ...node(false),
            am: () => Promise.reject(new Error('pond opted into no agent: set one with agents set')),
        };
        drawAgent(body, 'pond', none);
        await settled();
        expect(body.querySelector('.agent-head')?.textContent).toBe('pond opted into no agent: set one with agents set');
        expect(body.querySelector('.agent-begin')).toBeNull();
        expect(body.querySelector('.claude-say')).toBeNull();
    });

    // A sign-in the node refuses to begin leaves the button to press again, and says why.
    test('a sign-in that does not begin is said, and can be pressed again', async () => {
        const body = document.createElement('div');
        const refusing: NamespaceAgent = {
            ...node(false),
            begin: () => Promise.reject(new Error('this node has no Claude Code to sign in to')),
        };
        drawAgent(body, 'pond', refusing);
        await settled();
        const begin = body.querySelector<HTMLButtonElement>('.agent-begin')!;
        begin.click();
        await settled();
        expect(begin.disabled).toBe(false);
        expect(body.querySelector('.agent-failed')?.textContent).toContain('no Claude Code');
        expect(body.querySelector<HTMLAnchorElement>('.agent-url')?.hidden).toBe(true);
    });
});
