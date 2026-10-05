// Pi Element — the ROOT agent in Pi: the same agent as in the Claude element,
// in its other harness and a session of its own (ADR-048).
// "IT MEANS WE ALSO NEED A PI ELEMENT"

import type { Element } from '@teranos/elements';
import { tray } from '@teranos/elements';
import { apiJson } from './client';
import { Pi } from './sym';
import { drawClaude, type ClaudeAm, type ClaudeSaid, type RootAgent } from './claude-element';
import type { TranscriptRead } from './components/element/transcript-element';

const ELEMENT_ID = 'pi-element';

// Who the ROOT agent is in Pi and how the node runs it there (pi am).
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
const theNodeInPi: RootAgent = {
    am: () => apiJson<PiAm>('/api/pi').then(asAm),
    say: (says) => apiJson<PiSaid>('/api/pi/say', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ says }),
    }).then(asSaid),
    read: () => apiJson<{ transcript: TranscriptRead }>('/api/pi/session')
        .then(answer => answer.transcript),
};

export function createPiElement(): Element {
    return {
        id: ELEMENT_ID,
        title: 'Pi',
        symbol: Pi,
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
