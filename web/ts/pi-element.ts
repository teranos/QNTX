// Pi Element — the Node Agent in Pi: the same agent as in the Claude element,
// in its other harness and a session of its own (ADR-048).
// "IT MEANS WE ALSO NEED A PI ELEMENT"

import type { Element } from '@teranos/elements';
import { tray } from '@teranos/elements';
import { apiJson } from './client';
import { Pi } from './sym';
import { drawClaude, type ClaudeAm, type ClaudeSaid, type NodeAgent } from './claude-element';
import type { TranscriptRead } from './components/element/transcript-element';

const ELEMENT_ID = 'pi-element';

// Who the Node Agent is in Pi and how the node runs it there (pi am).
export interface PiAm {
    did: string;
    model: string;
    thinking: string;
    gateway: string;
    session: string;
    answering: boolean;
    pi: string;
    pi_version: string;
    not_ready: string;
}

// What it answered in Pi (pi say).
export interface PiSaid {
    answer: string;
    is_error: boolean;
    stop: string;
    session: string;
    model: string;
    pi_version: string;
    cost_usd: number;
    took_ms: number;
    unwritten: string;
}

/** Exported for tests: pi am as the Claude element draws who answers. */
export function asAm(is: PiAm): ClaudeAm {
    return {
        did: is.did, model: is.model, effort: is.thinking,
        permission_mode: '', permission_modes: [], allow: [],
        session: is.session, answering: is.answering, claude_code: is.pi,
        not_ready: is.not_ready,
        // Pi signs in by its gateway, not by claude login.
        signed_in: true, auth_method: is.gateway,
    };
}

/** Exported for tests: pi say as the Claude element draws an answer. */
export function asSaid(said: PiSaid): ClaudeSaid {
    return {
        answer: said.answer, is_error: said.is_error, subtype: said.stop,
        session: said.session, model: said.model, claude_code: said.pi_version,
        permission_mode: '', denied: [], cost_usd: said.cost_usd,
        took_ms: said.took_ms, unwritten: said.unwritten,
    };
}

// The node, as the Pi element asks it: its session in Pi, beside the one the
// Claude element reads.
const theNodeInPi: NodeAgent = {
    am: () => apiJson<{ pi: PiAm }>('/api/agent?harness=pi').then(is => asAm(is.pi)),
    say: (says) => apiJson<{ pi: PiSaid }>('/api/agent/say', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ harness: 'pi', says }),
    }).then(said => asSaid(said.pi)),
    read: () => apiJson<{ transcript: TranscriptRead }>('/api/agent/session?harness=pi')
        .then(answer => answer.transcript),
};

// Who this is for: whoever reaches these (audience.ts).
export const needs = ['/api/agent'] as const;

export function createPiElement(): Element {
    return {
        id: ELEMENT_ID,
        title: 'Pi',
        symbol: Pi,
        // Pi is said to as Claude is: it opens at what was said last.
        opensAt: 'end',
        renderContent: () => {
            const body = document.createElement('div');
            body.className = 'content-area claude';
            drawClaude(body, theNodeInPi);
            return body;
        },
    };
}

/** Opens the Pi element. */
export function openPiElement(): void {
    tray.open(ELEMENT_ID);
}
